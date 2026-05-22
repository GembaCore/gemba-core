---
phase: B-native-tmux
plan: 03
subsystem: native-adapter
tags: [native, tmux, sessions, streamsession, iohub]
dependency_graph:
  requires: [B-01, B-02]
  provides:
    - "OrchestrationPlane.StreamSession backed by sessionIOHub"
    - "Hub lifecycle tied to OrchestrationPlane.Close()"
  affects:
    - "Phase C HTTP /stream handler can now call adapter.StreamSession directly"
tech_stack:
  added: []
  patterns:
    - "type-assertion gate for optional backend extension (mirrors Pausable pattern)"
    - "resolver closure injection — keeps pane-id translation out of transport-layer code"
key_files:
  created:
    - internal/adapter/native/session_io_stream_test.go
  modified:
    - internal/adapter/native/orchestration.go
    - internal/adapter/native/session_io.go
decisions:
  - "Post-Close StreamSession fast-fails KindUnsupported (no re-attach)"
  - "Hub teardown precedes reaper/reconcile cancel in Close()"
  - "Test helpers (streamFakeBackend, plainFakeBackend) live inside the native package — no separate testutil shim"
metrics:
  duration_minutes: ~6
  tests_added: 7
  tests_passing: 314
  completed: 2026-05-21
---

# Phase B Plan 03: StreamSession Wiring Summary

OrchestrationPlane now satisfies the full Phase A SessionIO trio (SendInput
+ ResizeSession + StreamSession) against the real tmux backend, with the
streaming surface backed by the Plan B-02 sessionIOHub.

## Hub-Construction Flow

`NewWithConfig` runs a single type-assertion gate after the Fanout observer
is wired:

```go
if cfg.Backend != nil {
    if s, ok := cfg.Backend.(backend.Streamable); ok {
        resolver := func(sessionID string) (string, bool) {
            p.mu.Lock(); defer p.mu.Unlock()
            sess, ok := p.sessions[sessionID]
            if !ok || sess == nil { return "", false }
            paneID, _ := sess.ProviderMetadata["pane_id"].(string)
            return paneID, paneID != ""
        }
        p.hub = newSessionIOHub(s, resolver, 2000)
    }
}
```

Three properties matter:

1. The resolver closure captures `p` **by reference**, so it reads the live
   session map at Attach time, not at construction time. New sessions
   appearing post-NewWithConfig are immediately stream-attachable.
2. The resolver acquires `p.mu` briefly per call rather than the hub
   holding it across internals. The hub's own mutex (`h.mu`) does NOT
   nest under `p.mu`.
3. Backends that do not satisfy Streamable leave `p.hub == nil`. The
   StreamSession override returns `KindUnsupported` naming the backend
   in the error message — the SPA's capability negotiator hides the
   stream control without a separate manifest hop.

## Close() Semantics

`OrchestrationPlane.Close()` runs hub teardown **before** the reaper +
reconcile cancellations:

```go
func (o *OrchestrationPlane) Close() {
    if o.hub != nil {
        o.hub.Close()
        o.hub = nil      // post-Close StreamSession fast-fails Unsupported
    }
    if o.stopReaper != nil    { o.stopReaper() }
    if o.stopReconcile != nil { o.stopReconcile() }
}
```

Two decisions worth flagging:

- **Drain-first ordering**: subscribers may be reading bytes that the
  reconcile loop indirectly emits (status events on a pass). Closing the
  hub first means no goroutine is holding a stale channel when the loops
  stop.
- **Nil-the-field, no re-attach**: a subsequent `StreamSession` call after
  Close returns `KindUnsupported` rather than reconstructing the hub. This
  matches the "Close is final" contract every adapter follows; callers
  who want a new streaming surface construct a new OrchestrationPlane.

Close is idempotent — the `hub.Close()` itself short-circuits on a flipped
`closed` flag, and the nil-guard on the field handles the second invocation.

## SessionIO Trio Now Complete

```
core.UnsupportedSessionIO    embedded for forward-compat
        ├─ SendInput          overridden (Plan B-01, gm-v01.3.2)
        ├─ ResizeSession      overridden (Plan B-01, gm-v01.3.2)
        └─ StreamSession      overridden (Plan B-03, gm-v01.3.5) ◀ this plan
```

The embed is retained per the plan: if Phase C adds another verb to the
SessionIO surface, the mixin keeps existing adapters compiling.

## Test Coverage

Seven new behaviors in `session_io_stream_test.go`:

| # | Test                                                          | Asserts                                                 |
|---|---------------------------------------------------------------|---------------------------------------------------------|
| 1 | `TestStreamSession_HappyPath_SnapshotThenLiveThenCancel`     | snapshot → live → cancel-closes ordering                |
| 2 | `TestStreamSession_TwoSubscribers_OneStartPipe_OneStopPipe`  | refcount preserved through OrchestrationPlane           |
| 3 | `TestStreamSession_BackendNil_Unsupported`                   | zero-config plane degrades cleanly                      |
| 4 | `TestStreamSession_BackendNotStreamable_UnsupportedNamesBackend` | error message names the backend                    |
| 5 | `TestStreamSession_EmptySessionID_Validation`                | empty id rejected before any backend touch              |
| 6 | `TestStreamSession_UnknownSessionID_NotFound`                | resolver miss surfaces as KindSessionNotFound           |
| 7 | `TestStreamSession_CloseClosesSubscriberAndFastFailsAfter`   | Close drains subs + post-Close fast-fails + idempotent  |

Helpers reuse `fakeStreamable` + `newFakeStreamable` + injectable
`openFIFO` from `iohub_test.go` (Plan B-02). The new `streamFakeBackend`
composes those with Backend stub methods so the type satisfies both
`backend.Backend` and `backend.Streamable`. `plainFakeBackend` is a
Backend that deliberately does NOT satisfy Streamable, covering the
degrade path. Total helper code stayed under the 30-line dup threshold,
so the plan's "promote to testutil if duplication exceeds ~30 lines"
escape hatch was not triggered.

## Verification Gates

| Gate                                                           | Result |
|----------------------------------------------------------------|--------|
| `go build ./...`                                               | pass   |
| `go test ./internal/adapter/native/... -race -count=1`         | 314 pass |
| `go test ./core/... -run TestSessionIDOpacity -count=1`        | pass   |
| `grep -n "func.*StreamSession" internal/adapter/native/*.go` (non-test) | exactly 1 hit (session_io.go:133) |
| `OrchestrationPlane.Close()` idempotency                       | covered by test 7 |

## Opacity Guard Note

The resolver reads `sess.ProviderMetadata["pane_id"]` — there is no
`paneID := sessionID` shortcut in either the override or the hub. Pane-id
resolution stays inside the resolver closure injected at construction;
transport-layer code (this override, the eventual HTTP /stream handler)
only ever sees the opaque sessionID. `TestSessionIDOpacity` confirms it.

## Deviations from Plan

None — plan executed exactly as written. Test cases match the behavior
matrix item-for-item; helper placement (in-package, no testutil shim)
followed the plan's recommended branch since duplication was small.

## Bead

`gm-v01.3.5` — Story: Native StreamSession override — pipe-pane via hub
+ snapshot semantics. Ready to close after this commit ships.

## Self-Check: PASSED

- internal/adapter/native/session_io_stream_test.go: FOUND
- internal/adapter/native/orchestration.go (hub field + Close): FOUND
- internal/adapter/native/session_io.go (StreamSession override): FOUND
- Commit 7352ce5 (RED): FOUND
- Commit 88d6bb9 (GREEN): FOUND
