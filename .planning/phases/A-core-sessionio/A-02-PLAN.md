---
phase: A-core-sessionio
plan: 02
type: execute
wave: 2
depends_on: [A-01]
files_modified:
  - core/session_io_test.go
  - internal/adapter/native/orchestration_test.go
  - internal/adapter/mock/plane_test.go
  - internal/adapter/noop/orchestration_test.go
  - internal/adapter/gt/sessions_test.go
  - core/session_id_opacity_test.go
autonomous: true
requirements: [CORE-01, CORE-02, CORE-03, CORE-04]
bead: gm-v01.2
must_haves:
  truths:
    - "Unit tests assert SessionInputMode has exactly three string values (InputLiteral, InputKeys, InputSignal)"
    - "Unit tests assert SessionEvent.Kind documents four wire tokens (output, status, exit, disconnect)"
    - "Unit tests assert UnsupportedSessionIO's three methods return *AdaptorError{Kind: KindUnsupported}"
    - "Each adapter (native, gt, mock, noop) has a test confirming its SendInput/ResizeSession/StreamSession return KindUnsupported"
    - "A grep-driven guard test fails if any non-adapter caller treats sessionID as a tmux pane id"
    - "go test ./core/... ./internal/adapter/... passes"
  artifacts:
    - path: "core/session_io_test.go"
      provides: "Type-value + UnsupportedSessionIO contract tests"
    - path: "core/session_id_opacity_test.go"
      provides: "Grep-based guard that sessionID stays opaque across the tree"
    - path: "internal/adapter/{native,gt,mock,noop}/*_test.go"
      provides: "Per-adapter default-unsupported behavior assertions"
  key_links:
    - from: "core/session_io_test.go"
      to: "core.UnsupportedSessionIO"
      via: "instantiates the zero-value mixin and asserts the three returned errors"
      pattern: "UnsupportedSessionIO\\{\\}"
    - from: "core/session_id_opacity_test.go"
      to: "tree-wide caller code"
      via: "filepath.Walk + regex denylist for pane-id assumptions"
      pattern: "tmux.*pane.*sessionID|sessionID.*pane"
---

<objective>
Lock in the Phase A contract with tests that future phases cannot accidentally regress. Covers (a) type-value assertions, (b) per-adapter default-unsupported behavior, and (c) the C-21 opacity guarantee for `sessionID`.

Purpose: A future phase adding a real `SendInput` to native (Phase B) must not also accidentally make `gt` or `mock` start succeeding silently; and no transport-layer change may leak a pane-id assumption to a caller. These tests are cheap insurance against both.

Output: New test files in `core/` and four adapter packages, all green.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/A-core-sessionio/CONTEXT.md
@.planning/phases/A-core-sessionio/A-01-PLAN.md
@.planning/phases/A-core-sessionio/A-01-SUMMARY.md
@core/session_io.go
@core/errors.go
@core/orchestration.go

Sequencing: depends_on A-01 (the types, interface extension, mixin, and adapter sweep must already be landed). This plan only adds tests — no production code changes. Two atomic commits, one per task.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task A.4: Unit tests — type values + default-unsupported behavior</name>
  <bead>gm-v01.2.4</bead>
  <files>core/session_io_test.go, internal/adapter/native/orchestration_test.go, internal/adapter/mock/plane_test.go, internal/adapter/noop/orchestration_test.go, internal/adapter/gt/sessions_test.go</files>
  <behavior>
- `SessionInputMode` constants equal the wire strings `"literal"`, `"keys"`, `"signal"` (exact, lowercase, no extras).
- Documented `SessionEvent.Kind` tokens are exactly `"output"`, `"status"`, `"exit"`, `"disconnect"` — encoded as a `var SessionEventKinds = []string{...}` exported from core so test + future code can iterate without restating the list. (Optional add to core/session_io.go if not already present from A.1; if absent, add in this commit alongside the test that asserts it.)
- `UnsupportedSessionIO{}.SendInput(ctx, "sid", SessionInput{Mode: InputLiteral, Keys: "x"})` returns a non-nil error whose `core.AsAdaptorError(err).Kind == core.KindUnsupported`.
- Same assertion for `ResizeSession(ctx, "sid", 80, 24)` and `StreamSession(ctx, "sid")` (the latter returns a nil channel + the unsupported error).
- For each of the four production adapters (native, gt, mock, noop): instantiate the adapter with whatever its existing test helper uses (look at the existing `*_test.go` patterns in that package), call each of the three methods against a dummy sessionID, and assert `KindUnsupported`. These per-adapter tests catch the case where a future phase removes the mixin embed but forgets to override.
  </behavior>
  <action>
Create `core/session_io_test.go` with three top-level subtests under one `TestSessionIOContract` table:

1. `TestSessionIOContract/InputModeTokens` — assert string values of `InputLiteral` / `InputKeys` / `InputSignal` are `"literal"` / `"keys"` / `"signal"`.
2. `TestSessionIOContract/EventKindTokens` — assert `SessionEventKinds` (exported slice in core/session_io.go) contains exactly `"output"`, `"status"`, `"exit"`, `"disconnect"`. If the slice isn't yet exported, add it in the same commit alongside this test — it's a trivial complement to A.1 and rightly belongs with the assertion that pins the four values.
3. `TestSessionIOContract/UnsupportedMixin` — table-driven across the three methods; each row calls the method on a zero-value `UnsupportedSessionIO{}` and asserts the error round-trips through `core.AsAdaptorError` with `Kind == KindUnsupported`.

