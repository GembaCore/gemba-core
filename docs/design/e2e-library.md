---
title: "Playwright e2e library \u2014 design"
decision: none
backfill: pending
---

# Playwright e2e library — design

Companion to epic **gm-5v8v**. Records the architecture decisions made
in conversation so future contributors can read this doc instead of
re-deriving the answers.

The library lives at `testing/e2e/` (filed by gm-5v8v.1). The
single-script driver that ran for gm-0i0d
(`scripts/e2e/hello-world.test.mjs`) was migrated into
`testing/e2e/specs/integration/dispatch-chain.spec.ts` by gm-5v8v.15;
the script directory has been removed.

---

## 1. Library, not monolith

**Decision.** Build the e2e suite as a library of small, opinionated
specs grouped by tier and surface. Reject "one big spec file" or
"one driver per ticket".

**Reasoning.** The SPA exposes ~18 distinct routes / surfaces (Board,
Backlog, Grid, Graph, Sessions, Agents, Escalations, Capabilities,
Health, Mail, plus drawers and the global chrome). Each route has on
the order of 10–30 distinct interactions worth pinning. Multiplied
out — and again across the **fake** + **deep** backend axis — the
suite trends to **~300 specs**. At that size, copy-paste between
specs (auth setup, fixture seeding, navigation, waits) calcifies
faster than the surface evolves; a refactor of any shared step
touches every file. The scaffolding wins are:

- **Selector reuse** through page object models (POMs) — see §8.
- **Data setup reuse** through builders — see §9.
- **Backend-axis reuse** through fixtures — see §4.

A library also lets us tag specs by tier and grep-filter at the
project level (§7), which is the only sane way to keep the PR-fast
lane fast.

---

## 2. Surface inventory — implemented vs spec'd-pending

**Decision.** Specs cover both the surfaces that **exist today** and
the surfaces the ui-spec **ratifies but hasn't shipped yet**. The
latter live under `testing/e2e/specs/pending/`.

**Why pending specs.** Two reasons:

1. **Spec-first lock-in.** Writing the e2e for a surface before it
   ships pins the contract — the spec author can't quietly rename a
   `data-testid` or change a hotkey without breaking the pending
   spec, which the implementer must fix.
2. **Visible work.** A pending spec is a checklist of "this surface
   is on the roadmap and here's what 'done' looks like". Closing a
   feature bead means moving its spec out of `pending/` into the
   appropriate tier directory.

**Mechanic.** Pending specs are wrapped in `test.fixme(...)` with a
short rationale (`fixme: gm-XXX — feature not shipped`). Playwright
reports them but doesn't fail. CI lanes can opt them in via
`--grep-invert "@pending"` (default) or `--grep "@pending"` (nightly
audit).

**Implemented routes today** (as of 2026-04-25):

| Route          | Implementation file                | Tier coverage    |
| -------------- | ---------------------------------- | ---------------- |
| `/board`       | `web/src/pages/BoardPage.tsx`      | smoke / board    |
| `/backlog`     | `web/src/pages/BacklogPage.tsx`    | smoke / route    |
| `/grid`        | `web/src/pages/GridPage.tsx`       | smoke / grid     |
| `/graph`       | `web/src/pages/GraphPage.tsx`      | smoke / graph    |
| `/sessions`    | `web/src/pages/SessionsPage.tsx`   | smoke / sessions |
| `/agents`      | `web/src/pages/AgentsPage.tsx`     | smoke / sessions |
| `/escalations` | placeholder                        | smoke / pending  |
| `/capabilities`| placeholder                        | smoke / pending  |
| `/health`      | `web/src/pages/placeholders.tsx`   | smoke            |
| `/mail`        | placeholder (gated)                | smoke / pending  |

The drawer and dialog surfaces (WorkItemDrawer at ~70KB,
EpicDrawer, AgentDetailDrawer, JsonlImportDialog, NewSessionDialog,
EscalationPanel) are nested but tested as their own tier (§3). They
don't have routes; the test navigates to a parent surface and
opens them.

---

## 3. Tier taxonomy

**Decision.** Group specs by **tier**, not by route. Each tier
expresses a different *kind* of confidence the suite is buying.

