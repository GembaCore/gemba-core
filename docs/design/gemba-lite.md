# gemba-lite — Native dispatch workspace

**Goal:** Ship a workable native (tmux-backed) dispatch release whose
primary UX is a desktop-style **session workspace** — operator-driven
or beads-driven, both surfaced through one three-pane view.

## Scope (v1)

The user opens `/sessions`. They can stay in the existing **Table** mode
(unchanged) or switch to a new **Workspace** mode. In Workspace mode:

- Left rail: live sessions, grouped Live / Ended, status dots, +New.
- Middle: live terminal for the selected session (xterm.js), tabs for
  Terminal · Logs · Diff.
- Right: stacked status cards — Assignment, Worktree, Git status,
  Beads picked up, Escalations.

The terminal is **interactive from v1**, implemented via tmux primitives
(no PTY allocation in the server):

- **Output** — `tmux pipe-pane` (preferred) or polled `capture-pane`
  → server streams to browser as SSE.
- **Input** — browser POSTs keystrokes; server calls
  `Backend.SendKeys()` which already exists for the tmux backend. Use
  literal (`-l`) mode for printable input; named keys (`C-c`, `Up`,
  `Enter`) for control sequences.

This avoids opening a raw stdin pipe to a shell. Every input is one
auditable `SendKeys` call.

## Non-goals (v1)

- No new top-level nav surface. Workspace lives under `/sessions`.
- No new auth model. The streaming + input endpoints reuse the same
  middleware as `/api/sessions`.
- No multi-cursor / collaborative editing. Single operator per browser
  session; tmux itself handles concurrent input serialization.
- No webhooks / external triggers beyond what beads provides today.

## Core interface additions

Two methods + one resize on `core.OrchestrationPlaneAdaptor`. Designed
once for the full runtime matrix: native tmux today, Docker / k8s /
microVM later. The HTTP handler stays identical across runtimes; only
the adapter implementation differs.

```go
// core/orchestration.go (additions)

type SessionInputMode string
const (
    InputLiteral SessionInputMode = "literal" // typed chars
    InputKeys    SessionInputMode = "keys"    // named keys: Enter, C-c, Up
    InputSignal  SessionInputMode = "signal"  // SIGINT/SIGTERM — for non-TTY contexts
)

type SessionInput struct {
    Keys string
    Mode SessionInputMode
}

type SessionEvent struct {
    // output  — pane bytes
    // status  — session status transition (working/prompting/idle)
    // exit    — process gone; do not reconnect
    // disconnect — transient; client should retry the stream
    Kind  string
    Bytes []byte
    Meta  map[string]any
}

type OrchestrationPlaneAdaptor interface {
    // ...existing...
    SendInput(ctx context.Context, sessionID string, in SessionInput) error
    ResizeSession(ctx context.Context, sessionID string, cols, rows int) error
    StreamSession(ctx context.Context, sessionID string) (<-chan SessionEvent, error)
}
```

Adapters that genuinely can't satisfy a method return
`core.KindUnsupported`; the SPA renders read-only fallback.

### Adapter implementation matrix

| Adapter | `SendInput` literal | `SendInput` keys | `SendInput` signal | `ResizeSession` | `StreamSession` |
|---|---|---|---|---|---|
| native (tmux) | `send-keys -l` | `send-keys` | maps to `C-c` etc. | no-op (tmux owns geometry) | `pipe-pane` → channel |
| docker | write stdin via `attach` | write stdin bytes | `docker kill -s` | resize API call | `logs -f` for non-TTY; `attach` for TTY |
| k8s | `pods/exec` stdin frame | `pods/exec` stdin frame | pod `eviction` / signal via exec | `pods/exec` resize frame | **`pods/attach`**, NOT `logs -f` (logs misses TTY output) |
| microVM (Firecracker) | serial-console write | serial-console write | guest signal via vsock | serial geometry | vsock stream |

### IO lifecycle — refcounted fan-out

One persistent IO channel per session inside the adapter, regardless of
how many SSE viewers attach. HTTP handler subscribes/unsubscribes; the
adapter tears down the underlying channel when refcount hits zero.

Why this and not per-call: native tmux makes per-call cheap, but k8s
`pods/exec` is a TLS+auth+WebSocket handshake — paying that per
keystroke or per viewer is unacceptable, and rewriting the lifecycle
once k8s lands is more painful than building it once now.

### WSID / multi-tenant boundary

When sessions move to a shared k8s cluster, `sessionID` stays opaque
to callers. The k8s adapter resolves
`sessionID → (workspace, namespace, pod, container)` against the
in-flight WSID registry on every call. Native tmux ignores the
workspace dimension.

## Backend changes

