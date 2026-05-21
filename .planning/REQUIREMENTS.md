# Requirements

Derived from the gemba-lite spec (G-1..G-10, precedence 0) and the gemba-lite execution slices A–G. Each requirement is mapped to one phase (see `ROADMAP.md`). Containerized-sessions constraints (C-1..C-21) are tracked as **future-proofing** constraints — they shape interface decisions in v1 but do not generate v1 requirements.

## v1 Requirements

### Core Interface (CORE)

- **CORE-01** — Add `SessionInputMode`, `SessionInput`, and `SessionEvent` types to `core/`. Source: G-1.
- **CORE-02** — Extend `core.OrchestrationPlaneAdaptor` with `SendInput(ctx, sessionID, in)`, `ResizeSession(ctx, sessionID, cols, rows)`, and `StreamSession(ctx, sessionID) (<-chan SessionEvent, error)`. Source: G-1.
- **CORE-03** — Adapter sweep: every existing adapter (native, docker, applescript, k8s, mcp, testadaptors) embeds an `unsupportedSessionIO` helper that returns `core.KindUnsupported`. Source: G-1, G-4.
- **CORE-04** — Adapter interface must keep `sessionID` opaque to callers (k8s future-proofing — WSID boundary). Source: G-6, C-18.

### Native tmux SessionIO (NATIVE)

- **NATIVE-01** — Implement `Streamable` interface on the tmux backend: `StreamPane(ctx, sessionID) (<-chan []byte, error)` backed by `tmux pipe-pane`. Source: G-2, G-4.
- **NATIVE-02** — Tear down `pipe-pane` deterministically on session end or stream disconnect (`defer pipe-pane -O`); zero named-pipe leaks under disconnect storms. Source: G-9.
- **NATIVE-03** — Implement native `SendInput`: literal mode → `send-keys -l`; keys mode → `send-keys`; signal mode → mapped key (e.g. `C-c`). Escaping covers multi-line paste, quotes, unicode. Source: G-4, G-9.
- **NATIVE-04** — Implement native `ResizeSession` as no-op (tmux owns geometry). Source: G-4.
- **NATIVE-05** — Refcounted IO fan-out: one persistent IO channel per session inside adapter; HTTP handlers subscribe/unsubscribe; tear down underlying channel at refcount 0. Source: G-5.

### HTTP Transport (HTTP)

- **HTTP-01** — `GET /api/sessions/{id}/stream` SSE endpoint emitting `snapshot` (initial `capture-pane -p -e -J -S -2000`), `output` (incremental from `pipe-pane`), and `status` (working/prompting/idle) events. Source: G-3.
- **HTTP-02** — `POST /api/sessions/{id}/input` accepting `{ keys: string, mode: "literal" | "keys" }`. Gated by the existing confirm-nonce middleware. Source: G-3.
- **HTTP-03** — Every `/input` call emits an audit event consumed by the right-pane Beads / Recent events feeds. Source: G-3.
- **HTTP-04** — `GET /api/sessions/{id}/status` returns JSON `{ worktree {path, branch, ahead, behind}, git: porcelain entries, beads: assignments touched, assignment: {bead_id, title} | null }`. Source: G-3.
- **HTTP-05** — HTTP polling fallback for non-`Streamable` backends: `capture-pane` every 500ms, diff vs last snapshot. Source: G-2.

### Terminal Pane (TERM)

- **TERM-01** — Add `@xterm/xterm` + `@xterm/addon-fit` dependency; load lazily on first Workspace toggle. Source: G-7, G-9.
- **TERM-02** — Implement `SessionTerminal.tsx` rendering xterm.js wired to `useSessionStream` (writes SSE output) and `useSessionInput` (POSTs keystrokes with batching). Source: G-7.
- **TERM-03** — Standalone dogfooding page at `/sessions/{id}/term` consuming HTTP-01 + HTTP-02 only. Source: G-8 (slice D).
- **TERM-04** — `useSessionStream.ts` hook: SSE subscription, reconnect on `disconnect` event, terminal write on `output`. Source: G-7.
- **TERM-05** — `useSessionInput.ts` hook: POST with mode discrimination (literal vs keys), batched flush. Source: G-7.

### Workspace Shell (WS)

- **WS-01** — `SessionsWorkspace.tsx` three-pane layout: `SessionRail` (left) · `SessionTerminal` (middle, with Terminal · Logs · Diff tabs) · `StatusPane` (right). Source: G-7.
- **WS-02** — `SessionRail.tsx`: live sessions list grouped Live / Ended, status dots, `+New` action. Source: G-7.
- **WS-03** — `SessionsPage.tsx` gains a Table/Workspace mode toggle in the header; routes to `SessionsWorkspace` when toggle is on OR `?session=<id>` is present. Source: G-7.
- **WS-04** — All workspace surfaces gated behind `?ws=1` flag during v1; existing Table mode unchanged. Source: G-8.

### Right-Pane Cards (CARDS)

