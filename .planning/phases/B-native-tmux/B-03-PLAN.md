---
phase: B-native-tmux
plan: 03
type: execute
wave: 3
depends_on: [B-01, B-02]
files_modified:
  - internal/adapter/native/session_io.go
  - internal/adapter/native/session_io_stream_test.go
  - internal/adapter/native/orchestration.go
autonomous: true
requirements: [NATIVE-04, NATIVE-05]

must_haves:
  truths:
    - "OrchestrationPlane.StreamSession returns a channel of SessionEvent backed by the sessionIOHub from Plan 02."
    - "First subscriber receives a snapshot event before any live-output events."
    - "Backend without Streamable returns KindUnsupported (Phase B only the tmux backend qualifies)."
    - "OrchestrationPlane.Close() shuts the hub down cleanly (all reader goroutines exit)."
  artifacts:
    - path: "internal/adapter/native/session_io.go"
      provides: "StreamSession override + hub field on OrchestrationPlane + resolver wiring"
      contains: "func (o *OrchestrationPlane) StreamSession"
    - path: "internal/adapter/native/orchestration.go"
      provides: "Hub construction in NewWithConfig + hub.Close() invocation in Close()"
      contains: "iohub"
  key_links:
    - from: "OrchestrationPlane.StreamSession"
      to: "sessionIOHub.Attach"
      via: "thin wrapper, no business logic"
      pattern: "hub.Attach"
    - from: "NewWithConfig"
      to: "sessionIOHub construction with resolver closure"
      via: "Streamable type-assertion on cfg.Backend"
      pattern: "Streamable"
---

<objective>
Wire the StreamSession override on the native OrchestrationPlane to the
sessionIOHub built in Plan 02. After this plan, the full Phase B input +
output surface (SendInput, ResizeSession, StreamSession) is live against
real tmux.

Purpose: This is the integration point — a Phase C HTTP /stream handler
can call `adapter.StreamSession(ctx, id)` and pipe events to SSE without
any further adapter work. The hub does the heavy lifting; this plan
just makes it reachable from the core interface.

Output: StreamSession override committed; OrchestrationPlane owns one
sessionIOHub instance per Backend (nil when Backend doesn't satisfy
Streamable); Close() tears it down.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/B-native-tmux/CONTEXT.md
@.planning/phases/B-native-tmux/B-01-PLAN.md
@.planning/phases/B-native-tmux/B-02-PLAN.md
@core/session_io.go
@internal/adapter/native/orchestration.go
@internal/adapter/native/session_io.go
@internal/adapter/native/iohub.go
@internal/adapter/native/backend/streamable.go

<interfaces>
<!-- From Plan 02 (already shipped by this point). -->
```go
// internal/adapter/native/iohub.go
func newSessionIOHub(b backend.Streamable, resolver func(string) (string, bool), snapshotLines int) *sessionIOHub
func (h *sessionIOHub) Attach(ctx context.Context, sessionID string) (<-chan core.SessionEvent, error)
func (h *sessionIOHub) Close()
```