| Tier          | What it pins                                                               |
| ------------- | -------------------------------------------------------------------------- |
| `smoke`       | Every route mounts, no console errors, axe accessibility scan passes.      |
| `chrome`      | Sidebar / Topbar / Command palette / Hotkeys / AdaptorBanner — global UI.  |
| `route`       | Per-page interactions: filter chips, search, drawers, detail drill-in.     |
| `realtime`    | SSE `/events` + `/api/adaptors/stream` drive UI updates without refresh.   |
| `modes`       | Workspace-mode (unsupervised / supervised / managed) confirmation UX.      |
| `error`       | 4xx / 5xx surfaces, adaptor-degraded banner, network-flake recovery.       |
| `integration` | Multi-route flows: dispatch a session from the board, observe in sessions. |

**Why these seven and not, say, ten.** Each tier has a distinct CI
lane policy (§7) and a distinct *rate of churn*: smoke is stable,
realtime is flaky, integration is slow. Splitting a tier costs a CI
lane; merging two tiers loses the policy distinction. The seven were
chosen so each one has a self-evident "did this break?" signal.

**Directory layout.**

```
testing/e2e/specs/
  smoke/        # routes load, no console errors, axe
  chrome/       # global UI components
  board/        # /board specifics
  grid/         # /grid specifics
  graph/        # /graph specifics
  drawers/      # WorkItemDrawer / EpicDrawer / AgentDetailDrawer
  sessions/     # /sessions + /agents
  realtime/     # SSE-driven UI invalidation
  modes/        # workspace-mode confirmation UX
  auth/         # cookie / bearer / login redirect
  error/        # 4xx / 5xx / degraded-adaptor surfaces
  integration/  # multi-route flows
  pending/      # spec'd but not shipped (test.fixme)
```

A spec lives in **one** tier folder. Cross-cutting concerns (auth,
hotkeys) appear as helpers, not as a tier.

---

## 4. Backend axis: `fake` vs `deep`

**Decision.** Each spec runs against one of two backend modes, named
after the fixture:

- **`fake`** — `gemba serve` mounted with the in-memory
  `testadaptors.FakeWorkPlane`. State resets sub-second between
  workers; safely parallelizes; runs on every PR.
- **`deep`** — real `gemba serve` against a real Dolt server with
  the bd adaptor pointed at a real `.beads/` database. Serializes
  to ≤2 workers; runs on merge / nightly.

**Both modes use the same spec source.** A spec doesn't care which
backend it runs against unless it asserts on a behaviour that only
exists in one mode (a real `bd` mutation, an SSE round-trip across
the bd→dolt→hub pipeline). Those specs are tagged `@deep` (§5).

**Mechanic — Playwright project matrix.** `playwright.config.ts`
declares one project per (tier × backend) pair. The fixture wired
into each project's `use:` block selects the backend:

```ts
// fixtures/server.ts
type Backend = 'fake' | 'real';
export const test = base.extend<{ backend: Backend; baseURL: string }>({
  backend: ['fake', { option: true, scope: 'worker' }],
  baseURL: async ({ backend }, use, info) => {
    if (backend === 'fake') {
      // Spawn gemba serve --workplane=fake on a free port.
      use(await spawnFakeServer(info.workerIndex));
    } else {
      // Lease a Dolt DB + worktree pair from the worker pool.
      use(await leaseDeepServer(info.workerIndex));
    }
  },
});
```

Each project sets `backend: 'fake'` or `backend: 'real'`. The same
test file mounts in two projects with no per-spec branching — the
fixture switch carries it.

**Why two modes and not one.** Pure fake runs sub-second and is
useful for tens of repetitions per change, but it doesn't pin the
real bd→dolt→hub pipeline. Pure deep is honest but takes minutes per
spec and serializes hard. The split lets us pay for honesty only
where the spec asserts on it (§5).

---

## 5. The `@deep` tagging rule

**Rule.** Tag a spec `@deep` if it asserts on **a backend response
the fake doesn't faithfully reproduce**. Concretely:

- Spec issues a write (POST /api/work-items, PATCH, POST
  /sessions) AND inspects the persisted side-effect (re-read,
  list filter, SSE event landing) → `@deep`.
- Spec drives an SSE round-trip end-to-end (issue mutation in tab
  A, observe SSE invalidation in tab B) → `@deep`.
- Spec exercises adaptor-specific behaviour (`bd close` translates
  to `state_category=completed`; the dolt-url read-only adaptor
  returns 405) → `@deep`.
- Spec asserts an empty-state envelope when no adaptor is bound →
  **not** `@deep`. The fake handles that.
- Spec asserts on UI rendering of a fixture-supplied list →
  **not** `@deep`. The fake handles that.

**Mechanic.** Tags ride in the Playwright `test()` description:

```ts
test('drag epic into In Progress dispatches a session @deep', async () => { … });
```