Then in each adapter package add `TestSessionIO_DefaultUnsupported` (or extend an existing `_test.go` file — pick the one already exercising the adapter's lifecycle, e.g. `native/orchestration_test.go`, `mock/plane_test.go`, `noop/orchestration_test.go`, `gt/sessions_test.go`). Each test:

- Constructs the adapter via the same helper its sibling tests use (no new ctor wiring — reuse).
- Calls `SendInput`, `ResizeSession`, `StreamSession` against a synthetic sessionID like `"sid-phase-a"`.
- Asserts each returns `core.KindUnsupported`.

Where the adapter's existing test setup is heavy (e.g. native spins up a backend), keep the new test lightweight: call the method on a freshly-constructed plane with no live session — the unsupported error fires before any session lookup, so no live state is required.

Do NOT add any production overrides. The point of this test is precisely that Phase A leaves all four adapters in default-unsupported state.
  </action>
  <verify>
    <automated>go test ./core/... ./internal/adapter/native/... ./internal/adapter/gt/... ./internal/adapter/mock/... ./internal/adapter/noop/...</automated>
  </verify>
  <done>
- `TestSessionIOContract` passes with three subtests
- Four adapter packages each have a `TestSessionIO_DefaultUnsupported` (or equivalent) that asserts `KindUnsupported` on all three methods
- `SessionEventKinds` exported slice exists in core/session_io.go (added here if not already)
- Commit message: `test(core): pin SessionInputMode/SessionEvent tokens + per-adapter default-unsupported behavior (gm-v01.2.4)`
  </done>
</task>

<task type="auto">
  <name>Task A.5: Opacity guard — grep-based test that sessionID stays opaque</name>
  <bead>gm-v01.2.5</bead>
  <files>core/session_id_opacity_test.go</files>
  <action>
Create `core/session_id_opacity_test.go` with a single test `TestSessionIDOpacity` that walks the tree from the module root and fails if any non-allowlisted file contains code suggesting a caller treats `sessionID` as a tmux pane id.

Implementation:

1. Find module root by walking up from `runtime.Caller(0)` until a `go.mod` is seen (or use `os.Getenv("PWD")` fallback — Go tests run from the package dir, so `../..` from `core/` works as a simple anchor).

2. `filepath.WalkDir` from module root, visiting only `*.go` files. Skip:
   - `vendor/`, `node_modules/`, `web/`, `.planning/`, `docs/`, `docs-site/`, `cmd/gen-*` (codegen output), `_test.go` files (tests legitimately stage pane ids in mocks).
   - Files under `internal/adapter/native/backend/` — that's the tmux adapter's internals where the pane id IS the implementation; the opacity rule is for callers, not the backend itself.
   - The new file itself (`core/session_id_opacity_test.go`).

3. For each remaining file, read it once and check for forbidden regex patterns (compile once):
   - `sessionID\s*[:=].*paneID` — direct assignment of a pane id into a sessionID variable
   - `sessionID.*\.PaneID\b` — pulling `.PaneID` off a struct keyed by sessionID
   - `paneID\s*[:=]\s*sessionID\b` — converse: treating sessionID as if it IS a pane id
   - `tmux .*-t .*sessionID\b` — sessionID interpolated into a tmux `-t` flag (that's a target-pane flag and would be a leak)

4. On any match, `t.Errorf("opacity violation in %s:%d: %s", path, lineNum, line)` — emit ALL violations, don't stop at first.

5. Test the test: include a sanity assertion that the walker actually visited >= 50 files (so a broken walker doesn't silently pass).

Document the rationale in a leading file comment pointing at CONTEXT.md decision C-21 ("sessionID stays opaque across the interface; adapters resolve to their own identity internally"). Phase B's native tmux work will add the pane-id↔sessionID mapping inside `internal/adapter/native/backend/tmux.go`, which is allowlisted.

This test runs as part of `go test ./core/...` so any future PR that leaks a pane assumption fails CI before merge.
  </action>
  <verify>
    <automated>go test ./core/... -run TestSessionIDOpacity -v</automated>
  </verify>
  <done>
- `TestSessionIDOpacity` exists and passes against current main
- Walker visits >= 50 .go files (sanity sub-assertion)
- Allowlist documented in a top-of-file comment with the rationale + CONTEXT decision id
- Commit message: `test(core): guard sessionID opacity across the tree (gm-v01.2.5)`
  </done>
</task>

</tasks>

<verification>
```bash
go test ./core/... ./internal/adapter/...
```
Must exit 0. The opacity test in particular must visit the full tree (not silently skip due to a bad walker) — assert via the >= 50 files sanity check.
</verification>

<success_criteria>
1. Phase A's type set, mixin, and per-adapter default-unsupported behavior are pinned by tests that fail loudly if a future phase regresses them.
2. The C-21 opacity guarantee for `sessionID` is enforced by a grep-driven test that runs on every `go test ./core/...` invocation.
3. Two atomic commits, each tagged with its bead id.
4. No production code changes beyond (optionally) adding the `SessionEventKinds` exported slice if A.1 didn't already include it.
</success_criteria>

<output>
After completion, create `.planning/phases/A-core-sessionio/A-02-SUMMARY.md` recording:
- The exact test names landed and which file/package they live in
- The opacity test's allowlist as a deliverable artifact (so Phase B knows what's permitted)
- Two commit shas with their bead ids
- Confirmation that `go test ./core/... ./internal/adapter/...` is green
- A note flagging which Phase B / Phase C edits will need to be aware of the opacity guard (e.g. the HTTP `/input` handler must keep sessionID as a string passthrough, not parse it)
</output>
