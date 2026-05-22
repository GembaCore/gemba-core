---
phase: B-native-tmux
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/adapter/native/backend/backend.go
  - internal/adapter/native/backend/tmux.go
  - internal/adapter/native/backend/tmux_test.go
  - internal/adapter/native/session_io.go
  - internal/adapter/native/session_io_test.go
  - internal/adapter/native/orchestration.go
autonomous: true
requirements: [NATIVE-01, NATIVE-02, NATIVE-03]

must_haves:
  truths:
    - "Backend exposes a typed SendInput verb that distinguishes literal/keys/signal modes (not just SendKeys)."
    - "Native adapter's SendInput resolves an opaque sessionID to a tmux pane id inside backend/ only, then dispatches by Mode."
    - "Native adapter's ResizeSession returns nil (documented no-op) instead of KindUnsupported."
    - "Tmux SIGINT is delivered as 'C-c'; SIGTERM as 'C-\\'; SIGQUIT as 'C-\\\\' (configurable map)."
    - "go test ./core/... still passes — opacity guard does not trip."
  artifacts:
    - path: "internal/adapter/native/backend/backend.go"
      provides: "Backend.SendInput method on the interface (typed mode dispatch)"
      contains: "SendInput(ctx context.Context, sessionID string, in core.SessionInput) error"
    - path: "internal/adapter/native/backend/tmux.go"
      provides: "Tmux.SendInput implementation: literal uses 'send-keys -l --'; keys uses 'send-keys --'; signal maps via signalToKey table"
      contains: "func (t *Tmux) SendInput"
    - path: "internal/adapter/native/session_io.go"
      provides: "OrchestrationPlane.SendInput + ResizeSession overrides of the UnsupportedSessionIO mixin"
      contains: "func (o *OrchestrationPlane) SendInput"
  key_links:
    - from: "internal/adapter/native/session_io.go"
      to: "internal/adapter/native/backend/tmux.go"
      via: "lookupSessionPane -> backend.SendInput(paneID, in)"
      pattern: "paneID.*SendInput"
    - from: "internal/adapter/native/backend/tmux.go (signal mode)"
      to: "tmux send-keys"
      via: "signalToKey map (SIGINT->C-c, SIGTERM->C-\\, SIGQUIT->C-\\\\)"
      pattern: "signalToKey"
---

<objective>
Land the input path of the native tmux SessionIO: extend the Backend
interface with a typed SendInput verb, implement it on the Tmux backend
for literal/keys/signal modes, then override the UnsupportedSessionIO
mixin on the native OrchestrationPlane for SendInput + ResizeSession.

Purpose: After this plan, an operator (or the upcoming HTTP /input
endpoint) can inject a SessionInput payload through the core interface
and have the bytes / named keys / signal-as-keys land on the tmux pane.
ResizeSession becomes a real (no-op) method so callers stop seeing
KindUnsupported on a verb tmux does not own.

Output: SendInput + ResizeSession overrides committed, unit tests cover
each Mode + the signal map, opacity guard still green.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/B-native-tmux/CONTEXT.md
@core/session_io.go
@core/orchestration.go
@internal/adapter/native/orchestration.go
@internal/adapter/native/backend/backend.go
@internal/adapter/native/backend/tmux.go
@internal/adapter/native/start.go
@core/session_id_opacity_test.go
@docs/process/gsd-beads-bridge.md

<interfaces>
<!-- Phase A types the adapter must satisfy (verbatim from core/session_io.go). -->

```go
// core/session_io.go
type SessionInputMode string
const (
    InputLiteral SessionInputMode = "literal"
    InputKeys    SessionInputMode = "keys"
    InputSignal  SessionInputMode = "signal"
)
type SessionInput struct {
    Keys string           `json:"keys"`
    Mode SessionInputMode `json:"mode"`
}

// UnsupportedSessionIO mixin methods to OVERRIDE:
// SendInput(context.Context, string, SessionInput) error
// ResizeSession(context.Context, string, int, int) error
```

```go
// internal/adapter/native/backend/backend.go (existing)
type Backend interface {
    Name() string
    ListPanes(ctx context.Context) ([]Pane, error)
    SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error)
    SendKeys(ctx context.Context, sessionID string, keys string) error  // legacy — keep
    CapturePane(ctx context.Context, sessionID string, lines int) (string, error)
    Kill(ctx context.Context, sessionID string) error
}
```

