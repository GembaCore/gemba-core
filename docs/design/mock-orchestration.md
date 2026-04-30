---
title: "Mock orchestration adaptor — first-class plane for dry-run + headless acceptance"
decision: gm-x4i9
---

# D21 — Mock orchestration adaptor

> **Status:** Draft. Implementation deferred. This doc is the contract
> for the implementation epic `gm-root.28` and its 10 children.
>
> **Decision:** [gm-x4i9](../../) (D21)
> **Implementation epic:** [gm-root.28](../../)

---

## 1. Goal

Land a Go mock orchestration adaptor that:

- Conforms to `core.OrchestrationPlaneAdaptor` exactly like `native` and `gastown`.
- Mints `Session` objects, transitions through the full `Spawning → Working → SessionReady` lifecycle (per gm-s47n.10 / .11 / .12).
- Routes claimed beads through a Go port of the 8-template library originally written in TypeScript for `gm-root.27.2`.
- Is **available to operators** as `gemba serve --orchestration=mock` — not gated behind an env flag.

Two consumers:

1. The `gm-root.27` acceptance test, which today fails because no agent picks up its dispatched beads. After this epic, `pnpm test:native` actually closes M1/M2/M3 and the strict-numeric oracle gates real correctness.
2. **Operators** running a workflow as a "dry-run" — exercise the dispatch loop, verify pool config + bead routing + lifecycle, without spending API tokens on real claude sessions.

## 2. Why first-class

The acceptance test reveals the architectural gap: `MockAgentRunner` exists in TS today, has 18 unit tests, but is never invoked. The autodispatch daemon dispatches via `OrchestrationPlane.StartSession`. Only `native` is registered. The TS runner has no path into the dispatch loop.

Three rejected alternatives:

- **Test-only watcher**: a TS goroutine that polls for ready beads and runs `MockAgentRunner` directly, bypassing the daemon. Tests dispatch in a way no operator runs it; defeats the purpose of an integration test.
- **Test-only env-gated plane**: like `gm-root.27.22`'s `GEMBA_ENABLE_TEST_ESCALATIONS` pattern. Adds a feature flag for marginal scope reduction; mock-mode is a real product surface (dry-run) so the gate is wrong.
- **Sidecar bridge to TS**: keep the runner in TS, have a Go plane shell out to a sidecar process. Adds IPC for nothing; the templates port cleanly to Go.

The first-class adaptor + Go runner is the simplest answer that preserves both production-path coverage and operator usability.

## 3. Constraints carried forward

- **D15 §11.3** — real-claude path stays deferred. This epic doesn't touch the existing `native` adaptor; it adds `mock` alongside.
- **D16** — the 8 templates (init-repo, npm-install, write-component, write-test, build, serve, error-then-recover, noop) and the per-file content registry transfer verbatim.
- **D17** — the target JSONL pack already names templates that the mock will execute.
- **gm-s47n.10/.11/.12** — `SessionReady` recycling is the production warmth path. The mock plane MUST exercise it (full lifecycle parity), not shortcut it.
- **gm-e3.8** — claim model + soft-skip on inline races. The mock plane gets convergent concurrency for free; the conflict graph arbitrates.

## 4. Architecture

### 4.1 Plane shape

```
internal/adapter/mock/
  plane.go             # OrchestrationPlane type; conforms to core.OrchestrationPlaneAdaptor
  start.go             # StartSession lifecycle (Spawning → Working → SessionReady)
  recycle.go           # RecycleSession — clean state check + return to ready
  end.go               # EndSession — final cleanup
  list.go              # ListSessions / ListPendingRequests
  runner.go            # RunBead(ctx, beadID, projectDir) — the work
  templates.go         # 8 handlers + per-file content registry
  frontmatter.go       # parser (port of shared/runner/frontmatter.ts)
  *_test.go
```

Constructor: `mock.NewOrchestrationPlane(cfg Config)` returns the plane.

`internal/cli/serve.go` registers it via a new switch case:

```go
case "mock":
    return registerMockOrchestration(ctx, host, cfg)
```

### 4.2 Session lifecycle (full parity)

