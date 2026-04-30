---
title: "Acceptance test — native + gastown end-to-end builds of a target SPA via beads"
decision: gm-1avi
---

# D15 — Acceptance: native + gastown end-to-end builds of a target SPA via beads

> **Status:** Draft. Implementation deferred. This doc is the contract
> for the implementation epic `gm-root.27` and the 20 child beads under it.
> Any agent picking up a child bead reads this doc to understand the
> contract their bead must honor.
>
> **Decision:** [gm-1avi](../../) (D15)
> **Implementation epic:** [gm-root.27](../../)
> **Sub-decisions:** D16 ([gm-lpcn](../../)), D17 ([gm-xw8a](../../)), D18 ([gm-bvlm](../../)), D19 ([gm-zdik](../../)), D20 ([gm-l38m](../../))

---

## 1. Goal

Implement a headless, fully-autonomous end-to-end acceptance test that:

1. Bootstraps a fresh Gemba project on disk (ephemeral, hermetic).
2. Imports a milestone/epic/bead JSONL pack that drives the agent through three milestones: M1 (scaffolding), M2 (Hello world MVP), M3 (full conversion table).
3. Configures a pool via the SPA's `/settings/pools` editor (Playwright drives the UI).
4. Lets the autodispatch daemon route work to a pool of `acceptance-engineer` agents, who claim beads and complete them autonomously.
5. Injects a synthetic escalation between M2 and M3 to exercise the triage path; resolves it via the SPA escalation surface.
6. Builds and serves the target SPA after each MVP; Playwright validates the rendered output.
7. On any oracle failure, files a bug bead in the gemba rig (NOT the target rig) with full reproduction context.
8. Writes a structured report (JSON + markdown) capturing the run.

Two variants ship together:

- **Native** (default CI; <15 min wallclock with MockAgentRunner): pool reuse via `SessionReady` on a local claude binary.
- **Gastown** (manual / scheduled; opt-in via env): pool reuse via `gt sling` across rigs.

Both variants share the same Playwright spec body, target JSONL pack, oracle, and MockAgentRunner. They differ only in pool scope (`local` vs `<rig>`), server flag (`--orchestration=native` vs `--orchestration=gastown`), and bootstrap surfaces (none vs UI-driven `gt rig create` + `gt polecat create`).

## 2. Why first-class

A real acceptance test that exercises the **full propulsion loop** (operator files beads → daemon dispatches → pool member claims → agent works → bead-done emit → SessionReady → next bead) is the only way to catch regressions in the orchestration layer that unit tests can't see. Today there's:

- A smoke E2E for `/settings/pools` (gm-s47n.18) — UI only.
- Conformance tests for adaptors (Group A–F) — adaptor surface only.
- Per-component vitest — per-component only.

What's missing: a test that exercises a **complete operator-driven build**, end to end, with both native and gastown agent backends. This decision establishes that test.

## 3. Foundation (what exists today)

Recently shipped, all reused:

| Bead | What it ships |
|---|---|
| `gm-root.24` | `gemba newproject` auto-seeds `agents.toml` + personas + CLAUDE.md. |
| `gm-s47n.10` (D13) | Session-pool primitive. |
| `gm-s47n.11` | Native idle lifecycle: `SessionReady` + `RecycleSession`. |
| `gm-s47n.12` | Autodispatch daemon wired into `gemba serve`. |
| `gm-s47n.14` | `/done` slash command emits `gemba-state bead-done` (slash-command path; direct `gt done` still pending `gm-s47n.20`). |
| `gm-s47n.16` | Pool config editor at `/settings/pools` (adaptor-aware). |
| `gm-s47n.17` | RunCommandModal + gt mutation buttons in pool editor (gastown rig/polecat creation via UI). |
| `gm-s47n.18` | Smoke E2E spec for `/settings/pools` editor (selector reference). |
| `gm-e7.12` + `gm-e7.13` | gt-adaptor capability probe + RecycleSession via `gt handoff`. |
| `gm-root.26` | FTUX trio: onboarder CTA gate, session toast + LiveSessionsBadge, pool empty-state helper. (Used as bonus oracle assertions.) |
| `gm-e3.8` | ClaimModel manifest gate + soft-skip on inline races. |

Future enhancement (does not block):

| Bead | Effect when it lands |
|---|---|
| `gm-s47n.20` | Direct `gt done` invocations also emit `bead-done`. Acceptance-engineer persona's done-skill pin to slash command can drop. |

