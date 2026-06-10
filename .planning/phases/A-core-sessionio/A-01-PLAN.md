---
phase: A-core-sessionio
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - core/orchestration.go
  - core/session_io.go
  - core/session_io_test.go
  - core/orchestration_test.go
  - internal/adapter/native/orchestration.go
  - internal/adapter/gt/agents.go
  - internal/adapter/gt/sessions.go
  - internal/adapter/mock/plane.go
  - internal/adapter/noop/orchestration.go
autonomous: true
requirements: [CORE-01, CORE-02, CORE-03, CORE-04]
bead: gm-v01.2
must_haves:
  truths:
    - "core package exports SessionInputMode with three values: InputLiteral, InputKeys, InputSignal"
    - "core package exports SessionInput{Keys, Mode} and SessionEvent{Kind, Bytes, Meta}"
    - "OrchestrationPlaneAdaptor interface includes SendInput, ResizeSession, StreamSession"
    - "core exports an UnsupportedSessionIO mixin whose three methods return core.KindUnsupported"
    - "Every existing adapter (native, gt, mock, noop, in-tree test fakes) embeds the mixin and compiles against the extended interface"
    - "Calling SendInput/ResizeSession/StreamSession on any in-tree adapter returns *AdaptorError{Kind: KindUnsupported}"
    - "go build ./... is green"
    - "go test ./core/... ./internal/adapter/... passes"
    - "No caller in the tree treats sessionID as a tmux pane id (verified by grep)"
  artifacts:
    - path: "core/session_io.go"
      provides: "SessionInputMode, SessionInput, SessionEvent types + UnsupportedSessionIO mixin"
    - path: "core/session_io_test.go"
      provides: "Type-value + default-unsupported behavior tests"
    - path: "core/orchestration.go"
      provides: "Extended OrchestrationPlaneAdaptor interface with three new methods"
      contains: "SendInput, ResizeSession, StreamSession"
  key_links:
    - from: "core/orchestration.go"
      to: "core/session_io.go"
      via: "interface methods reference SessionInput, SessionEvent types"
      pattern: "SendInput|ResizeSession|StreamSession"
    - from: "internal/adapter/{native,gt,mock,noop}"
      to: "core.UnsupportedSessionIO"
      via: "embedded struct mixin satisfies the three new interface methods"
      pattern: "core\\.UnsupportedSessionIO"
---

<objective>
Land the foundational session-IO contract on `core.OrchestrationPlaneAdaptor` so every downstream gemba-lite slice (B native tmux, C HTTP transport, D-G frontend) can be built against a stable interface. Adds three core types, three interface methods, and a default-unsupported mixin embedded in every existing adapter.

Purpose: Without this, Phase B has nothing to implement and Phase C has nothing to call. Designed once for the full runtime matrix (tmux today, Docker/k8s/microVM tomorrow) so we never rewire interfaces when the dispatcher moves into a cluster.

Output: Extended `core` package + adapter sweep, all green under `go build ./...` and `go test ./core/... ./internal/adapter/...`.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/A-core-sessionio/CONTEXT.md
@.planning/ROADMAP.md
@docs/design/gemba-lite.md
@docs/process/gsd-beads-bridge.md
@core/orchestration.go
@core/errors.go

<interfaces>
<!-- The exact shapes to add. Extracted from docs/design/gemba-lite.md §"Core interface additions". -->
<!-- Lock these names — every downstream phase imports them. -->

Types (NEW — add to core/session_io.go):

```go
type SessionInputMode string
const (
    InputLiteral SessionInputMode = "literal" // typed chars
    InputKeys    SessionInputMode = "keys"    // named keys: Enter, C-c, Up
    InputSignal  SessionInputMode = "signal"  // SIGINT/SIGTERM
)

type SessionInput struct {
    Keys string
    Mode SessionInputMode
}

type SessionEvent struct {
    Kind  string         // "output" | "status" | "exit" | "disconnect"
    Bytes []byte
    Meta  map[string]any
}
```

Interface extension (add to existing core.OrchestrationPlaneAdaptor in core/orchestration.go):

```go
SendInput(ctx context.Context, sessionID string, in SessionInput) error
ResizeSession(ctx context.Context, sessionID string, cols, rows int) error
StreamSession(ctx context.Context, sessionID string) (<-chan SessionEvent, error)
```