```
StartSession(spec) →
   1. Mint Session{ID, PaneID: "mock-<random>", Status: Spawning}
   2. Resolve bead via WorkPlane.GetWorkItem(beadID)
   3. Transition: Spawning → Working (StatusReason: "running template <name>")
   4. RunBead(ctx, beadID, projectDir):
        a. Parse frontmatter from bead description
        b. Look up template handler (init-repo / write-component / ...)
        c. Execute handler against projectDir
        d. bd close <id>
        e. gemba-state bead-done --bead <id> (best-effort)
   5. Transition: Working → SessionReady
   6. Persist session in the in-memory map; daemon's ListSessions sees it as
      pool-eligible.

RecycleSession(sessionID) →
   1. Verify projectDir is in a clean state (no leftover work).
   2. Re-emit SessionReady (no-op if already there).
   3. Return.

EndSession(sessionID) →
   1. Remove from map.
   2. No process to kill (mock has no pane).
```

The daemon's view is identical to native: `SessionReady` members are reused across beads; the warmth path exercises every node. The only invisible-to-the-daemon difference is that `Working` transitions take milliseconds instead of minutes.

### 4.3 Concurrency (convergent)

Per D21, mock plane allows N parallel sessions. With `size=2+` in pool.toml:

- Daemon picks 2 ready beads on the same tick.
- Issues 2 concurrent `StartSession` calls.
- Both sessions enter `Working` simultaneously, each on a separate goroutine.
- Each calls `RunBead` against its assigned bead — operations are file-write + shell-out, safe under concurrency on different files.
- Conflict graph (`gm-e3.8`) arbitrates if two beads target the same files. The mock plane doesn't need to participate in conflict-graph logic; the daemon owns that.

The acceptance test's strict-numeric oracle (16-row table) catches correctness regardless of bead-close ordering.

### 4.4 Operator-mode caveats

`gemba serve --orchestration=mock` is documented as a **dry-run mode**:

| What it does | What it doesn't do |
|---|---|
| Exercises pool config, dispatch, claim index, SessionReady recycling, the gemba-state bead-done bridge | Run real claude sessions |
| Closes beads via the bd binary | Produce real code-gen quality |
| Honors the same `pool.toml` shape (`[pool.<scope>.<persona>]`) | Connect to the Anthropic API |
| Surfaces escalations, sessions, work-items via the SPA | Validate that beads' DoD is met (templates do mechanical work, not LLM reasoning) |

A README warning section makes this clear so operators don't accidentally rely on mock-mode for production workflows.

## 5. Template port (TS → Go)

The 8 templates live in TS today. Porting is mechanical:

| Template | TS body | Go port |
|---|---|---|
| `init-repo` | `git init` + write package.json + write vite.config.ts | identical with `os/exec` for git, `os.WriteFile` for files |
| `npm-install` | `npm install --offline` (fallback online) | identical with `os/exec` |
| `write-component` | look up file path in registry, write file | identical with `os.WriteFile` and a Go map registry |
| `write-test` | shares write-component path | identical |
| `build` | `npm run build` | identical |
| `serve` | no-op (preview owned by step bead) | identical |
| `error-then-recover` | first call throws, second succeeds | identical with a package-scoped counter |
| `noop` | write declared files from registry | identical |

The per-file content registry is hand-tuned to the D17 contract + D18 oracle. Go gets the same map verbatim.

The TS unit tests (18 cases) port to Go test cases (`*_test.go`). Same structure, same assertions, same fixtures.

## 6. Acceptance test integration

After the plane lands, the variant wrapper changes one line:

```ts
// variants/native/spec.ts
serveArgs: ['--orchestration=mock', '--pool-config', POOL_CONFIG],
//                            ^^^ was 'native'
```

The TS `MockAgentRunner` + `factory.ts` + `templates.ts` + `mock.ts` are removed. The TS test files (`templates.test.ts`, `frontmatter.test.ts`) are removed since the Go ports cover the same surface.

`pnpm test:native` then exercises:

1. bootstrapProject spins gemba with `--orchestration=mock`.
2. Mock plane registers; daemon ticks; pool warmth engages.
3. M1 imported → mock claims beads → templates execute → bd close → SessionReady → next bead.
4. Same for M2, M3. Triage between M2 and M3.
5. Strict-numeric oracle on the served SPA: 16 rows, °F = °C × 9/5 + 32 to one decimal.
6. AcceptanceFailure thrown only if any step actually failed.

Expected wall-clock: ~2-5 minutes for the full run (down from the current 16+ minutes of pure timeout-waiting).

## 7. What the TS runner becomes