## 4. MockAgentRunner architecture (D16, gm-lpcn)

The CI default. Deterministic. Uses no API tokens, makes no network calls. Tests the orchestration layer, not code-generation quality.

### 4.1 Contract

```typescript
interface AgentRunner {
  run(ctx: Context, beadID: string): Promise<void>;
  recycle(): Promise<void>;
  close(): Promise<void>;
}

interface AgentRunnerFactory {
  create(env: Env): AgentRunner;
}

// Factory returns mock by default; real-claude when env.GEMBA_ACCEPTANCE_REAL_AGENTS=1
```

### 4.2 Templates

The mock matches each claimed bead against a small library of template handlers. Each handler reads bead description frontmatter (`template:`, `testid:`, `files:`) and performs the mechanical work:

| Template | What it does |
|---|---|
| `init-repo` | `git init`, write `package.json`, write minimal `vite.config.ts`. |
| `npm-install` | `npm install` in temp project (uses pre-warmed offline cache). |
| `write-component` | Writes a TSX component matching `testid` from frontmatter. |
| `write-test` | Writes a vitest spec asserting the bead's DoD. |
| `build` | `npm run build`, verifies `dist/index.html` exists. |
| `serve` | `vite preview` on injected port, verifies HTTP 200. |
| `error-then-recover` | Fails on first attempt, succeeds on second (exercises retry). |
| `noop` | No-op for milestone-marker beads. |

After running the template, the mock:

1. Closes the bead via `bd close <id>`.
2. Emits `gemba-state bead-done --bead <id>` (the same emit a real polecat does).

### 4.3 Determinism

- Bounded sleeps (max 200ms per template).
- Pinned random seed via `NewMockAgentRunner({seed: 0xACCEPT})`.
- No network: npm install uses `--offline` against a pre-warmed cache; vite build is local; vite preview is local.

### 4.4 Real-vs-mock factory

A single `AgentRunnerFactory` interface; `NewAgentRunner(env)` returns mock by default, real-claude when `GEMBA_ACCEPTANCE_REAL_AGENTS=1`. The real-claude branch wraps the existing native adapter spawn path — does not duplicate it.

## 5. Target JSONL pack (D17, gm-xw8a)

Three milestones × ~4 task beads each = 12 task beads + 3 epic beads + 3 milestone beads + 2 internal decisions = **~20 beads** in the target project.

### 5.1 Pack files

- `testing/acceptance/temperature-spa/shared/target-jsonl/m1.jsonl` — scaffolding milestone, epic, 3 tasks.
- `testing/acceptance/temperature-spa/shared/target-jsonl/m2.jsonl` — Hello world MVP milestone, epic, 4 tasks.
- `testing/acceptance/temperature-spa/shared/target-jsonl/m3.jsonl` — conversion table milestone, epic, 5 tasks.

### 5.2 M1 — Scaffolding

| Bead | Template | Files |
|---|---|---|
| M1.1 | `init-repo` | `package.json`, `vite.config.ts` |
| M1.2 | `noop` | (config copy: `tsconfig.json`, `index.html`, `src/main.tsx`) |
| M1.3 | `npm-install` | `node_modules/` |

### 5.3 M2 — Hello world MVP

| Bead | Template | testid | Files |
|---|---|---|---|
| M2.1 | `write-component` | `app-root` | `src/App.tsx` |
| M2.2 | `write-test` | `app-root` | `src/App.test.tsx` |
| M2.3 | `build` | — | `dist/` |
| M2.4 | `serve` | — | (preview server) |

### 5.4 M3 — Conversion table

| Bead | Template | testid | Files |
|---|---|---|---|
| M3.1 | `write-component` (pure fn) | — | `src/temperatureRows.ts` |
| M3.2 | `write-component` | `temperature-table`, `row-{c}` | `src/TemperatureTable.tsx` |
| M3.3 | `write-test` | — | `src/TemperatureTable.test.tsx` |
| M3.4 | `write-component` (replaces App.tsx) | `app-root`, `temperature-table` | `src/App.tsx` |
| M3.5 | `build` + `serve` | — | (rebuild + reserve) |

### 5.5 Bead description frontmatter

Every task bead description begins with:

```
template: write-component
testid: temperature-table
files: src/TemperatureTable.tsx
```

`MockAgentRunner` reads this; falls back to keyword match on title; on no match files a synthetic escalation `template_unknown`.

### 5.6 Edges

