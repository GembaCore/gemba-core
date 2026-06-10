---
phase: B-native-tmux
plan: 02
subsystem: native-adapter / streaming
tags: [streamable, iohub, refcount, fan-out, tmux, pipe-pane, fifo]
requirements: [NATIVE-04, NATIVE-05]
bead: gm-v01.3.4
depends_on: [B-01]
provides:
  - backend.Streamable optional extension interface
  - *Tmux satisfaction of Streamable (compile-time asserted)
  - sessionIOHub: refcounted fan-out + FIFO lifecycle
affects:
  - internal/adapter/native/backend/{streamable.go,tmux_stream.go,tmux_stream_test.go,tmux.go}
  - internal/adapter/native/{iohub.go,iohub_test.go}
metrics:
  duration: ~25 min wall
  tasks: 2 (both TDD)
  files_created: 5
  files_modified: 1
  tests_added: 18 (9 backend + 9 hub)
  test_runs_under_race: count=10 storm reps clean
completed: 2026-05-21
---

# Phase B Plan 02: Streamable extension on Tmux backend + sessionIOHub pure infra Summary

Built the pure-infra refcounted fan-out (Streamable interface, *Tmux
satisfaction, sessionIOHub) that Plan B-03 will plug into
`OrchestrationPlane.StreamSession`. No StreamSession override yet,
no OrchestrationPlane wiring — that's Wave 3.

## What shipped (one-liners)

- **`backend.Streamable`** — three-method optional extension mirroring
  the `Pausable` pattern; type-assert from the adapter.
- **`*Tmux` satisfies Streamable** — `pipe-pane -o` writing to a FIFO
  under `$TMPDIR`, `pipe-pane` (no command) to toggle off, `capture-pane
  -p -e -J` for ANSI-preserving snapshots. Idempotent via in-struct
  `activePipes map + pipeMu`.
- **`sessionIOHub`** — one StartPipe per pane regardless of subscriber
  count; per-subscriber buffered chan (16), non-blocking fan-out;
  last-out detach triggers StopPipe + FIFO remove via a single reader
  goroutine and `sync.Once` teardown.

## Shipped constants & contracts (Plan B-03 reads these)

| Constant / contract               | Value                                              |
| --------------------------------- | -------------------------------------------------- |
| `subscriberBuffer`                | 16 events                                          |
| `snapshotLines` default           | 2000 lines                                         |
| FIFO path template                | `$TMPDIR/gemba-stream-<safeFIFOName(paneID)>.fifo` |
| `safeFIFOName("%0")`              | `"0"` (strips `%`, keeps `[a-zA-Z0-9_-]`)          |
| `safeFIFOName("")` / all-bad      | `"pane"` fallback                                  |
| Compile-time interface assertion  | `var _ Streamable = (*Tmux)(nil)`                  |
| Streamable methods (locked order) | `StartPipe → StopPipe → SnapshotPane`              |

## Plan B-03 wiring pattern

```go
// In NewWithConfig() once a Streamable-capable backend is configured:
streamable, ok := cfg.Backend.(backend.Streamable)
if !ok {
    // Leave OrchestrationPlane's UnsupportedSessionIO mixin in place —
    // StreamSession will keep returning KindUnsupported.
} else {
    p.hub = newSessionIOHub(streamable, p.resolveSessionToPane, 2000)
}

// In Close():
if p.hub != nil { p.hub.Close() }

// In the StreamSession override (plan B-03 will add this):
func (o *OrchestrationPlane) StreamSession(ctx context.Context, sessionID string) (<-chan core.SessionEvent, error) {
    if o.hub == nil { return nil, unsupported("StreamSession") }
    return o.hub.Attach(ctx, sessionID)
}
```

`resolveSessionToPane` is the adapter-side closure that looks up
`o.sessions[sessionID].ProviderMetadata["pane_id"]` (existing pattern
already used by `lookupSessionPane`). Keeping the translation in the
adapter and the hub purely closure-driven preserves CONTEXT.md C-21
opacity (the hub itself never imports a pane-id shape).

## Race / lifecycle correctness

The first GREEN attempt had a `chansend` ⇄ `close` race under the
100-goroutine storm test. Fix (committed in the same task 2 patch):
hold `h.mu` across the fan-out non-blocking send AND across every
`close(sub.ch)` (in `detach` and in `teardownStream`). `sync.Once`
guards each subscriber's close so the per-context cancellation watcher
and the stream-teardown path can both safely race to call it.

Verified clean under `go test -race -count=10` (90/90 storm passes).

## Commits

| SHA       | Type | Summary                                                                |
| --------- | ---- | ---------------------------------------------------------------------- |
| `ec4fa6c` | test | RED — Streamable + tmux pipe-pane/snapshot lifecycle (9 cases)         |
| `854d559` | feat | GREEN — Streamable iface + *Tmux satisfaction + activePipes/pipeMu     |
| `b1bdb22` | test | RED — sessionIOHub refcount + storm + fan-out (9 cases)                |
| `e1cbde0` | feat | GREEN — sessionIOHub w/ race-safe fanout/detach/teardown + Close       |

## Acceptance floor (final state)

- `go build ./...` — Success
- `go test ./internal/adapter/native/... -race -count=1` — 307 passed
- `go test ./core/... -run TestSessionIDOpacity -count=1` — 1 passed
  (opacity guard remains green; hub lives in `internal/adapter/native/`
  and only the backend package contains pane-id shape knowledge)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] Race between fan-out send and channel close on storm test**
- **Found during:** Task 2 first GREEN test pass
- **Issue:** Storm test (100 goroutine attach/cancel) tripped the race
  detector: `fanout` released `h.mu` before sending, so a parallel
  `detach`/`teardown` could close the subscriber channel mid-send.
- **Fix:** Hold `h.mu` across the non-blocking fan-out send AND across
  every `close(sub.ch)`. Send is `select { default }` so lock-hold
  stays bounded.
- **Files modified:** `internal/adapter/native/iohub.go`
- **Commit:** `e1cbde0`

**2. [Rule 2 — Critical functionality] Stale FIFO recovery in `StartPipe`**
- **Found during:** Task 1 GREEN design
- **Issue:** Plan said `syscall.Mkfifo` -> wrap as `KindAdaptorDegraded`
  on error, but didn't address EEXIST from a prior process crash leaving
  a FIFO behind. Hard-erroring on EEXIST would make the backend
  permanently unrecoverable until the user manually `rm`'d the stale
  file.
- **Fix:** On EEXIST, `os.Remove` the stale FIFO and retry mkfifo once;
  surface degraded only if the retry also fails.
- **Files modified:** `internal/adapter/native/backend/tmux_stream.go`
- **Commit:** `854d559`

No architectural deviations (no Rule 4 events). No auth gates.

## Known Stubs

None — this plan is pure infra. The Streamable interface and hub are
complete; Plan B-03 wires them to `OrchestrationPlane.StreamSession`.

## Threat Flags

None — pipe-pane writes to a 0600 FIFO under `$TMPDIR` owned by the
gemba server user; no new network/auth surface introduced.

## Self-Check: PASSED

- `internal/adapter/native/backend/streamable.go` FOUND
- `internal/adapter/native/backend/tmux_stream.go` FOUND
- `internal/adapter/native/backend/tmux_stream_test.go` FOUND
- `internal/adapter/native/iohub.go` FOUND
- `internal/adapter/native/iohub_test.go` FOUND
- Commit `ec4fa6c` FOUND
- Commit `854d559` FOUND
- Commit `b1bdb22` FOUND
- Commit `e1cbde0` FOUND