After this epic lands, the TS code at `testing/acceptance/temperature-spa/shared/runner/` is dead. Two options:

- **Remove entirely.** The Go port is the source of truth; the TS files come down in `gm-root.28.8`'s commit.
- **Keep as a doc fixture.** `runner/README.md` says "see internal/adapter/mock/ — this directory is preserved for historical reference." The 18 TS unit tests stay green; they're a parallel correctness check.

Default: **remove.** Two implementations of the same templates is a maintenance burden; the Go port has the production-path coverage.

## 8. Risks and open questions

### 8.1 npm offline cache contamination

The mock's `npm-install` template uses `--offline`. If the cache is corrupted, the test silently degrades to online install. A unit test should pre-warm the cache during the harness's first invocation and assert it's present before any milestone runs.

### 8.2 Concurrent file writes

With size=N parallel sessions, two `write-component` templates could target the same file (the M3.4 "replace App.tsx" beads from different concurrent runs of the same test). The conflict graph + claim-index single-assignee guarantee should prevent this — the same bead can't be claimed twice. Verify by stress-test.

### 8.3 Template determinism vs convergent dispatch

`error-then-recover` has a package-scoped counter. With concurrent sessions, both might race the counter. Either:

- The counter is per-session (state lives on the Session object, not package-scoped).
- The template's contract relaxes: "throws on the FIRST attempt of a particular bead id, succeeds thereafter."

Default: **per-bead**. Cleaner contract, no shared mutable state.

### 8.4 SessionReady contract under mock

The native plane's `SessionReady` requires a clean worktree (gm-s47n.11). The mock has no worktree per se — it writes to projectDir directly. Either:

- The recycle check is a no-op for mock (the contract doesn't apply).
- The recycle check verifies projectDir is in a known-clean state (e.g., no untracked files in places only the prior bead would have written).

Default: **no-op recycle for mock**. The mock isn't simulating worktree dirty-state; that's a real-claude concern.

### 8.5 Operator confusion

A first-time operator running `gemba serve --orchestration=mock` on their real project might not realize their beads close without real work happening. Mitigations:

- Server log line on every mock-dispatch: `mock: closing bead <id> via template <name> (no real work performed)`.
- README warning prominently in the autonomous-dispatch quickstart.
- `mock` adaptor reports a `dry_run: true` flag in `/api/capabilities` so the SPA can show a banner.

The banner work is out of scope for this epic; document as a follow-up.

## 9. Module layout (target)

```
internal/adapter/mock/
  plane.go
  start.go
  recycle.go
  end.go
  list.go
  runner.go
  templates.go
  frontmatter.go
  plane_test.go
  templates_test.go
  frontmatter_test.go
  runner_test.go
internal/cli/
  serve.go               (modified — adds 'mock' case)
docs/getting-started/
  autonomous-dispatch.md (modified — adds Mock mode section)

testing/acceptance/temperature-spa/
  shared/runner/         (REMOVED in .8)
  shared/runner/*.test.ts (REMOVED in .8)
  variants/native/spec.ts (modified — serveArgs uses --orchestration=mock)
```

## 10. Acceptance criteria for this decision

D21 is **ratified** when:

- `docs/design/mock-orchestration.md` exists and links to `gm-x4i9`.
- `gm-root.28` filed with all 10 children.
- The native acceptance variant passes end-to-end after `gm-root.28.8` lands (flakes acceptable per convergent ordering; correctness gated by the oracle).

D21 is **rejected** when:

- The operator decides mock-mode shouldn't be a peer adaptor.
- A superseding decision is filed with a `supersedes:gm-x4i9` edge.

## 11. References

- D15 (`gm-1avi`) — Acceptance test architecture
- D16 (`gm-lpcn`) — MockAgentRunner architecture (the TS predecessor)
- D17 (`gm-xw8a`) — Target JSONL pack
- D18 (`gm-bvlm`) — Acceptance oracle
- gm-s47n.10 — Session pool primitive
- gm-s47n.11 — Native idle lifecycle (SessionReady contract)
- gm-s47n.12 — Autodispatch daemon
- gm-e3.8 — Claim model + soft-skip on inline races
- gm-root.27 — Acceptance test epic (the consumer)
- gm-root.27.35 — Gastown adaptor wiring (sister fix to .3 here)
- internal/adapter/native/ — Reference implementation pattern
