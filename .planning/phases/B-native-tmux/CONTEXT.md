# Phase B — Native tmux SessionIO

## Goal

An operator can stream tmux output and inject keystrokes through the
new core interface against a real tmux backend, with deterministic
resource cleanup.

## Depends on

Phase A (`feat/gemba-lite-A-core-sessionio` — merged to main):
- `core.SessionInput` / `SessionEvent` / `SessionInputMode` types
- `OrchestrationPlaneAdaptor.SendInput` / `ResizeSession` /
  `StreamSession` methods
- `UnsupportedSessionIO` mixin embedded in every adapter
- `sessionID` opacity guard test (allowlist:
  `internal/adapter/native/backend/`)

## Why now

Phase A landed the contract. Phase B is the first real
implementation — the proof that the contract is right and the
substrate for Phase C's HTTP endpoints. Native tmux is the simplest
backend to satisfy; container/k8s backends follow the same pattern
in later milestones.

## Scope (this phase)

### Native adapter implementation

Override the mixin defaults on `internal/adapter/native` (or wherever
the native adapter's `OrchestrationPlaneAdaptor` lives):

1. **`SendInput(ctx, sessionID, in SessionInput) error`**
   - Resolve `sessionID` → tmux pane id inside
     `internal/adapter/native/backend/` (the only allowlisted dir per
     the opacity guard).
   - Dispatch by `in.Mode`:
     - `InputLiteral` → `tmux send-keys -t <pane> -l -- <keys>`
     - `InputKeys` → `tmux send-keys -t <pane> -- <keys>` (named keys
       like `Enter`, `C-c`, `Up`)
     - `InputSignal` → translate signal name to tmux key sequence
       (`SIGINT` → `C-c`, `SIGTERM` → `C-\`, etc.) on tmux; the
       semantic hook exists so future container backends can call
       real signals.
   - The existing `Backend.SendKeys` already handles the literal/keys
     split via the `Enter` suffix convention — extend it or wrap it
     so `InputLiteral` uses `-l` consistently. Tests cover quoted,
     multi-line, and unicode payloads.

2. **`ResizeSession(ctx, sessionID, cols, rows int) error`**
   - tmux owns geometry server-side; for the native backend this is
     effectively a no-op that returns nil. Document why. The method
     exists so container/k8s backends can do the real thing.

3. **`StreamSession(ctx, sessionID) (<-chan SessionEvent, error)`**
   - First subscription: open a `tmux pipe-pane` to a FIFO under
     `$TMPDIR/gemba-stream-<safe-sessionID>.fifo`. Reader goroutine
     scans bytes off the FIFO and fans out `SessionEvent{Kind:
     "output"}` to subscribers.
   - On subscribe: increment refcount; on `<-ctx.Done()` /
     unsubscribe: decrement.
   - When refcount → 0: `tmux pipe-pane -O -t <pane>` (toggles off),
     remove the FIFO, close the channel.
   - Snapshot semantics on first subscribe (so new viewers see
     backscroll): synthesize a `capture-pane -p -e -J -S -2000` and
     send it as the first event to that subscriber only.
   - Status / exit events: when the pane disappears (kill, end),
     emit a final `SessionEvent{Kind: "exit"}` then close the channel.

### Refcounted fan-out infrastructure

Add a small in-adapter component (`sessionIOHub` or similar) that
owns the per-session FIFO + subscriber set. Single struct, single
mutex; nothing fancy. Tests:
- Two subscribers attach to one session → one `pipe-pane` opened.
- One detaches → `pipe-pane` stays open.
- Both detach → `pipe-pane` torn down, FIFO file removed.
- Disconnect storm (rapid attach/detach × N) → zero leaked FIFO
  files in `$TMPDIR`.

### Backend interface (optional extension)

The plan in `docs/design/gemba-lite.md` proposed a `Streamable`
extension to the internal `Backend` interface. Decision: add it only
if the native adapter needs it to keep the layering clean — i.e. the
FIFO + refcount machinery should live in the adapter, while the
tmux-specific shell-outs (`pipe-pane`, `capture-pane`, named-pipe
filename derivation) stay inside `internal/adapter/native/backend/`.
Other backends (Docker, AppleScript) won't satisfy `Streamable` in
this phase.

## Out of scope

- HTTP endpoints — Phase C.
- Frontend xterm.js wiring — Phase D.
- Docker / k8s `StreamSession` implementations — later milestone.
- Refcounted fan-out across processes — single-process only.

## Success criteria (from ROADMAP)

1. `StreamPane` on tmux backend emits live pane bytes via `pipe-pane`;
   integration test reads a known echo round-trip end-to-end.
2. Disconnect storm test (N subscribers attach/detach) leaves zero
   leaked named-pipe files on the host.
3. `SendInput` literal mode round-trips quoted multi-line + unicode
   input; keys mode delivers `Enter`, `C-c`, `Up` correctly; signal
   mode maps to expected control key.
4. Refcounted fan-out test: two subscribers share one underlying
   tmux IO channel; channel torn down only when both detach.
5. `go build ./...` green; `go test ./internal/adapter/native/...`
   green.

## Bead linkage

Epic: `gm-v01.3` — Native tmux SessionIO implementation
Stories: none seeded yet. The planner should propose a story list;
file the beads under `gm-v01.3` before commits land per the bridge
protocol.

Suggested split (planner can refine):
- `gm-v01.3.1` — Wrap/extend Backend.SendKeys for literal vs keys vs signal
- `gm-v01.3.2` — Native adapter SendInput override (resolve sessionID → pane id, dispatch by Mode)
- `gm-v01.3.3` — Native adapter ResizeSession override (no-op + doc)
- `gm-v01.3.4` — sessionIOHub: refcounted fan-out + FIFO lifecycle
- `gm-v01.3.5` — Native adapter StreamSession override using the hub + snapshot semantics
- `gm-v01.3.6` — Integration tests: echo round-trip + multi-subscriber fan-out + disconnect storm + signal dispatch

## Test environment notes

- tmux integration tests must be guarded by a `-integration` build
  tag and skipped when `tmux` isn't on `$PATH` (existing convention
  per `internal/adapter/native/peek_test.go` patterns).
- The opacity guard test will trip if pane-id resolution leaks
  outside `internal/adapter/native/backend/`. Keep the translation
  inside that boundary.