### New endpoints

1. **`GET /api/sessions/{id}/stream`** — SSE.
   Events:
   - `snapshot`: initial `capture-pane -p -e -J -S -2000` payload (so
     the operator gets backscroll on open).
   - `output`: incremental chunks from `pipe-pane`.
   - `status`: session status transitions (working/prompting/idle).

2. **`POST /api/sessions/{id}/input`** — body
   `{ keys: string, mode: "literal" | "keys" }`.
   - `literal` → `tmux send-keys -t <pane> -l -- <keys>`
   - `keys`    → `tmux send-keys -t <pane> -- <keys>` (named keys; the
     browser sends `Enter`, `C-c`, `Up`, etc.)
   - Auth: requires the standard confirm-nonce middleware. Every call
     emits an audit event for the right-pane "Beads picked up" /
     "Recent events" feeds.

3. **`GET /api/sessions/{id}/status`** — JSON snapshot for the right
   pane:
   - `worktree`: path, branch, ahead/behind
   - `git`: porcelain entries
   - `beads`: assignments touched in this session (claims, closes,
     comments) — derived from existing session event log
   - `assignment`: bead id + title (or `null` for manual sessions)

### Backend interface extensions

The existing `Backend` interface (`internal/adapter/native/backend`)
already covers `SendKeys`, `CapturePane`, `Kill`. We add **one**
optional extension for streaming:

```go
// Streamable is satisfied by backends that can stream pane output
// without polling. tmux satisfies it via `pipe-pane`; other backends
// fall back to capture-pane polling.
type Streamable interface {
    StreamPane(ctx context.Context, sessionID string) (<-chan []byte, error)
}
```

Non-tmux backends are served by a polling fallback in the HTTP layer
(`capture-pane` every 500ms, diff against last snapshot).

## Frontend changes

### Files added

```
web/src/views/sessions/
  SessionsWorkspace.tsx          # three-pane container
  SessionRail.tsx                # left rail
  SessionTerminal.tsx            # xterm.js + input wiring
  StatusPane.tsx                 # right card stack
  cards/
    AssignmentCard.tsx
    WorktreeCard.tsx
    GitStatusCard.tsx
    BeadsCard.tsx
    EscalationsCard.tsx
web/src/hooks/
  useSessionStream.ts            # SSE subscription → xterm writer
  useSessionInput.ts             # POST /input with batching
  useSessionStatus.ts            # /status polling
```

### Files modified

- `web/src/pages/SessionsPage.tsx` — add Table/Workspace toggle in
  header; route to `SessionsWorkspace` when toggle is on or
  `?session=<id>` is present.
- `web/src/components/sessions/NewSessionDialog.tsx` — add a "blank
  session in worktree" path (manual mode already exists in the API; the
  dialog currently requires a bead — extend it).

### New dependency

- `@xterm/xterm` + `@xterm/addon-fit` (terminal rendering only; we feed
  it the SSE stream and forward keystrokes to the input endpoint).

## Execution slices

Each slice is independently shippable behind a `?ws=1` flag until the
final cutover.

1. **Backend slice A — output streaming**
   - `Streamable` interface; tmux `pipe-pane` impl.
   - `GET /sessions/{id}/stream` SSE endpoint with capture-pane fallback.
2. **Backend slice B — input**
   - `POST /sessions/{id}/input` with literal + named-key modes.
   - Audit event on every call.
3. **Backend slice C — status snapshot**
   - `GET /sessions/{id}/status` (git porcelain + assignment + beads
     ledger projected from existing event log).
4. **Frontend slice D — terminal pane**
   - xterm.js wired to slices A + B. Standalone page at
     `/sessions/{id}/term` for early dogfooding.
5. **Frontend slice E — workspace shell**
   - Three-pane layout, rail, mode toggle on `SessionsPage`.
6. **Frontend slice F — right-pane cards**
   - Consumes slice C.
7. **Frontend slice G — blank-session dialog path**
   - Extends `NewSessionDialog` for the manual flow.

Slices A-C can land independently of D-G and unblock CLI tools that
want the same data. Slices D-G land sequentially behind the flag, then
flag-flip in one PR.

## Risks

- **`pipe-pane` lifecycle** — must be torn down when the session ends
  or the SSE client disconnects, else tmux leaks named-pipe files. We
  scope it to the request and use a `defer pipe-pane -O` on stream
  close.
- **Send-keys escaping** — multi-line paste, quotes, unicode. The
  `-l` (literal) flag handles printable input; control sequences go
  through the keyed path. Tests cover both.
- **xterm.js bundle weight** — ~120KB gzip. Acceptable for a power-user
  surface; loaded lazily on first Workspace toggle.