Existing helper inside `internal/adapter/native/orchestration.go`:
`func (o *OrchestrationPlane) lookupSessionPane(sessionID string) (string, *core.Session, error)`
— use this as the single resolution point (it lives in the adapter package, not in `core`, which keeps opacity intact: the pane id is derived from `sess.ProviderMetadata["pane_id"]`, never from the sessionID string).
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Extend Backend interface + Tmux.SendInput (literal / keys / signal)</name>
  <bead>gm-v01.3.1</bead>
  <files>internal/adapter/native/backend/backend.go, internal/adapter/native/backend/tmux.go, internal/adapter/native/backend/tmux_test.go</files>
  <behavior>
    - Backend interface gains `SendInput(ctx, sessionID, in core.SessionInput) error`. The legacy `SendKeys` stays on the interface (StartSession + bridge code still call it via the "Enter"-suffix convention) — do NOT remove it in this task.
    - Tmux.SendInput dispatches by `in.Mode`:
      - InputLiteral: shell out `tmux send-keys -t <pane> -l -- <keys>`. The `-l` flag tells tmux to treat input as literal bytes (no key-name interpretation). Multi-line and unicode payloads must round-trip; if the payload is large (>256 bytes) or contains `\n`, route through the existing `pasteText` (load-buffer + paste-buffer -d) path so quoting + signal stripping stay safe.
      - InputKeys: shell out `tmux send-keys -t <pane> -- <keys>`. Caller-supplied named keys (`Enter`, `C-c`, `Up`, `M-x`, …) are passed through; tmux owns the name table.
      - InputSignal: translate via `signalToKey` map then call the InputKeys path. Map MUST cover: SIGINT->"C-c", SIGTERM->"C-\\", SIGQUIT->"C-\\\\", SIGTSTP->"C-z", SIGHUP->"C-d" (best-effort surrogate — comment why). Unknown signal -> return `core.NewAdaptorError(core.KindValidation, "tmux: signal %q unsupported", in.Keys)`.
    - Validation: empty paneID -> error (mirrors existing SendKeys); empty keys for InputLiteral/InputKeys -> error; InputSignal with empty keys -> error.
    - Tests (tmux_test.go) — DO NOT spawn real tmux; inject a fake runner. Build on the existing `run`/`runWithStdin` plumbing: extract a `runner` function field on `Tmux` so tests can capture argv. Cases:
      - literal short string -> argv contains `send-keys -t %0 -l --`.
      - literal with `\n` -> goes via load-buffer + paste-buffer (assert load-buffer argv seen).
      - literal with unicode (emoji + accented) -> argv carries the raw bytes verbatim.
      - keys "Enter" -> argv `send-keys -t %0 -- Enter` (NO `-l`).
      - keys "C-c" -> argv `send-keys -t %0 -- C-c`.
      - signal "SIGINT" -> argv ends in `-- C-c`.
      - signal "SIGTERM" -> argv ends in `-- C-\\`.
      - signal "BOGUS" -> returns KindValidation error, argv runner NOT invoked.
      - empty paneID -> returns error, runner NOT invoked.
  </behavior>
  <action>
    Add `SendInput(ctx context.Context, sessionID string, in core.SessionInput) error` to the `Backend` interface in backend.go. Implement on `*Tmux` per the behavior table. Introduce an unexported `signalToKey` map at file scope in tmux.go with the mappings listed above and a comment explaining each surrogate (tmux has no real signal verb; we emit the corresponding terminal control key). Refactor `Tmux` to hold an injectable `runner func(ctx, stdin, args...) ([]byte, error)` field defaulting to the current `runWithStdin`; existing callers continue to work unchanged. Do NOT remove `SendKeys` — start.go's preamble-injection path still uses it.
    Implements decision per CONTEXT.md scope item 1 ("Dispatch by in.Mode") and bead gm-v01.3.1.
  </action>
  <verify>
    <automated>cd /Users/mikebengtson/Documents/GitHub/gemba && go test ./internal/adapter/native/backend/... -run SendInput -count=1 && go build ./...</automated>
  </verify>
  <done>
    Backend interface includes SendInput; Tmux.SendInput passes all 9 test cases above; legacy SendKeys still compiles and its existing tests still pass; `go build ./...` green; no goroutine leaks (tests use t.Cleanup to drain the runner channel if any).
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Native adapter SendInput + ResizeSession overrides</name>
  <bead>gm-v01.3.2</bead>
  <files>internal/adapter/native/session_io.go, internal/adapter/native/session_io_test.go, internal/adapter/native/orchestration.go</files>
  <behavior>
    - New file `internal/adapter/native/session_io.go` carries the Phase B overrides for the UnsupportedSessionIO mixin. The mixin stays embedded on `OrchestrationPlane`; Go method-resolution prefers the explicit method on the outer struct, so adding SendInput/ResizeSession here shadows the mixin without removing the embed (StreamSession stays unsupported until plan 03).
    - `func (o *OrchestrationPlane) SendInput(ctx, sessionID, in core.SessionInput) error`:
      - Backend nil -> `unsupported("SendInput")` (mirrors existing StartSession behavior so the zero-config adapter still degrades cleanly).
      - in.Mode is not one of the three locked SessionInputMode values -> `core.NewAdaptorError(core.KindValidation, "native: unknown SessionInputMode %q", in.Mode)`.
      - Resolve via `o.lookupSessionPane(sessionID)` (existing helper). Unknown session -> propagate its KindSessionNotFound unchanged.
      - Dispatch: `o.cfg.Backend.SendInput(ctx, paneID, in)`. Wrap non-typed errors with `core.WrapAdaptorError(core.KindProcessFailed, err, "native: send-input %s", sessionID)`.
    - `func (o *OrchestrationPlane) ResizeSession(ctx, sessionID, cols, rows int) error`:
      - Backend nil -> `unsupported("ResizeSession")` (consistent with other Backend-gated verbs).
      - Validate sessionID non-empty + cols/rows > 0 (KindValidation on violation). The validation matters for the contract; the no-op is just the body.
      - Confirm the session exists via `lookupSessionPane` so callers get KindSessionNotFound on an unknown id (don't silently return nil for nonexistent sessions — that would mask bugs).
      - Documented no-op: tmux owns geometry server-side; the method exists so container/k8s backends can override it later. Comment block in the function MUST cite CONTEXT.md scope item 2.
    - `OrchestrationPlane` struct comment in orchestration.go updated: "Phase B overrides SendInput + ResizeSession; StreamSession still inherits the mixin until plan 03."
    - Tests (session_io_test.go) — use a fake Backend implementing the new SendInput verb (the testdata package already has fakes for the existing methods; extend or add a minimal `fakeBackend` local to the test file). Cases:
      - SendInput with Backend=nil -> KindUnsupported.
      - SendInput with unknown Mode -> KindValidation, fakeBackend.SendInput NOT called.
      - SendInput with unknown sessionID -> KindSessionNotFound.
      - SendInput happy path (InputLiteral) -> fakeBackend records (paneID, in) once.
      - SendInput when backend returns a non-typed error -> wrapped to KindProcessFailed.
      - ResizeSession Backend=nil -> KindUnsupported.
      - ResizeSession empty sessionID -> KindValidation.
      - ResizeSession cols=0 -> KindValidation.
      - ResizeSession unknown session -> KindSessionNotFound.
      - ResizeSession happy path -> returns nil; no Backend method invoked (no-op).
  </behavior>
  <action>
    Create `internal/adapter/native/session_io.go` with the two override methods per the behavior block. Update the doc comment on `OrchestrationPlane.UnsupportedSessionIO` embed in orchestration.go to reflect that SendInput + ResizeSession are now overridden (StreamSession still mixed-in until plan 03). Create `internal/adapter/native/session_io_test.go` covering all 10 cases. Implements bead gm-v01.3.2 + gm-v01.3.3.
    Naming note: do NOT add a `core.NewAdaptorError` helper or otherwise extend `core/` — Phase A locked the surface. All new logic stays under `internal/adapter/native/`.
  </action>
  <verify>
    <automated>cd /Users/mikebengtson/Documents/GitHub/gemba && go test ./internal/adapter/native/... -run "SendInput|ResizeSession" -count=1 && go test ./core/... -run TestSessionIDOpacity -count=1 && go build ./...</automated>
  </verify>
  <done>
    OrchestrationPlane.SendInput + ResizeSession compile and satisfy core.OrchestrationPlaneAdaptor (the existing `var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)` assertion still holds). All 10 test cases pass. Opacity guard test still green (no leak of pane-id resolution outside `internal/adapter/native/backend/`). `go build ./...` green.
  </done>
</task>

</tasks>

<verification>
- `go build ./...` green.
- `go test ./internal/adapter/native/... -count=1` green (existing tests + new SendInput/ResizeSession suites).
- `go test ./core/... -count=1` green (opacity guard test still passes — pane-id resolution remains inside the allowlisted backend dir).
- `grep -rn "func.*SendInput" internal/adapter/native/ | grep -v _test.go` shows exactly 2 hits: the Backend method on `*Tmux` and the OrchestrationPlane override.
- Legacy `start.go` still compiles unchanged (preamble injection still uses `Backend.SendKeys`).
</verification>

<success_criteria>
1. Backend interface carries SendInput; Tmux backend implements it for literal/keys/signal with correct argv per the test matrix.
2. Native adapter overrides SendInput + ResizeSession on the UnsupportedSessionIO mixin; both return typed errors on the failure paths and proceed normally on the happy paths.
3. Opacity guard test in core/session_id_opacity_test.go still passes (pane id resolution stays inside internal/adapter/native/backend/ + the adapter's existing lookupSessionPane helper which reads ProviderMetadata).
4. Beads gm-v01.3.1, gm-v01.3.2, gm-v01.3.3 ready to close after this plan ships.
</success_criteria>

<output>
After completion, create `.planning/phases/B-native-tmux/B-01-SUMMARY.md` capturing:
- Files added/changed (with line counts).
- The signalToKey mapping table actually shipped.
- Confirmation that StreamSession is still the mixin default (plan 03 wires it).
- Bead notes posted to gm-v01.3.1, gm-v01.3.2, gm-v01.3.3.
</output>
