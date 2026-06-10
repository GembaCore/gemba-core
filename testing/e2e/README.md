# gemba-e2e

Playwright e2e test library for the Gemba SPA. Two backends, seven
tiers, one project-matrix in `playwright.config.ts`.

> Owning epic: **gm-5v8v** (E2E test library). Scaffold landed by
> **gm-5v8v.1**. Sibling beads fill in the remaining tiers.

## Layout

```
testing/e2e/
  playwright.config.ts        # tier × backend project matrix
  fixtures/
    server.ts                 # backend = 'fake' | 'real' switch
    workplane.ts              # WorkPlane seed fixture (stub → gm-5v8v.5/6)
    auth.ts                   # token + cookie fixture (stub → gm-5v8v.12)
    modes.ts                  # workspace mode fixture (stub → gm-5v8v.11)
    sse.ts                    # SSE emitter (stub → gm-5v8v.10)
  pages/
    AppShell.ts               # POM base — shell assertions
  helpers/
    waits.ts                  # idle waits (small, real)
    dragdrop.ts               # @dnd-kit pointer choreography (stub → gm-5v8v.5)
    hotkeys.ts                # 33-chord helper (stub → gm-5v8v.4)
  specs/
    smoke/                    # gm-5v8v.3   — routes / axe / instance_id
    chrome/                   # gm-5v8v.4   — sidebar/topbar/palette/hotkeys/banner
    board/                    # gm-5v8v.5
    grid/                     # gm-5v8v.6
    graph/                    # gm-5v8v.7
    drawers/                  # gm-5v8v.8
    newproject/               # /onboard setup + ratify route coverage
    sessions/                 # gm-5v8v.9
    realtime/                 # gm-5v8v.10
    modes/                    # gm-5v8v.11
    auth/                     # gm-5v8v.12
    error/                    # gm-5v8v.13
    pending/                  # gm-5v8v.14  — living specs for unbuilt UI
    integration/              # gm-5v8v.15  — the migrated hello-world script
```

## Backends

| Backend | What it is | When it runs |
|---|---|---|
| **fake** | `page.route()` intercepts `/api/**` + `/events` with empty-state JSON and an idle SSE stream. Sub-second resets. Parallelizes freely. No Go binary, no Dolt. | Every PR. |
| **deep** | Real `gemba serve` + bd + per-worker tempdir-isolated embedded Dolt. One server per worker; tests reset bead state between runs. Serialized to ≤2 workers. | Merge / nightly. Landed by **gm-5v8v.2**. |

The fake backend is the default for `pnpm test`. Use the deep
matrix when a spec asserts on real backend behaviour (writes, SSE
round-trips, Dolt reads):

```bash
make build                                 # produces bin/gemba
pnpm --filter gemba-e2e test:smoke:deep    # smoke-deep only
pnpm --filter gemba-e2e test:deep          # every @deep spec across the matrix
```

Per-worker isolation: each Playwright worker gets its own
`mkdtemp` directory under `$TMPDIR`. Inside it, `bd init --prefix
e2e<worker>` creates a fresh `.beads/` with an embedded Dolt
engine local to that directory, then `gemba serve --project-dir
<td>` boots against it on a free port. Teardown rms the tempdir.
The shared `:3307` Dolt server is never touched, so deep-mode
testing can't generate orphan databases.

## Tiers and projects

`playwright.config.ts` enumerates the full matrix up-front. Most projects
`testIgnore: ['**/*']` until their child bead replaces the entry. This
keeps the matrix visible without false-greens.

All projects in the table below are **active** — they pick up real
spec files. Deep projects gate on `GEMBA_E2E_RUN_DEEP=1` until the
upstream bd isolation fix lands (gm-h4n); without that flag they
appear as `pending('...')` placeholders.

| Project | Tier | Backend | Owning bead |
|---|---|---|---|
| `smoke-fake` | smoke | fake | gm-5v8v.3 |
| `smoke-deep` | smoke | real | gm-5v8v.3 (gated) |
| `chrome-fake` | chrome | fake | gm-5v8v.4 |
| `route-fake` | route | fake | gm-5v8v.5/6/8/9 + onboarding route specs |
| `route-deep` | route | real | gm-5v8v.5/6/8/9 + onboarding setup (gated) |
| `realtime-fake` | realtime | fake | gm-5v8v.10 |
| `realtime-deep` | realtime | real | gm-5v8v.10 (gated) |
| `modes-fake` | modes | fake | gm-5v8v.11 |
| `modes-deep` | modes | real | gm-5v8v.11 (gated) |
| `auth-fake` | auth | fake | gm-5v8v.12 |
| `auth-deep` | auth | real | gm-5v8v.12 (gated) |
| `error-fake` | error | fake | gm-5v8v.13 |
| `error-deep` | error | real | gm-5v8v.13 (gated) |
| `integration-deep` | integration | real | gm-5v8v.15 (gated) |
| `gastown-deep` | gastown | real | optional local adaptor lifecycle (gated separately) |

`chrome-deep` is intentionally absent — chrome specs are pure SPA-shell
assertions; the AdaptorBanner integration that *would* matter against a
real backend is already covered by `realtime-deep`. Removed in gm-5v8v.18.