- **CARDS-01** — `StatusPane.tsx` stacks status cards consuming `useSessionStatus`. Source: G-7.
- **CARDS-02** — `AssignmentCard.tsx` — renders bead id + title, or "Manual session" when null. Source: G-3, G-7.
- **CARDS-03** — `WorktreeCard.tsx` — path, branch, ahead/behind from `/status.worktree`. Source: G-3, G-7.
- **CARDS-04** — `GitStatusCard.tsx` — porcelain entries from `/status.git`. Source: G-3, G-7.
- **CARDS-05** — `BeadsCard.tsx` — assignments touched (claims / closes / comments) from `/status.beads`. Source: G-3, G-7.
- **CARDS-06** — `EscalationsCard.tsx` — surfaces escalation events from existing session event log. Source: G-7.
- **CARDS-07** — `useSessionStatus.ts` hook: polled fetch of `/status` with sensible interval. Source: G-7.

### Blank-Session Dialog (BLANK)

- **BLANK-01** — Extend `NewSessionDialog.tsx` with a "blank session in worktree" path (manual mode, no required bead). Source: G-7.
- **BLANK-02** — Dispatch path verified end-to-end: dialog → server creates worktree → tmux session spawned → operator lands in Workspace mode with live terminal. Source: success metric.
- **BLANK-03** — Flag-flip PR: once BLANK-02 is verified, remove `?ws=1` gate so Workspace becomes the default `/sessions` view. Source: G-8.

## Traceability

| Requirement | Phase | Bead | Status |
|-------------|-------|------|--------|
| CORE-01 | Phase A (Core) | gm-v01.2.1 | Pending |
| CORE-02 | Phase A (Core) | gm-v01.2.2 | Pending |
| CORE-03 | Phase A (Core) | gm-v01.2.3 | Pending |
| CORE-04 | Phase A (Core) | gm-v01.2 (epic) | Pending |
| NATIVE-01 | Phase B (Native tmux) | gm-v01.3 | Pending |
| NATIVE-02 | Phase B (Native tmux) | gm-v01.3 | Pending |
| NATIVE-03 | Phase B (Native tmux) | gm-v01.3 | Pending |
| NATIVE-04 | Phase B (Native tmux) | gm-v01.3 | Pending |
| NATIVE-05 | Phase B (Native tmux) | gm-v01.3 | Pending |
| HTTP-01 | Phase C (HTTP) | gm-v01.4 | Pending |
| HTTP-02 | Phase C (HTTP) | gm-v01.4 | Pending |
| HTTP-03 | Phase C (HTTP) | gm-v01.4 | Pending |
| HTTP-04 | Phase C (HTTP) | gm-v01.4 | Pending |
| HTTP-05 | Phase C (HTTP) | gm-v01.4 | Pending |
| TERM-01 | Phase D (Terminal) | gm-v01.5 | Pending |
| TERM-02 | Phase D (Terminal) | gm-v01.5 | Pending |
| TERM-03 | Phase D (Terminal) | gm-v01.5 | Pending |
| TERM-04 | Phase D (Terminal) | gm-v01.5 | Pending |
| TERM-05 | Phase D (Terminal) | gm-v01.5 | Pending |
| WS-01 | Phase E (Workspace) | gm-v01.5 | Pending |
| WS-02 | Phase E (Workspace) | gm-v01.5 | Pending |
| WS-03 | Phase E (Workspace) | gm-v01.5 | Pending |
| WS-04 | Phase E (Workspace) | gm-v01.5 | Pending |
| CARDS-01 | Phase F (Right Cards) | gm-v01.5 | Pending |
| CARDS-02 | Phase F (Right Cards) | gm-v01.5 | Pending |
| CARDS-03 | Phase F (Right Cards) | gm-v01.5 | Pending |
| CARDS-04 | Phase F (Right Cards) | gm-v01.5 | Pending |
| CARDS-05 | Phase F (Right Cards) | gm-v01.5 | Pending |
| CARDS-06 | Phase F (Right Cards) | gm-v01.5 | Pending |
| CARDS-07 | Phase F (Right Cards) | gm-v01.5 | Pending |
| BLANK-01 | Phase G (Blank Dialog) | gm-v01.6 | Pending |
| BLANK-02 | Phase G (Blank Dialog) | gm-v01.6 | Pending |
| BLANK-03 | Phase G (Blank Dialog) | gm-v01.6 | Pending |

**Coverage:** 33 / 33 requirements mapped (100%).

## Constraint Carry-Forward (Future-Proofing)

The following containerized-sessions constraints inform v1 interface design but do not generate v1 requirements:

- C-1 (`Pane`→`Session` rename), C-9 (workspace volume model), C-10 (lifecycle mapping), C-18 (k8s future-proofing audit), C-19 (observability event shape) — already absorbed into CORE-02 / CORE-04 via opaque `sessionID` and `SessionEvent` schema.
- C-2..C-8, C-11..C-17, C-20..C-21 — deferred to the containerized-sessions epic (`gm-root.15`).