The deep project's `grep` filter is `/@deep/`; the fake project's
filter is `/^(?!.*@deep)/`. A spec without `@deep` runs in the fake
matrix only. A spec with `@deep` runs in **both** matrices —
running it in fake ensures the test itself isn't broken before the
deep matrix invests minutes proving the backend.

**Anti-pattern.** Don't tag `@deep` defensively — the deep matrix
budget is small. If a spec doesn't assert on a backend response
that fake misrepresents, dropping `@deep` is correct.

---

## 6. Worker isolation in deep mode

**Problem.** Deep mode's "real Dolt + real bd" stack is not a static
fixture; each worker's spec mutates the database. Two workers
sharing one Dolt DB will trample each other.

**Decision.** In deep mode, each Playwright worker leases a private
**(Dolt database, beads worktree)** pair from a small pool. The
pool size matches the configured worker count (≤2 in CI today).

**Mechanic.**

1. **Dolt DB namespacing.** The deep harness pre-creates databases
   `e2e_w0`, `e2e_w1`, … one per worker slot, in the local Dolt
   server (port 3307). Each worker's `gemba serve` is started with
   `--dolt-db=e2e_w<workerIndex>`.

2. **Worktree namespacing.** Each worker also gets a private
   worktree at `/tmp/gemba-e2e/w<workerIndex>/` so a spec that
   spawns a session doesn't collide with a sibling worker's session.

3. **Reset between specs.** Each worker truncates its private
   tables (or `dolt sql -q "DROP TABLE …; CREATE TABLE …"`) in a
   `test.beforeEach`. Between-spec reset is faster than between-worker
   tear-down and lets specs assume a clean slate.

4. **Cleanup.** Worktrees are removed on `afterAll`; Dolt databases
   persist across CI runs (creation is the slow step) and are
   truncated, not dropped.

**Anti-pattern.** Do **not** point all workers at the production
local Dolt database. The CLAUDE.md "test pollution" warning (orphan
`testdb_*` / `beads_t*` rows) is exactly this failure mode at scale.
The lease pool keeps test data namespace-isolated AND survivable
across runs.

---

## 7. CI lane → project-matrix mapping

**Decision.** Three CI lanes, each binding a different subset of the
(tier × backend) project grid:

| Lane         | Trigger                | Projects                                                                                  | Budget   |
| ------------ | ---------------------- | ----------------------------------------------------------------------------------------- | -------- |
| **PR-fast**  | every push to a PR     | `smoke@fake`, `chrome@fake`, `route@fake`, `error@fake`                                   | <2 min   |
| **merge**    | post-merge to `main`   | PR-fast + `realtime@fake`, `modes@fake`, `integration@fake`, `smoke@deep`, `chrome@deep`  | <8 min   |
| **nightly**  | scheduled              | every project — full fake matrix + full deep matrix + `pending/` audit                    | unlimited|

**Why the lanes are layered, not parallel.** Each lane is a strict
superset of the previous one. A spec that fails PR-fast will also
fail merge and nightly, so we don't need a separate "what lane do I
go in" decision per spec — the project tag does it.

**Why deep doesn't ride PR-fast.** Two reasons:

1. **Cost.** Deep mode serializes and a spec that takes 30 seconds in
   fake takes 2–5 minutes in deep (real bd subprocess spawn, real
   Dolt commit, real hub fan-out). Multiplying by spec count blows
   the PR feedback loop.
2. **Flakiness budget.** Real bd + real Dolt has a non-zero rate of
   transient failures (Dolt server hiccups, file-locking races on
   git worktrees). Putting that on PR-fast forces every contributor
   to learn the deep mode failure modes; putting it on merge means
   the merge-queue triage stays in one team's head.

**The escape hatch.** A `[deep]` opt-in tag in a PR title forces
PR-fast to include the deep matrix. Used when a PR touches the bd
adaptor directly.

---

## 8. POM strategy + `data-testid` selectors

**Decision.** Each route gets a Page Object Model in `pages/`. The
POM exposes high-level methods (`board.dragCardTo(...)`,
`grid.openImportDialog()`) that internally select on
**`data-testid` attributes**. Tests do not call `page.locator(...)`
on raw CSS selectors.

**Why `data-testid`.** Three reasons:

1. **Already there.** The SPA already exposes ~160+ `data-testid`
   attributes (sample: `grep -rn "data-testid" web/src`). Every new
   surface lands one alongside the markup. The cost to switch to
   another scheme is ≥1 day of mechanical refactoring with zero
   product gain.