- Each task `depends-on` its predecessor in the same milestone.
- Each epic `parent_child` to its milestone.
- Each task `parent_child` to its epic.
- M2 epic `depends-on` M1 milestone (so the daemon doesn't dispatch M2 work until M1 closes).
- M3 epic `depends-on` M2 milestone.

## 6. Oracle (D18, gm-bvlm)

### 6.1 Per-milestone gates

**M1 oracle:**

- All M1 beads → closed.
- On disk: `package.json`, `vite.config.ts`, `tsconfig.json`, `index.html`, `src/main.tsx`, `node_modules/`.
- `npm run dev` exits 0 (or starts and listens; SIGTERM after liveness probe).

**M2 oracle:**

- All M2 beads → closed.
- `npm run build` exits 0; `dist/index.html` exists.
- `vite preview` (or equivalent) on injected port responds 200 at `/`.
- Playwright loads the URL, asserts `[data-testid="app-root"]` with textContent `"Hello world"`.
- `npm test` (vitest) passes.

**M3 oracle:**

- All M3 beads → closed.
- Build + preview gates as M2.
- Playwright loads URL.
- `[data-testid="temperature-table"]` exists.
- Exactly **16** `[data-testid^="row-"]` elements.
- For each row, the °F cell equals `(c * 9/5 + 32).toFixed(1)` where `c` is the row's celsius value parsed from the testid.
- Cherry-pick assertions:
  - `row-0` → 32.0
  - `row-100` → 212.0
  - `row-300` → 572.0
- `npm test` (vitest) passes.

### 6.2 Numeric tolerance

Strict-numeric: exact match on integer-valued cells (0, 32, 212, 572). One decimal place tolerance for fractional cells (e.g., `row-20` → 68.0). The oracle compares **formatted strings**, not floats — locale and rounding stay reproducible.

### 6.3 Failure paths

Each failure type files a separate bug bead via `.19` (bug-filing helper):

| Failure | Bug title | Severity |
|---|---|---|
| `npm run build` non-zero | "target build failed" + log tail | CRITICAL |
| Test runner failed | "target tests failed: <spec>" | HIGH |
| Numeric mismatch | "oracle mismatch row-{c}: expected X, got Y" | HIGH |
| Row count mismatch | "row-count: expected 16, got N" | HIGH |
| Missing testid | "missing testid: {testid}" | HIGH |
| Preview server unreachable | "preview server unreachable on port N" | CRITICAL |

### 6.4 FTUX bonus assertions

The test also exercises `gm-root.26` surfaces:

- Pool empty-state helper visible BEFORE pool configured (sanity check; the helper is the cue a fresh-project operator would see).
- Onboarder CTA gates correctly on a project without `[llm]` in `~/.gemba/config.toml`.
- Session toast appears when first session dispatches.
- LiveSessionsBadge increments 0 → 1 on first dispatch and decrements back on completion.

## 7. Pool setup via UI (D19, gm-zdik)

The test does NOT write `pool.toml` directly. It drives Playwright through `/settings/pools` to exercise the editor itself.

### 7.1 Native flow

1. Navigate to `/settings/pools`.
2. Scope dropdown auto-selects `local` (editor hides scope axis on native).
3. Persona dropdown: select `acceptance-engineer`.
4. Set `size = 1`, `floor = 0.5`.
5. Click Save → `PUT /api/pool-config` → server writes `pool.toml`.
6. Restart prompt appears; harness restarts gemba server with new config.

### 7.2 Gastown flow

1. Navigate to `/settings/pools`.
2. Click `+ New rig` → `RunCommandModal` opens.
3. Type rig name (e.g., `acceptance-{run-id}`) → modal runs `gt rig create`.
4. Refresh.
5. Click `+ New polecat` → modal runs `gt polecat create`.
6. Scope dropdown: select new rig.
7. Persona dropdown: select `acceptance-engineer`.
8. Set `size = 1`, `floor = 0.5`.
9. Save → restart server with new pool.toml.

### 7.3 Selector reuse

From `gm-s47n.18` smoke spec:

- `[data-testid="pools-page"]`
- `[data-testid="pool-scope-select"]`
- `[data-testid="pool-persona-select"]`
- `[data-testid="pool-size-input"]`
- `[data-testid="pool-floor-input"]`
- `[data-testid="pool-save-button"]`

Plus `gm-s47n.17`'s additions:

- `[data-testid="pool-new-rig-button"]`
- `[data-testid="pool-new-polecat-button"]`
- `[data-testid="run-command-modal"]`
- `[data-testid="run-command-input"]`
- `[data-testid="run-command-execute"]`

### 7.4 Side benefit

If the editor surface ever ships without these testids, the acceptance test fails fast with a "selector not found" error. The acceptance test is a regression net for the editor itself.

## 8. Native vs gastown variant differences (D20, gm-l38m)

### 8.1 Shared

- Target JSONL pack (D17).
- Playwright spec body (`shared/spec.ts`).
- Oracle (D18).
- MockAgentRunner (D16).
- Pool-via-UI path (D19).

### 8.2 Differences

| | Native | Gastown |
|---|---|---|
| Server flag | `--orchestration=native` | `--orchestration=gastown` |
| Pool TOML scope | `[pool.local.acceptance-engineer]` | `[pool.<rig-name>.acceptance-engineer]` |
| Pre-server bootstrap | none beyond `gemba newproject` | UI-driven `gt rig create` + `gt polecat create` |
| Capability probe | not required | `gt` binary version checked at startup; fail fast if too old |
| Teardown | rm temp dir, free ports | rm temp dir, free ports, UI-driven `gt rig remove` |
| Time budget | <15 min mocked / <90 min real | <30 min mocked / hours real |
| CI viability | ✅ default | ❌ manual / nightly only |
| Pool reuse path | `/done` slash → `bead-done` → `SessionReady` | `/done` slash → `bead-done` → `SessionReady` (same) |

### 8.3 Done-skill pin

Both variants pin the `acceptance-engineer` persona's done-skill to the `/done` slash command (per `gm-s47n.14`). Direct `gt done` would not emit `bead-done` until `gm-s47n.20` lands. Pinning to slash command guarantees pool reuse on both backends today.

### 8.4 Concurrent runs

Each variant runs against an ephemeral Dolt server on a random port. Two acceptance runs (native + gastown) on the same machine concurrently is supported. CI default runs only native; gastown is opt-in via `GEMBA_ACCEPTANCE_RUN_GASTOWN=1`.

## 9. Implementation epic — wave structure

The epic `gm-root.27` has 20 implementation children:

### Wave 1 — Shared core (gm-root.27.1 – .5)

- `.1` Headless agent supervisor (factory: real claude vs mock)
- `.2` MockAgentRunner with bead templates
- `.3` Ephemeral Dolt + project bootstrap helper
- `.4` Target JSONL pack (the actual files committed, per D17)
- `.5` Synthetic escalation injector

### Wave 2 — Shared Playwright spec (gm-root.27.6 – .11)

- `.6` Spec scaffolding (server lifecycle, navigation helpers)
- `.7` Pool config setup driver via `/settings/pools`
- `.8` M1 step
- `.9` M2 step
- `.10` M3 step
- `.11` Triage step (synthetic escalation + UI-resolve)

### Wave 3a — Native variant (gm-root.27.12 – .14)

- `.12` Native variant spec wrapper
- `.13` Native pool fixture
- `.14` Native cleanup

### Wave 3b — Gastown variant (gm-root.27.15 – .17)

- `.15` Gastown variant spec wrapper + UI-driven gt rig + polecat creation
- `.16` Gastown pool fixture template
- `.17` Gastown cleanup

### Wave 4 — Reporting + bug-filing + cleanup orchestration (gm-root.27.18 – .20)

- `.18` Test report writer
- `.19` Bug-filing helper
- `.20` Cleanup orchestration

## 10. Module layout

```
testing/acceptance/temperature-spa/
  shared/
    spec.ts                              # Playwright orchestration body
    helpers/
      bootstrap.ts                       # ephemeral Dolt + gemba newproject
      pool-via-ui.ts                     # drives /settings/pools
      escalation.ts                      # synthetic escalation injector
      cleanup.ts                         # generic cleanup utilities
      report.ts                          # JSON + markdown report writer
      bug-filer.ts                       # files beads in gemba rig
    runner/
      factory.ts                         # AgentRunnerFactory
      mock.ts                            # MockAgentRunner + templates
      real-claude.ts                     # wraps native adapter spawn
    target-jsonl/
      m1.jsonl                           # scaffolding pack
      m2.jsonl                           # Hello world MVP pack
      m3.jsonl                           # conversion table pack
    oracle/
      m1.ts                              # M1 assertions
      m2.ts                              # M2 assertions
      m3.ts                              # M3 assertions
      ftux.ts                            # FTUX bonus assertions
  variants/
    native/
      spec.ts                            # native entry point
      fixtures/
        pool.toml                        # [pool.local.acceptance-engineer]
    gastown/
      spec.ts                            # gastown entry point
      fixtures/
        pool.toml.tmpl                   # template with {{.Rig}} placeholder
      gt-bootstrap.ts                    # UI-driven gt rig + polecat
      gt-teardown.ts                     # UI-driven gt rig remove
  reports/                               # historical run reports
```

## 11. Risks and open questions

### 11.1 MockAgentRunner template drift

If a target JSONL bead's `template:` directive references a non-existent template, the runner files a `template_unknown` synthetic escalation. The implementer of `.4` (target JSONL pack) and `.2` (MockAgentRunner) must keep their template names in lockstep. Mitigation: a unit test in `.4` that loads each JSONL file, extracts every `template:` value, and asserts the runner has a matching handler.

### 11.2 Daemon polling cadence

The autodispatch daemon ticks every 10s. Each milestone has 3–5 beads with depends-on chains; per bead is at minimum 10s wait + ~200ms mock work + bead-close + recycle. M3's 5 beads serially is ~50s minimum. Three milestones is ~150s minimum + Playwright overhead + build/serve time. Total: ~5–10 min mocked. Fits the <15 min budget.

### 11.3 Real-claude path stability (deferred)

The real-claude path is opt-in and not validated in this epic. Expect rate-limit handling, retry on session crash, and longer timeouts in the follow-up epic.

### 11.4 Gastown rig name collisions

Each gastown run generates a unique rig name (e.g., `acceptance-{run-id}` where run-id is a ulid). Cleanup removes it. If cleanup fails (test process panic), an orphaned rig sits in `gt`. The `.20` cleanup orchestrator includes a "purge stale acceptance rigs" step that runs at startup of every test run; rigs older than 24h get removed.

### 11.5 Gemba-server restart cost

Pool config save requires a server restart (per `gm-s47n.16` — hot-reload deferred). The harness orchestrates the restart cleanly. Restart adds ~5s per variant per run.

### 11.6 Escalation respond UI completeness

Per the survey, the escalation respond UI is partially shipped: backend handlers ready, frontend surface exists at `/escalations` but full approve/deny button wiring may be incomplete. The triage step (`.11`) probes for the UI; on missing button, falls back to direct API call and files a bead noting "escalation respond UI incomplete." The acceptance test thus also serves as a regression net for that surface.

### 11.7 Concurrent test runs on shared Dolt

The deep-mode E2E gating issue (`gm-h4n`) — bd-init colliding on the shared Dolt server — is solved by the ephemeral-Dolt helper (`.3`). Each acceptance run gets its own Dolt instance on a random port. CI parallelism is supported.

### 11.8 npm offline cache

MockAgentRunner's `npm-install` template uses `--offline`. The cache is pre-warmed at test setup time (one-time `npm install` run during the harness's first invocation, cached at `~/.cache/gemba-acceptance-npm/`). If the cache is missing or stale, the test falls back to online install (slower but correct).