`newproject/` has both fake and deep coverage. Fake route specs drive
the deterministic setup pane, canned Onboarder conversation, ratify
modal, and handoff screen without a real LLM. The `@deep` setup spec
calls `POST /api/v1/onboarding/setup` against a real `gemba serve` and
asserts the native worktree guidance files are written before any LLM
session is launched.

## Tag convention

Tags are **specifically additive** — Playwright `--grep`s on them.

| Tag | Meaning |
|---|---|
| `@smoke` | Routes load + AppShell renders. Zero backend assertions. |
| `@chrome` | Sidebar / Topbar / palette / hotkeys / adaptor banner. |
| `@drag` | Exercises drag-drop. Requires the dragdrop helper. |
| `@realtime` | Asserts SSE → react-query invalidation. |
| `@auth` | Exercises login / middleware enforcement. |
| `@mode-managed` | Asserts the managed-mode confirmation UX. |
| `@deep` | **Asserts on real backend behaviour** — writes, SSE round-trips, Dolt reads. The deep-* projects grep this. Spec is *not* tagged `@deep` if it only renders the SPA shell. |
| `@gastown` | Optional Gas Town adaptor lifecycle. Runs only under `gastown-deep`, not under regular deep or CI lanes. |

## Running

From the workspace root:

```bash
pnpm install                       # one-time; pulls @playwright/test
pnpm --filter gemba-e2e exec playwright install chromium  # one-time
pnpm --filter gemba-e2e test       # smoke-fake only (the DoD)
pnpm --filter gemba-e2e test:all   # full matrix (most projects ignore everything for now)
pnpm --filter gemba-e2e typecheck
```

Optional Gas Town adaptor check:

```bash
make build
pnpm --filter gemba-e2e test:gastown
```

`test:gastown` sets `GEMBA_E2E_GASTOWN=1` and runs only the
`gastown-deep` project. The current spec starts a real `gemba serve`
with `--orchestration=gastown`, creates an isolated Beads workspace,
dispatches a bead through `gt sling`, watches `/api/sessions` transition
from working to completed, and confirms the underlying bead closes. It
uses a deterministic `gt` shim so the adaptor can be regression-tested
without launching a real LLM. This lane is deliberately absent from
commit hooks and GitHub Actions.

From this directory:

```bash
pnpm test                          # smoke-fake
pnpm test:smoke                    # alias of test
pnpm test -- --grep "/board"       # single-route smoke
pnpm exec playwright test --ui     # UI mode
pnpm report                        # open last HTML report
```

### Pointing at an already-running dev server

```bash
GEMBA_E2E_NO_WEBSERVER=1 GEMBA_E2E_BASE_URL=http://localhost:5173 pnpm test
```

## CI lanes

`.github/workflows/e2e.yml` runs three lanes against the project
matrix above. Each lane targets a budget so PR review stays fast and
the deeper coverage runs where it can afford to. Source of truth for
the lane wiring is the workflow file; this table documents intent.

| Lane | Trigger | Projects | Budget |
|---|---|---|---|
| `pr-fast` | `pull_request` | `smoke-fake` + `chrome-fake` + `route-fake` + `smoke-deep` | ≤ 5 min |
| `merge` | `push` to `main` | every fake project + `smoke-deep` / `chrome-deep` / `route-deep` / `realtime-deep` | ≤ 15 min |
| `nightly` | `schedule` 06:00 UTC | full matrix (all fake + every deep + `integration-deep`) | ≤ 30 min |
| `dispatch` | `workflow_dispatch` | operator-chosen `inputs.project` (or full matrix when empty) | ≤ 30 min |

Notes:

- All lanes pre-build `bin/gemba` (deep tier needs it) and cache the
  Playwright browser binary under `~/.cache/ms-playwright` keyed off
  `pnpm-lock.yaml`.
- `GEMBA_E2E_RUN_DEEP=1` is set in CI so the `smoke-deep` project
  flips from `pending(...)` to its real configuration. CI runners
  are ephemeral so the gm-h4n bd-init pollution risk doesn't bite —
  see the comment in `playwright.config.ts`.
- HTML report is uploaded as a per-lane artifact (`playwright-report-<lane>`).
  Trace + video + screenshot dirs upload only on failure
  (`playwright-traces-<lane>`, retention 7-14 days).
- `dispatch` accepts a `project` input (single project name) and a
  `run_deep` boolean. Use it to re-run a single project against a
  ref without waiting for the full lane.
- Pending deep projects (`chrome-deep`, `route-deep`, `realtime-deep`)
  are listed in the `merge` lane today but `testIgnore` everything
  until their child bead activates them. Naming them now avoids a
  workflow edit at activation time.

## Selector strategy

Prefer existing `data-testid` attributes — the SPA already ships ~179
of them. ARIA roles + accessible names are the second choice. CSS
class selectors are forbidden; they churn with Tailwind.

## Adding a spec

1. Find the right tier directory under `specs/`.
2. Import `{ test, expect }` from `../../fixtures/server`.
3. Tag the test (`@chrome`, `@drag`, `@deep`, …).
4. Use `pages/AppShell.ts` as a base for any route POM you need.
5. If it asserts on backend behaviour, add `@deep` so the deep
   matrix grep filter picks it up.

## Out of scope for gm-5v8v.1

Real-backend fixture, per-route specs, CI lanes, hello-world
migration — all owned by sibling beads under gm-5v8v.