Mixin (NEW — add to core/session_io.go):

```go
// UnsupportedSessionIO is the default-unsupported helper every adaptor
// that has not yet implemented session IO embeds. The three methods
// return *AdaptorError{Kind: KindUnsupported} so the caller can hide
// the control (per G-9, error-kind contract gm-faz).
type UnsupportedSessionIO struct{}

func (UnsupportedSessionIO) SendInput(context.Context, string, SessionInput) error { ... }
func (UnsupportedSessionIO) ResizeSession(context.Context, string, int, int) error { ... }
func (UnsupportedSessionIO) StreamSession(context.Context, string) (<-chan SessionEvent, error) { ... }
```

Existing AdaptorError construction pattern (from core/errors.go):

```go
core.NewAdaptorError(core.KindUnsupported, "adaptor X does not support StreamSession")
```

Existing in-tree implementations of OrchestrationPlaneAdaptor (from grep `var _ core.OrchestrationPlaneAdaptor`):

- internal/adapter/native/orchestration.go (line 104) — `*native.OrchestrationPlane`
- internal/adapter/gt/agents.go (line 128) — `*gt.OrchestrationPlane` (methods spread across gt/sessions.go, gt/escalations.go, etc.)
- internal/adapter/mock/plane.go (line 345) — `*mock.OrchestrationPlane`
- internal/adapter/noop/orchestration.go (line 86) — `*noop.OrchestrationPlane`
- core/orchestration_test.go (line 336) — `*noopOrchestrator` (test fixture; must also embed mixin)
</interfaces>

Sequencing note: each task is its own atomic commit; gsd-executor runs `go build ./...` and `go test ./core/... ./internal/adapter/...` between tasks. Task A.2 will leave the tree red-building (interface extended, adapters not yet embedding the mixin) — that is expected and resolved by Task A.3.
</context>

<tasks>

<task type="auto">
  <name>Task A.1: Add SessionInput / SessionEvent / SessionInputMode core types</name>
  <bead>gm-v01.2.1</bead>
  <files>core/session_io.go</files>
  <action>
Create a new file `core/session_io.go` (do NOT cram into orchestration.go — keeps the diff readable and gives Phase B a focused file to grow). Declare the three types verbatim per docs/design/gemba-lite.md §"Core interface additions" (see `<interfaces>` block above):

- `SessionInputMode` string enum with exactly three const values: `InputLiteral="literal"`, `InputKeys="keys"`, `InputSignal="signal"` — these are LOCKED per CONTEXT.md G-4.
- `SessionInput struct { Keys string; Mode SessionInputMode }`.
- `SessionEvent struct { Kind string; Bytes []byte; Meta map[string]any }` — `Kind` carries one of the four LOCKED tokens per CONTEXT.md G-5 (`"output"`, `"status"`, `"exit"`, `"disconnect"`); document those four in a comment but do NOT enum-encode them (callers stream raw strings over SSE — the wire token IS the value).

Each type gets a Go doc comment referencing the locked-decision id (G-4 / G-5) so future readers know the enumeration is closed.

Do NOT add the interface methods yet (Task A.2). Do NOT add the mixin yet (Task A.2 in same commit window — see note). Per the bridge contract this commit is the atomic landing of the three types only.
  </action>
  <verify>
    <automated>go build ./core/... && go vet ./core/...</automated>
  </verify>
  <done>
- core/session_io.go exists with the three exported types
- `go build ./core/...` is green
- `grep -n 'InputLiteral\|InputKeys\|InputSignal' core/session_io.go` shows exactly the three locked tokens
- Commit message: `feat(core): add SessionInput/SessionEvent/SessionInputMode types (gm-v01.2.1)`
  </done>
</task>

<task type="auto">
  <name>Task A.2: Extend OrchestrationPlaneAdaptor + add UnsupportedSessionIO mixin</name>
  <bead>gm-v01.2.2</bead>
  <files>core/orchestration.go, core/session_io.go</files>
  <action>
Two coordinated edits in one commit so the interface and its default helper land atomically:

