---
phase: B-native-tmux
plan: 02
type: execute
wave: 2
depends_on: [B-01]
files_modified:
  - internal/adapter/native/backend/streamable.go
  - internal/adapter/native/backend/tmux_stream.go
  - internal/adapter/native/backend/tmux_stream_test.go
  - internal/adapter/native/iohub.go
  - internal/adapter/native/iohub_test.go
autonomous: true
requirements: [NATIVE-04, NATIVE-05]

must_haves:
  truths:
    - "A single sessionIOHub multiplexes one tmux pipe-pane FIFO across N subscribers."
    - "Two subscribers attaching to the same session causes exactly one pipe-pane spawn."
    - "Refcount drops to zero -> pipe-pane is toggled off, FIFO file is removed from $TMPDIR."
    - "Disconnect storm (100 rapid attach/detach cycles) leaves zero leaked gemba-stream-*.fifo files."
    - "Backend exposes a Streamable optional extension; only the tmux backend satisfies it."
  artifacts:
    - path: "internal/adapter/native/backend/streamable.go"
      provides: "Streamable optional extension interface (StartPipe / StopPipe / SnapshotPane)"
      contains: "type Streamable interface"
    - path: "internal/adapter/native/backend/tmux_stream.go"
      provides: "Tmux implementation of Streamable: pipe-pane open/close + FIFO path derivation + capture-pane snapshot"
      contains: "func (t *Tmux) StartPipe"
    - path: "internal/adapter/native/iohub.go"
      provides: "sessionIOHub: refcounted per-session FIFO subscriber set + fan-out goroutine"
      contains: "type sessionIOHub struct"
  key_links:
    - from: "iohub.Attach"
      to: "Streamable.StartPipe"
      via: "first-subscriber refcount transition 0->1"
      pattern: "refcount.*StartPipe"
    - from: "iohub.detach"
      to: "Streamable.StopPipe + os.Remove(fifo)"
      via: "last-subscriber refcount transition N->0"
      pattern: "StopPipe.*Remove"
---

<objective>
Build the refcounted fan-out infrastructure that Plan 03 will plug into
StreamSession. This plan is PURE INFRA — no override of the mixin yet,
no change to OrchestrationPlane. We add:
  1. A Streamable optional-extension on the Backend interface (mirrors
     the Pausable pattern already in backend.go).
  2. The Tmux satisfaction of Streamable: open `tmux pipe-pane` to a
     FIFO under $TMPDIR, close + remove on stop, capture-pane for the
     first-subscriber snapshot.
  3. A sessionIOHub struct in the adapter that owns the per-session
     FIFO + subscriber set, with single-mutex refcount semantics.

Purpose: Plan 03's StreamSession override becomes a thin wrapper around
Attach(sessionID). All the lifecycle nastiness (refcount, FIFO cleanup,
disconnect-storm safety) is tested in isolation here.

Output: Streamable + tmux_stream + iohub committed with comprehensive
unit tests using a fake Streamable; zero FIFO leaks under storm test.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/B-native-tmux/CONTEXT.md
@.planning/phases/B-native-tmux/B-01-PLAN.md
@core/session_io.go
@internal/adapter/native/backend/backend.go
@internal/adapter/native/backend/tmux.go
@internal/adapter/native/orchestration.go

<interfaces>
<!-- Pattern to mirror: the existing Pausable extension in backend.go. -->

```go
// internal/adapter/native/backend/backend.go (existing pattern)
type Pausable interface {
    Pause(ctx context.Context, sessionID string) error
    Unpause(ctx context.Context, sessionID string) error
}
```

<!-- Phase A event type that the hub emits. -->

