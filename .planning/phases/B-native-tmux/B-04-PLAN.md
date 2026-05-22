---
phase: B-native-tmux
plan: 04
type: execute
wave: 4
depends_on: [B-01, B-02, B-03]
files_modified:
  - internal/adapter/native/session_io_integration_test.go
  - internal/adapter/native/testhelpers_tmux_test.go
autonomous: true
requirements: [NATIVE-01, NATIVE-02, NATIVE-04, NATIVE-05]

must_haves:
  truths:
    - "An integration test spawns a real tmux pane, sends input, reads the echo back through StreamSession, and tears down cleanly."
    - "Multi-subscriber fan-out works against a real tmux backend (two subscribers see the same bytes from one pipe-pane)."
    - "A 50-attach-detach storm against a real tmux session leaves zero gemba-stream-*.fifo files in $TMPDIR."
    - "Signal dispatch (SIGINT mapped to C-c) interrupts a real foreground process in the pane."
    - "All integration tests skip cleanly when tmux is not on $PATH (CI without tmux stays green)."
  artifacts:
    - path: "internal/adapter/native/session_io_integration_test.go"
      provides: "Build-tag-guarded integration suite covering echo round-trip, fan-out, storm, signals"
      contains: "//go:build integration"
    - path: "internal/adapter/native/testhelpers_tmux_test.go"
      provides: "Shared helpers: spawn a fresh tmux pane bound to a temp socket, register cleanup, build an OrchestrationPlane with a real backend"
      contains: "func spawnIntegrationPane"
  key_links:
    - from: "integration test"
      to: "real tmux server (isolated -L socket)"
      via: "exec.LookPath('tmux') gate + t.Skipf when missing"
      pattern: "exec.LookPath.*tmux"
---

<objective>
Prove Phase B end-to-end against a real tmux server. Three of the four
ROADMAP success criteria for this phase are integration-shaped (live
echo round-trip, multi-subscriber fan-out, disconnect storm with zero
FIFO leaks); they cannot be fully discharged by the unit suites in
Plans 01-03. This plan adds a build-tag-guarded integration suite that
spawns its own tmux server on a temp socket, exercises the full
SessionIO trio, and asserts the success criteria directly.

Purpose: Phase B is done iff a real tmux pane can be driven through
the core interface. After this plan ships, Phase C (HTTP transport)
can be built on a backend known to satisfy the contract under real
conditions, not just under mocks.