1. In `core/orchestration.go`, add three methods to the existing `OrchestrationPlaneAdaptor` interface (append at end, after `Subscribe`):

   ```go
   // SendInput delivers a keystroke / signal payload to a live session.
   // Adaptors that cannot inject input return KindUnsupported (G-9).
   // Mode is one of InputLiteral / InputKeys / InputSignal (G-4).
   SendInput(ctx context.Context, sessionID string, in SessionInput) error

   // ResizeSession communicates a new viewport geometry to the session
   // (cols × rows). Native tmux is a no-op (tmux owns geometry);
   // Docker / k8s / microVM adaptors translate to their resize primitive.
   // Resize is intentionally its own method — never smuggled through
   // SendInput (G-6).
   ResizeSession(ctx context.Context, sessionID string, cols, rows int) error

   // StreamSession returns a channel of SessionEvents for the given
   // session. The adaptor closes the channel on ctx cancellation OR when
   // the underlying transport detaches. Refcounted fan-out (G-7) is an
   // adaptor-side implementation detail; callers see one channel per
   // call, but adaptors MAY share underlying IO across subscribers.
   // sessionID is opaque (C-21) — adaptors resolve it internally.
   StreamSession(ctx context.Context, sessionID string) (<-chan SessionEvent, error)
   ```

   Add a paragraph to the existing `OrchestrationPlaneAdaptor` doc block above the interface keyword referencing the new session-IO trio and pointing at `UnsupportedSessionIO` as the canonical default-noop helper.

2. In `core/session_io.go`, add `UnsupportedSessionIO` mixin struct:

   ```go
   // UnsupportedSessionIO is the default-noop mixin every adaptor that
   // has not implemented session IO embeds (G-9). The three methods
   // return a tagged *AdaptorError{Kind: KindUnsupported} so the
   // capability-negotiation UI hides the control and conformance Group F
   // (gm-faz) passes. Phase A embeds this in every existing adaptor;
   // Phase B native-tmux overrides StreamSession + SendInput + provides
   // a real (no-op) ResizeSession.
   type UnsupportedSessionIO struct{}

   func (UnsupportedSessionIO) SendInput(context.Context, string, SessionInput) error {
       return NewAdaptorError(KindUnsupported, "adaptor does not implement SendInput")
   }

   func (UnsupportedSessionIO) ResizeSession(context.Context, string, int, int) error {
       return NewAdaptorError(KindUnsupported, "adaptor does not implement ResizeSession")
   }

   func (UnsupportedSessionIO) StreamSession(context.Context, string) (<-chan SessionEvent, error) {
       return nil, NewAdaptorError(KindUnsupported, "adaptor does not implement StreamSession")
   }
   ```

Expected state after this commit: `go build ./core/...` is GREEN (core itself satisfies). `go build ./...` will be RED because the five in-tree implementations of OrchestrationPlaneAdaptor no longer satisfy the interface — that is fixed in Task A.3 within the same wave. Note this expectation in the commit body so future bisect doesn't blame this commit unfairly.
  </action>
  <verify>
    <automated>go build ./core/... && go vet ./core/...</automated>
  </verify>
  <done>
- `OrchestrationPlaneAdaptor` interface has the three new methods (`grep -n 'SendInput\|ResizeSession\|StreamSession' core/orchestration.go` returns matches inside the interface block)
- `UnsupportedSessionIO` is exported from core (`grep -n 'type UnsupportedSessionIO' core/session_io.go`)
- `go build ./core/...` green; `go build ./...` may be RED with "does not implement OrchestrationPlaneAdaptor" — that is the expected pre-A.3 state
- Commit message: `feat(core): extend OrchestrationPlaneAdaptor with SendInput/ResizeSession/StreamSession + default mixin (gm-v01.2.2)`
  </done>
</task>

<task type="auto">
  <name>Task A.3: Adapter sweep — embed UnsupportedSessionIO in every implementation</name>
  <bead>gm-v01.2.3</bead>
  <files>internal/adapter/native/orchestration.go, internal/adapter/gt/agents.go, internal/adapter/mock/plane.go, internal/adapter/noop/orchestration.go, core/orchestration_test.go</files>
  <action>