```go
// core/session_io.go (existing)
type SessionEvent struct {
    Kind  string         `json:"kind"`         // "output" | "status" | "exit" | "disconnect"
    Bytes []byte         `json:"bytes,omitempty"`
    Meta  map[string]any `json:"meta,omitempty"`
}
var SessionEventKinds = []string{"output", "status", "exit", "disconnect"}
```
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Streamable backend extension + Tmux implementation</name>
  <bead>gm-v01.3.4</bead>
  <files>internal/adapter/native/backend/streamable.go, internal/adapter/native/backend/tmux_stream.go, internal/adapter/native/backend/tmux_stream_test.go</files>
  <behavior>
    - New `Streamable` interface in streamable.go (NOT added to base Backend — kept optional, callers type-assert exactly like Pausable):
      ```go
      type Streamable interface {
          // StartPipe opens a server-side fan-out from the backing
          // pane to a named pipe under $TMPDIR and returns the path.
          // Idempotent: calling twice with the same sessionID returns
          // the existing path. The caller (sessionIOHub) is
          // responsible for opening the FIFO for read; this method
          // only arranges that data starts flowing into it.
          StartPipe(ctx context.Context, sessionID string) (fifoPath string, err error)
          // StopPipe tears the server-side fan-out down and removes
          // the FIFO file. Safe to call when StartPipe was never
          // invoked (returns nil).
          StopPipe(ctx context.Context, sessionID string) error
          // SnapshotPane returns up to `lines` of backscroll for a
          // first-subscriber snapshot event. For tmux this is
          // capture-pane -p -e -J -S -<lines>; -e preserves ANSI so
          // xterm.js can render colors.
          SnapshotPane(ctx context.Context, sessionID string, lines int) ([]byte, error)
      }
      ```
    - Tmux satisfaction in tmux_stream.go:
      - `fifoPath(paneID)` = `filepath.Join(os.TempDir(), "gemba-stream-"+safeFIFOName(paneID)+".fifo")`. `safeFIFOName` strips the leading `%` from `%N` pane ids and replaces anything outside `[a-zA-Z0-9_-]` with `_` (mirrors the existing `safeTmuxBufferName` helper — extract or duplicate; do NOT make tests depend on the buffer-name helper directly).
      - `StartPipe`:
        - Compute fifoPath.
        - If file already exists AND its mtime is fresh (started within process lifetime — track in an in-struct `activePipes map[string]struct{}` guarded by a mutex), return the path unchanged (idempotent).
        - Otherwise: `syscall.Mkfifo(fifoPath, 0600)`. ENOTSUP / unsupported FS -> wrap as `core.WrapAdaptorError(core.KindAdaptorDegraded, err, "tmux: mkfifo")`.
        - Shell: `tmux pipe-pane -o -t <paneID> 'cat >> <fifoPath>'`. The `-o` flag opens (toggles on) the pipe; quoting the shell command is critical — wrap the path in single quotes and escape any embedded single quotes (paneIDs never contain them but safeFIFOName guarantees alphanumeric anyway).
        - Record paneID in `activePipes`.
      - `StopPipe`:
        - Mutex: if paneID not in activePipes, return nil.
        - Shell: `tmux pipe-pane -t <paneID>` (no command argument toggles the pipe OFF — that's tmux's semantics).
        - `os.Remove(fifoPath)`. ENOENT is not an error.
        - Delete from activePipes.
      - `SnapshotPane`: `tmux capture-pane -p -e -J -t <paneID> -S -<lines>` then return raw bytes. `-e` preserves color escapes (different from existing CapturePane which omits -e).
    - Tests (tmux_stream_test.go): The argv-runner injection from Plan 01 (`runner` field on `Tmux`) carries over — reuse it. Cases:
      - StartPipe happy path: runner sees `pipe-pane -o -t %0 cat >> /tmp/gemba-stream-0.fifo`; fifoPath returned; file exists on disk (or skip the on-disk assertion if `syscall.Mkfifo` fails on the test platform — guard with `t.Skipf` so CI on macOS/Linux passes and other OSes don't block).
      - StartPipe idempotent: second call for same paneID returns same path; runner is NOT invoked a second time.
      - StopPipe after StartPipe: runner sees `pipe-pane -t %0`; file no longer exists.
      - StopPipe without StartPipe: returns nil; runner NOT invoked.
      - StopPipe twice: second call is nil.
      - SnapshotPane: runner sees `capture-pane -p -e -J -t %0 -S -200` for lines=200; for lines<=0 defaults to 200.
      - safeFIFOName: %0 -> "0"; %abc-1 -> "abc-1"; "weird/path" -> "weird_path".
      - Concurrent StartPipe(%0) x 50 from goroutines: runner invoked exactly once for `pipe-pane -o`; assert via atomic counter on the runner.
  </behavior>
  <action>
    Create streamable.go with the `Streamable` interface. Create tmux_stream.go with the `*Tmux` methods (extend the struct with `activePipes map[string]struct{}` + `pipeMu sync.Mutex`; initialize the map in `NewTmux`). Use `syscall.Mkfifo` from `golang.org/x/sys/unix` if needed, but the std `syscall` package's Mkfifo works on darwin+linux which is the supported set. On any GOOS where mkfifo is unavailable, `StartPipe` should return KindAdaptorDegraded with a clear message; do NOT panic.
    Create tmux_stream_test.go per the behavior matrix. Implements bead gm-v01.3.4 (backend half).
  </action>
  <verify>
    <automated>cd /Users/mikebengtson/Documents/GitHub/gemba && go test ./internal/adapter/native/backend/... -run "Stream|Pipe|Snapshot|safeFIFO" -race -count=1 && go build ./...</automated>
  </verify>
  <done>
    Streamable interface compiles; *Tmux satisfies it (assert via `var _ Streamable = (*Tmux)(nil)` at file scope in tmux_stream.go); all test cases pass under -race; existing tmux backend tests unaffected.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: sessionIOHub — refcounted fan-out + FIFO lifecycle</name>
  <bead>gm-v01.3.4</bead>
  <files>internal/adapter/native/iohub.go, internal/adapter/native/iohub_test.go</files>
  <behavior>
    - `sessionIOHub` struct in adapter package (NOT in backend/ — pane-id resolution lives in the adapter; the hub speaks sessionID to its caller and paneID to the Streamable backend, with the translation done by the adapter helper passed in at construction):
      ```go
      type sessionIOHub struct {
          mu      sync.Mutex
          streams map[string]*hubStream    // keyed by paneID
          backend Streamable
          // resolver translates the opaque sessionID -> paneID. Set
          // by the OrchestrationPlane at construction; staying out
          // of the backend package keeps the opacity contract clean.
          resolver func(sessionID string) (paneID string, ok bool)
          snapshotLines int                // default 2000 per CONTEXT.md
      }
      type hubStream struct {
          paneID      string
          fifoPath    string
          subscribers map[*hubSubscriber]struct{}
          cancel      context.CancelFunc   // stops the reader goroutine
          done        chan struct{}        // closed when reader exits
      }
      type hubSubscriber struct {
          ch chan core.SessionEvent
      }
      ```
    - `Attach(ctx, sessionID)` -> `(<-chan core.SessionEvent, error)`:
      1. resolver(sessionID) -> paneID; not found -> KindSessionNotFound.
      2. Lock mu. If streams[paneID] exists:
         - Build a fresh subscriber, add to set.
         - Synthesize a first event from backend.SnapshotPane(paneID, snapshotLines) — `SessionEvent{Kind: "output", Bytes: snapshot}`. Send to THIS subscriber only (non-blocking; if the subscriber channel is unbuffered, buffer to 16 events).
         - Spawn a small goroutine watching ctx.Done() to detach this subscriber.
         - Return the subscriber's channel.
      3. If streams[paneID] does NOT exist (first subscriber):
         - backend.StartPipe(paneID) -> fifoPath; error -> wrap KindAdaptorDegraded.
         - Open the FIFO for read: `os.OpenFile(fifoPath, os.O_RDONLY, 0)`. The open blocks until a writer attaches; tmux's pipe-pane is already the writer because StartPipe ran first. Wrap in a timeout (5s) to avoid hanging when tmux quietly fails.
         - Snapshot via backend.SnapshotPane; send to this first subscriber as the initial event.
         - Spawn the reader goroutine (see below). Record stream.
         - Spawn ctx.Done() watcher.
         - Return subscriber channel.
    - Reader goroutine: bufio.Scanner OR a `bufio.Reader.Read([]byte, 4096)` loop. Each chunk fans out to every subscriber via non-blocking send (`select { case sub.ch <- ev: default: /* drop on slow subscriber */ }`). On EOF / read error -> emit `SessionEvent{Kind: "exit"}` to all subscribers, close all subscriber channels, call backend.StopPipe(paneID), os.Remove(fifoPath), delete stream entry. Use sync.Once to make tear-down idempotent.
    - `detach(sub)`: lock mu; remove from stream.subscribers; if subscribers is now empty -> call stream.cancel (which stops the reader goroutine; the reader's defer block handles StopPipe + FIFO removal + close). Always close sub.ch from the detach path (not the reader) so ctx-cancel detach gets a clean close.
    - `Close()`: lock mu; for every stream call stream.cancel; wait on stream.done. Used in OrchestrationPlane.Close() (plan 03 will wire this).
    - Tests (iohub_test.go) — use a fake Streamable backed by an in-process pipe (`io.Pipe`) instead of a real FIFO so tests run on every OS:
      - Fake exposes StartPipe returning the read-end path AND letting tests write bytes via a `Write([]byte)` test helper. Use `io.Pipe` or a `*os.File` pair created via `os.Pipe()` (cross-platform) — for these tests we override the OS-level "open FIFO for read" step via an injectable opener function on the hub (add `openFIFO func(path string) (io.ReadCloser, error)` field, default `os.OpenFile`).
      - Snapshot-only: single subscriber, no writer activity, ctx cancelled -> received exactly one event (the snapshot), then channel closes; backend StopPipe called once; fifoPath removed (or recorded as removed in the fake).
      - Two subscribers attach to same sessionID -> backend.StartPipe called ONCE (assert via counter); both subscribers receive their own snapshot; writing N bytes via the fake -> both subscribers see the bytes.
      - One of two subscribers detaches (cancel its ctx) -> StopPipe NOT called; remaining subscriber still receives subsequent writes.
      - Both detach -> StopPipe called exactly once; fifoPath removed; stream entry gone from streams map.
      - Disconnect storm: 100 goroutines each attach + immediately cancel (sequential per goroutine, parallel across). Assert: hub.streams is empty at the end; fake.RemoveCount == fake.StartCount; no goroutine leak (use `runtime.NumGoroutine` snapshot before/after with a 100ms settle window).
      - Slow subscriber: subscriber that never reads from its channel does NOT block other subscribers (write 1000 events, fast subscriber receives them, slow subscriber drops most).
      - resolver returns ok=false -> Attach returns KindSessionNotFound.
      - backend.StartPipe returns error -> Attach returns wrapped KindAdaptorDegraded; no FIFO opened; no goroutine spawned.
      - hub.Close() with active streams -> all reader goroutines exit; all subscriber channels close with a final disconnect or exit event.
  </behavior>
  <action>
    Create internal/adapter/native/iohub.go with the struct + Attach + detach + reader loop + Close per the behavior block. Channel buffer per subscriber: 16 events (room to absorb a snapshot + a few output bursts; documented constant `subscriberBuffer = 16`). Snapshot lines default: 2000 (per CONTEXT.md "capture-pane … -S -2000").
    Create iohub_test.go with all 9 test cases. The fake Streamable lives in the test file (no need for a shared test helper yet — plan 03 will reuse the hub directly, not the fake). Use a small `fakeStreamable` struct with atomic counters for StartCount / StopCount / RemoveCount, a chan to inject bytes, and an `openFIFO` override slot. Implements the hub half of bead gm-v01.3.4.
  </action>
  <verify>
    <automated>cd /Users/mikebengtson/Documents/GitHub/gemba && go test ./internal/adapter/native/... -run "IOHub|Hub|Attach|Detach|Storm" -race -count=1 && go build ./...</automated>
  </verify>
  <done>
    sessionIOHub passes all 9 unit tests including the 100-goroutine storm under -race; no goroutine leak detected; one StartPipe per session regardless of subscriber count; StopPipe called exactly once when refcount hits zero; hub does NOT reach into the backend package's pane-id resolution (resolver is injected — opacity preserved).
  </done>
</task>

</tasks>

<verification>
- `go test ./internal/adapter/native/... -race -count=1` green.
- `go test ./core/... -count=1` green (opacity guard still passes — the hub speaks paneID only via the injected resolver, never grep-matches the forbidden patterns).
- No new files under `core/`.
- `ls /tmp/gemba-stream-*.fifo 2>/dev/null | wc -l` -> 0 after the storm test (use `t.TempDir()` for the fake's "FIFO" paths so the assertion is scoped, but also include a final assertion that the production `os.TempDir()` path has no leaked files — guard with build tag if it's flaky on shared CI runners).
</verification>

<success_criteria>
1. Streamable interface exists and is satisfied by *Tmux (compile-time assertion).
2. sessionIOHub maintains correct refcount semantics: one StartPipe per session, StopPipe iff refcount hits zero, FIFO removed on tear-down.
3. Disconnect storm test passes with -race and leaves zero leaked FIFO files / goroutines.
4. Bead gm-v01.3.4 ready to close after this plan ships.
5. Plan 03 can wire `OrchestrationPlane.StreamSession` directly to `hub.Attach` with no further plumbing.
</success_criteria>

<output>
After completion, create `.planning/phases/B-native-tmux/B-02-SUMMARY.md` capturing:
- The exact subscriber buffer size, snapshot line count, and FIFO path template shipped.
- Confirmation that `*Tmux` satisfies `Streamable` via the `var _ Streamable = (*Tmux)(nil)` assertion.
- The pattern Plan 03 should use: `hub := newSessionIOHub(streamable, o.resolveSessionToPane, 2000)`.
- Bead notes posted to gm-v01.3.4.
</output>