## 12. Acceptance criteria for this decision

D15 is **ratified** when:

- This doc exists and links back to `gm-1avi`.
- Implementation epic `gm-root.27` filed with all 20 children.
- Sub-decisions D16–D20 filed.

D15 is **rejected** when:

- The operator decides the acceptance test is not worth the maintenance cost (e.g., flake budget too high).
- A superseding decision is filed with a `supersedes:gm-1avi` edge.

Until either resolution, status is **draft**.

## 13. References

- D6 (`gm-d1m1`) — Decision-capture convention
- D13 (`gm-s47n.13`) — Session pool primitive
- D14 (`gm-thq1`) — Channel-bridge architecture (reuses some patterns: identity, persistence, pluggability)
- DD-16 (`gm-ege`) — External-consumer optionality
- `internal/cli/newproject.go` — project bootstrap
- `internal/server/newproject_ratify.go` — atomic ratification
- `internal/adapter/native/` — real-claude spawn path
- `internal/adapter/gastown/` — gt sling path
- `internal/planner/` — claim index + dispatch policy
- `internal/server/escalations.go` — escalation respond
- `web/src/pages/PoolsPage.tsx` — pool editor (reference)
- `testing/e2e/` — existing Playwright infrastructure to mirror
- `gm-s47n.18` — smoke E2E spec (selector reference)