For each of the five in-tree implementations of `core.OrchestrationPlaneAdaptor` (identified via `grep -rn 'var _ \(core\.\)\?OrchestrationPlaneAdaptor' core/ internal/`), embed `core.UnsupportedSessionIO` as an anonymous field on the receiver struct so the three new interface methods are satisfied by promotion. Touch ONLY the struct declaration — do not move fields, do not reformat unrelated code.

Specific edits:

1. `internal/adapter/native/orchestration.go` — `OrchestrationPlane` struct: add `core.UnsupportedSessionIO` as first anonymous field. Update the `var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)` line (line ~104) — it already exists, just confirm it still compiles.

2. `internal/adapter/gt/agents.go` (or wherever the gt `OrchestrationPlane` struct is declared — check `grep -n 'type OrchestrationPlane struct' internal/adapter/gt/`): embed `core.UnsupportedSessionIO`. The compliance check is at gt/agents.go:128 — verify it still passes.

3. `internal/adapter/mock/plane.go` — `OrchestrationPlane` struct: embed `core.UnsupportedSessionIO`. Compliance check at line ~345.

4. `internal/adapter/noop/orchestration.go` — `OrchestrationPlane` struct: embed `core.UnsupportedSessionIO`. Compliance check at line ~86.

5. `core/orchestration_test.go` — the test fixture `noopOrchestrator` (line ~336): embed `UnsupportedSessionIO` (no `core.` prefix — same package).

Then sweep for any other implementor that might exist as a test-only fake. Run:

```bash
grep -rn 'var _ \(core\.\)\?OrchestrationPlaneAdaptor' core/ internal/ testing/ web/ cmd/ 2>/dev/null
```

If any compliance check exists beyond the five above, embed the mixin in that struct too. If a struct implements the interface implicitly (no explicit `var _` check) it will surface as a build error — fix it the same way.

Do NOT override any of the three methods on any adapter in this phase — leaving them as `UnsupportedSessionIO`-promoted is the correct Phase A endpoint per CONTEXT.md scope. Phase B will override on native; Phase B+ on gt etc.
  </action>
  <verify>
    <automated>go build ./... && go test ./core/... ./internal/adapter/...</automated>
  </verify>
  <done>
- `go build ./...` is GREEN — interface fully satisfied across the tree
- `go test ./core/... ./internal/adapter/...` is GREEN — no regressions
- Every implementor of OrchestrationPlaneAdaptor embeds `core.UnsupportedSessionIO` (grep verifies)
- All `var _ core.OrchestrationPlaneAdaptor = ...` compliance checks still compile
- Commit message: `feat(adapters): embed UnsupportedSessionIO mixin in native/gt/mock/noop + test fakes (gm-v01.2.3)`
  </done>
</task>

</tasks>

<verification>
After Task A.3 the tree must be fully green:

```bash
go build ./...
go test ./core/... ./internal/adapter/...
```

Both must exit 0. No skipped tests. No `-tags` gymnastics.
</verification>

<success_criteria>
1. `core` package exports `SessionInputMode` (with `InputLiteral`/`InputKeys`/`InputSignal`), `SessionInput`, `SessionEvent`, and the extended `OrchestrationPlaneAdaptor` interface with `SendInput`/`ResizeSession`/`StreamSession`.
2. `core` package exports `UnsupportedSessionIO` mixin returning `*AdaptorError{Kind: KindUnsupported}` for all three methods.
3. Every in-tree adapter (native, gt, mock, noop) and the `core/orchestration_test.go` `noopOrchestrator` test fixture embed `UnsupportedSessionIO`.
4. Calling any of the three new methods on any of those adapters returns an error satisfying `core.AsAdaptorError(err).Kind == core.KindUnsupported`.
5. `sessionID` remains opaque — every doc comment in the new interface block reinforces C-21; no caller code is changed to treat it as a tmux pane id.
6. `go build ./...` and `go test ./core/... ./internal/adapter/...` are green.
7. Three atomic commits, each tagged with the matching bead id in its trailer.
</success_criteria>

<output>
After completion, create `.planning/phases/A-core-sessionio/A-01-SUMMARY.md` recording:
- The exact final type/method signatures landed
- The five files where the mixin was embedded
- Confirmation that no method was overridden in Phase A (so Phase B knows it owns the first real implementation)
- Three commit shas with their bead ids
</output>