2. **Refactor-stable.** A `data-testid` survives Tailwind class
   churn, role/aria refactors, and CSS-in-JS rewrites. A
   `getByRole('button', { name: 'Save' })` selector breaks the
   day someone renames the button.
3. **Searchable.** `grep "grid-import-jsonl"` instantly answers
   "what test depends on this control?" — a property neither
   role-based nor structural selectors give cheaply.

**The contract.** New UI MUST land a `data-testid` on every control
the spec author would target: buttons, modals, dialog wrappers,
input fields, list rows, status pills. The naming convention is
`<surface>-<purpose>` (e.g. `grid-import-jsonl`, `board-column-1`,
`drawer-close`). Nested testids stack: `grid-import-summary` lives
inside `grid-import-dialog`.

**POM shape (sketch).**

```ts
// pages/BoardPage.ts
export class BoardPage {
  constructor(private page: Page) {}
  async goto() { await this.page.goto('/board'); }
  column(name: 'Backlog' | 'In Progress' | 'Done') {
    return this.page.getByTestId(`board-column-${name}`);
  }
  card(id: string) { return this.page.getByTestId(`board-card-${id}`); }
  async dragCardTo(cardId: string, columnName: string) { … }
}
```

Specs read like prose, not Playwright:

```ts
const board = new BoardPage(page);
await board.goto();
await board.dragCardTo('gm-foo', 'In Progress');
await expect(board.card('gm-foo')).toBeVisible();
```

**When NOT to add a POM method.** If a method would be called by
exactly one spec, inline the testid in the spec. POMs exist to
deduplicate, not to ceremonially wrap every selector.

---

## 9. Builders, not fixture-as-data

**Decision.** Test setup uses **builder functions** that produce
WorkItems / Escalations / Agents / Sessions, not static JSON
fixtures.

**Why.** Static JSON fixtures (a `fixtures/work-items.json` file
listing 50 beads) have three failure modes:

1. **Drift.** Adding a required field to `WorkItem` breaks every
   fixture file at once and the failure points at the JSON, not at
   the spec that produces the failing scenario.
2. **Inflexibility.** A spec that needs "any bead in the
   `started` state with priority 2" picks one from the file and
   becomes coupled to that particular row's other fields.
3. **Discovery.** `grep -l "gm-fixture-7"` doesn't tell you what
   that bead represents semantically. A builder call
   `bead({ stateCategory: 'started', priority: 2 })` does.

**Builder shape.**

```ts
// builders/workitem.ts
export function bead(patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id: nextId(),
    kind: 'task',
    title: 'fixture',
    state_category: 'unstarted',
    status: 'open',
    created_at: nowISO(),
    updated_at: nowISO(),
    ...patch,
  };
}
```

Specs compose:

```ts
const epic = bead({ kind: 'epic', state_category: 'started' });
const child = bead({ kind: 'task', relationships: [{ kind: 'parent_child', from: epic.id, to: '' }] });
await server.seed([epic, child]);
```

**Anti-pattern.** Don't add a `seedTen()` helper that returns ten
arbitrary beads. Specs that need "ten beads" either care about a
specific ten (use builders) or they're really testing pagination
(use `Array.from({ length: 10 }, () => bead())` inline). The
intermediate "named fixture set" surface gets stuck between the two.

---

## Open questions — to be resolved in scaffold (gm-5v8v.1)

These are intentionally not pinned here because the scaffold ticket
will inherit them. Listed for visibility:

- **Dolt DB lease pool sizing in CI.** Today's plan is ≤2 deep
  workers. Whether to push this to 3–4 depends on Dolt server
  steady-state CPU on the CI runners, which we don't have a
  measurement for yet.
- **Auth fixture shape.** Fake mode runs `auth=open` by default;
  deep mode needs token + cookie path tested. Whether the auth
  tier is its own project axis or a per-spec setup function is a
  judgement call the scaffold will make.
- **Pending-spec discoverability.** `test.fixme` works but doesn't
  link back to the bead that ungates it. We may want a custom
  reporter that prints "5 pending specs ungated by gm-XXX" alongside
  each lane's results. Out of scope for the design but cheap to add
  later.

---

## References

- Epic: gm-5v8v
- Migrated dispatch driver: `testing/e2e/specs/integration/dispatch-chain.spec.ts` (gm-5v8v.15)
- Conformance audit pattern (mirrors the tier idea on the Go side):
  `testing/runner.go`
- ui-spec / surface inventory bead: gm-p27 (closed gate)