Output: One integration test file + one test helper file, both guarded
by `//go:build integration`. CI runs them when invoked with
`go test -tags integration ./internal/adapter/native/...`; default
`go test ./...` skips them.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/B-native-tmux/CONTEXT.md
@.planning/phases/B-native-tmux/B-01-PLAN.md
@.planning/phases/B-native-tmux/B-02-PLAN.md
@.planning/phases/B-native-tmux/B-03-PLAN.md
@core/session_io.go
@internal/adapter/native/orchestration.go
@internal/adapter/native/peek_test.go
@internal/adapter/native/backend/tmux.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Integration test helpers + suite (echo + fan-out + storm + signal)</name>
  <bead>gm-v01.3.6</bead>
  <files>internal/adapter/native/testhelpers_tmux_test.go, internal/adapter/native/session_io_integration_test.go</files>
  <action>
    Create both files with the `//go:build integration` build tag on line 1 (mandatory — without the tag the default test runner picks them up and breaks CI without tmux).

    HELPERS (testhelpers_tmux_test.go):
    - `func requireTmux(t *testing.T) string` — `exec.LookPath("tmux")`; `t.Skipf("tmux not on PATH: %v", err)` on miss; returns the resolved path. Mirrors the pattern in peek_test.go.
    - `func newIntegrationTmux(t *testing.T) *backend.Tmux` — constructs a `*backend.Tmux` bound to an ISOLATED tmux server: use a temp socket via `tmux -L gemba-int-<rand>` so the test does not collide with any tmux server the developer is already running. The existing `NewTmux()` constructor doesn't take a socket name; either extend it with `NewTmuxWithSocket(socket string) (*Tmux, error)` (preferred — also useful for future Phase F isolation tests) OR set `TMUX_TMPDIR` to `t.TempDir()` before constructing and document the choice. Register cleanup: `t.Cleanup(func(){ exec.Command(tmuxPath, "-L", socket, "kill-server").Run() })`.
    - `func newIntegrationPlane(t *testing.T) (*OrchestrationPlane, string)` — builds an OrchestrationPlane with the integration tmux backend, spawns a pane running `sh -c "cat"` (echoes stdin to stdout — ideal for round-trip), inserts a synthetic `core.Session{ID: "test-<rand>", ProviderMetadata: {"pane_id": <paneID>}, Status: SessionReady}` into `o.sessions` (bypass StartSession — the full StartSession path has worktree provisioning we do not need here). Returns the plane + the sessionID. t.Cleanup calls `o.Close()` then the backend.Kill on the pane.

    INTEGRATION SUITE (session_io_integration_test.go):

    Test 1 — `TestIntegrationEchoRoundTrip`:
      - Setup: plane + sessionID via helper.
      - Attach: `events, err := plane.StreamSession(ctx, sessionID)`.
      - Consume first event (snapshot); assert Kind="output" (bytes can be empty — pane is fresh).
      - Send: `plane.SendInput(ctx, sessionID, SessionInput{Mode: InputLiteral, Keys: "hello-gemba\n"})`.
      - Wait up to 3s for an event whose Bytes contain "hello-gemba". Use a deadlined select loop, not a sleep.
      - Cancel ctx; assert channel closes within 1s.

    Test 2 — `TestIntegrationMultiSubscriberFanOut`:
      - Setup: plane + sessionID.
      - Attach subscriber A; consume snapshot.
      - Attach subscriber B (same sessionID); consume snapshot.
      - Send one literal input "shared-bytes\n".
      - Assert: BOTH A and B receive an event containing "shared-bytes" within 3s.
      - Assert internally: only one FIFO file under `$TMPDIR/gemba-stream-*` (`filepath.Glob`).
      - Cancel A's ctx; wait 200ms; assert FIFO still exists, B still receives bytes when another input is sent.
      - Cancel B's ctx; wait 200ms; assert `filepath.Glob` returns 0 files.

    Test 3 — `TestIntegrationDisconnectStorm`:
      - Setup: plane + sessionID.
      - Baseline: snapshot existing `gemba-stream-*` files (other developer sessions might be running — record the baseline set, only assert the delta is empty).
      - Storm: 50 iterations of (Attach with a 100ms ctx, read at most one event, ctx auto-cancels). Run sequentially so the test is deterministic; the unit hub test (Plan 02) already covered the parallel-stress angle.
      - Settle: 500ms.
      - Assert: the delta vs baseline is exactly zero files matching `gemba-stream-*` for this sessionID's pane (the FIFO names are derived from the pane id; only count those).
      - Assert goroutine count is within ±5 of baseline.

    Test 4 — `TestIntegrationSignalDispatch`:
      - Setup: plane + sessionID but spawn the pane running a real foreground process that ignores SIGTERM but exits on SIGINT — easiest: `sh -c "trap 'echo INT-RECEIVED; exit 0' INT; while true; do sleep 0.1; done"`.
      - Wait briefly (~200ms) for the script to install the trap.
      - Attach subscriber; consume snapshot.
      - SendInput with Mode=InputSignal, Keys="SIGINT".
      - Wait up to 3s for an event whose Bytes contain "INT-RECEIVED".
      - Cancel ctx; cleanup runs.

    Test 5 — `TestIntegrationKeysMode`:
      - Setup: plane + sessionID running `sh -c "while read line; do echo got:[$line]; done"`.
      - SendInput literal "ping" then SendInput keys "Enter".
      - Assert: receive event containing "got:[ping]" within 3s.
      - Covers the InputKeys named-key path (Enter) under real tmux.

    Test 6 — `TestIntegrationResizeNoOp`:
      - Setup: plane + sessionID.
      - Call `plane.ResizeSession(ctx, sessionID, 120, 40)`.
      - Assert: returns nil.
      - Assert pane is still alive (Backend.CapturePane succeeds).
      - This documents the no-op contract from CONTEXT.md scope §2 with an executable check.

    All tests use `context.WithTimeout(t.Context(), 10*time.Second)` as their top-level ctx so a wedged tmux can't hang CI.

    Implements bead gm-v01.3.6.
  </action>
  <verify>
    <automated>cd /Users/mikebengtson/Documents/GitHub/gemba && go test -tags integration ./internal/adapter/native/... -count=1 -timeout 90s -run Integration && go test ./internal/adapter/native/... -count=1 && go test ./core/... -count=1 && go build ./...</automated>
  </verify>
  <done>
    All 6 integration tests pass on a host with tmux installed AND skip cleanly on a host without tmux. Default `go test ./...` invocation (no `-tags integration`) does NOT pick up the new files (they have the build tag). Unit tests from Plans 01-03 unaffected. Opacity guard still green. `gemba-stream-*.fifo` files cleaned up after all integration tests run.
  </done>
</task>

</tasks>

<verification>
- With tmux installed: `go test -tags integration ./internal/adapter/native/... -run Integration -count=1 -timeout 90s` exits 0.
- Without tmux: same command exits 0 with all 6 tests reported as SKIP.
- Without `-tags integration`: `go test ./internal/adapter/native/... -count=1` exits 0 and reports 0 of the new tests run (they are excluded by the build tag).
- After the suite finishes: `ls $TMPDIR/gemba-stream-*.fifo 2>/dev/null | wc -l` reports a count <= the pre-suite baseline (no net leak).
- `go test ./core/... -run TestSessionIDOpacity -count=1` still green.
</verification>

<success_criteria>
1. Echo round-trip succeeds end-to-end through the core interface against real tmux.
2. Two subscribers share one pipe-pane (verified by FIFO file count == 1 while both attached).
3. 50-cycle disconnect storm leaves zero leaked FIFO files for the test's pane.
4. SIGINT mode interrupts a real foreground process via tmux C-c translation.
5. Resize is a no-op that returns nil and leaves the pane intact.
6. The full Phase B ROADMAP success-criteria checklist (1-4 + "go build ./... + go test ./internal/adapter/native/... green") is now executable and green.
7. Bead gm-v01.3.6 ready to close after this plan ships.
</success_criteria>

<output>
After completion, create `.planning/phases/B-native-tmux/B-04-SUMMARY.md` capturing:
- The exact `-tags integration` invocation CI must run to exercise the suite.
- Whether `NewTmuxWithSocket` was added or `TMUX_TMPDIR` was used (record the choice).
- Final FIFO-leak count from the storm test (must be 0).
- Bead notes posted to gm-v01.3.6.
- Phase B retrospective hooks: any tmux quirks observed (e.g. pipe-pane buffering surprises) worth recording for Phase C's HTTP /stream wiring.
</output>