<!-- The mixin we're overriding (from Phase A). -->
```go
// core/session_io.go
func (UnsupportedSessionIO) StreamSession(context.Context, string) (<-chan SessionEvent, error)
```
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: StreamSession override + hub wiring in OrchestrationPlane</name>
  <bead>gm-v01.3.5</bead>
  <files>internal/adapter/native/orchestration.go, internal/adapter/native/session_io.go, internal/adapter/native/session_io_stream_test.go</files>
  <behavior>
    - Add `hub *sessionIOHub` field to OrchestrationPlane.
    - In `NewWithConfig`: if `cfg.Backend != nil` AND `cfg.Backend` satisfies `backend.Streamable`, construct the hub with:
      ```go
      resolver := func(sessionID string) (string, bool) {
          o.mu.Lock(); defer o.mu.Unlock()
          sess, ok := o.sessions[sessionID]
          if !ok || sess == nil { return "", false }
          p, _ := sess.ProviderMetadata["pane_id"].(string)
          return p, p != ""
      }
      o.hub = newSessionIOHub(cfg.Backend.(backend.Streamable), resolver, 2000)
      ```
      Note: capture `o` by reference in the closure (closure must read `o.sessions` at Attach time, not at NewWithConfig time). The hub does NOT hold `o.mu` — the resolver acquires it briefly per call.
    - In `OrchestrationPlane.Close()`: if `o.hub != nil`, call `o.hub.Close()` (place BEFORE the existing reaper/reconcile cancels — drain subscribers before stopping the loops that might be emitting status events).
    - New override in session_io.go:
      ```go
      func (o *OrchestrationPlane) StreamSession(ctx context.Context, sessionID string) (<-chan core.SessionEvent, error) {
          if o.cfg.Backend == nil   { return nil, unsupported("StreamSession") }
          if o.hub == nil           { return nil, core.NewAdaptorError(core.KindUnsupported, "native: backend %q does not implement Streamable", o.cfg.Backend.Name()) }
          if sessionID == ""        { return nil, core.NewAdaptorError(core.KindValidation, "native: StreamSession requires session id") }
          return o.hub.Attach(ctx, sessionID)
      }
      ```
    - Update the OrchestrationPlane doc comment in orchestration.go: "Phase B overrides SendInput + ResizeSession + StreamSession; UnsupportedSessionIO embed retained for forward-compatibility if Phase C adds verbs."
    - Tests (session_io_stream_test.go) — REUSE the fakeStreamable + fakeBackend patterns from Plan 02's tests; promote them to a small `testutil` file inside the `native_test` package if duplication exceeds ~30 lines (do not export to a separate package — keep test helpers internal).
      - Construct OrchestrationPlane with a fakeBackend that ALSO satisfies Streamable; insert a fake session into `o.sessions` directly (mimicking what StartSession does) with `ProviderMetadata["pane_id"]="%0"`.
      - StreamSession happy path: ctx with cancel; receive snapshot event (Kind="output", Bytes matches fake snapshot); fake.Write(["live bytes"]); receive output event; cancel ctx; channel closes.
      - Two subscribers same sessionID: both receive their own snapshot; fake.StartCount == 1; cancel both; fake.StopCount == 1.
      - StreamSession with Backend=nil (zero-config plane): KindUnsupported.
      - StreamSession with Backend that does NOT satisfy Streamable (use plain fakeBackend without StartPipe): KindUnsupported, error message names the backend.
      - StreamSession with empty sessionID: KindValidation.
      - StreamSession with unknown sessionID: KindSessionNotFound (propagated from hub).
      - Close() with one active subscriber: subscriber's channel closes; subsequent StreamSession call returns KindUnsupported OR re-attaches cleanly (decide and document — recommend re-attach: Close() is a clean shutdown only; tests assert Attach returns a channel that immediately receives a snapshot if a new subscriber attaches post-Close. Actually safer: after Close(), set `o.hub = nil` so subsequent calls fast-fail KindUnsupported; test asserts this).
      - Opacity guard: run `go test ./core/... -run TestSessionIDOpacity -count=1` and assert exit 0 (the resolver closure references `sess.ProviderMetadata["pane_id"]` not a `paneID := sessionID` pattern, so it should be fine — but assert).
  </behavior>
  <action>
    Modify orchestration.go to add `hub *sessionIOHub` and wire construction + Close. Add the StreamSession override to session_io.go (file already exists from Plan 01 — append). Create session_io_stream_test.go per the behavior matrix. Update the embed comment per behavior block. Implements bead gm-v01.3.5.
    Decision per CONTEXT.md scope §"Backend interface (optional extension)": Streamable lives in `internal/adapter/native/backend/`; the FIFO + refcount machinery lives in `internal/adapter/native/` (the hub, shipped Plan 02). This task only stitches them together.
  </action>
  <verify>
    <automated>cd /Users/mikebengtson/Documents/GitHub/gemba && go test ./internal/adapter/native/... -race -count=1 && go test ./core/... -run TestSessionIDOpacity -count=1 && go build ./...</automated>
  </verify>
  <done>
    OrchestrationPlane satisfies the full Phase A SessionIO trio (SendInput + ResizeSession + StreamSession) with real implementations; the `var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)` assertion still holds; all 8 stream test cases pass under -race; opacity guard green; Close() shuts down the hub before reaper/reconcile.
  </done>
</task>

</tasks>

<verification>
- `go test ./internal/adapter/native/... -race -count=1` green.
- `go test ./core/... -count=1` green (opacity guard passes — pane-id resolution stays inside the adapter's resolver closure, which reads ProviderMetadata; no `paneID := sessionID` pattern).
- `grep -rn "func.*StreamSession" internal/adapter/native/ | grep -v _test.go` shows exactly 1 hit (the override in session_io.go).
- `OrchestrationPlane.Close()` is idempotent: calling twice does not panic.
</verification>

<success_criteria>
1. StreamSession is now a real method on OrchestrationPlane; it returns (<-chan SessionEvent, error) backed by the hub.
2. Backend without Streamable degrades to KindUnsupported with a clear, named error.
3. Hub lifecycle is tied to OrchestrationPlane lifecycle (constructed in NewWithConfig, torn down in Close).
4. Phase B's three success-criteria deliverables (snapshot-first stream, multi-subscriber fan-out, disconnect storm cleanliness) are now exercisable end-to-end via the unit-tested hub + this thin wrapper.
5. Bead gm-v01.3.5 ready to close after this plan ships.
</success_criteria>

<output>
After completion, create `.planning/phases/B-native-tmux/B-03-SUMMARY.md` capturing:
- The hub-construction flow (resolver closure + Streamable type-assertion).
- The decision on Close() semantics (set hub=nil so post-Close StreamSession fast-fails).
- Confirmation that the full SessionIO trio is implemented and the mixin embed is retained.
- Bead notes posted to gm-v01.3.5.
</output>
