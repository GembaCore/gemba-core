# Roadmap: Gemba — gemba-lite v1

**Milestone bead:** `gm-v01.1` — gemba-lite v1: session workspace ship
**Granularity:** Standard (7 phases, one per execution slice)
**Coverage:** 33 / 33 v1 requirements mapped (100%)

Phases are aligned 1:1 with the seven execution slices named in `docs/design/gemba-lite.md` §"Execution slices" (A–G). Each phase references its corresponding bead Epic under the `gm-v01.1` milestone.

## Phases

- [ ] **Phase A: Core SessionIO Interface** — Land `SessionInput`/`SessionEvent` types and the three new `OrchestrationPlaneAdaptor` methods with adapter sweep.
- [ ] **Phase B: Native tmux SessionIO** — Implement `Streamable`, `SendInput`, `ResizeSession`, refcounted IO fan-out for the tmux backend.
- [ ] **Phase C: HTTP Transport** — Ship SSE `/stream`, audited `POST /input`, and `GET /status` endpoints.
- [ ] **Phase D: Terminal Pane** — Wire xterm.js to slices A+B+C at `/sessions/{id}/term` for early dogfooding.
- [ ] **Phase E: Workspace Shell** — Three-pane layout, rail, and Table/Workspace toggle on `SessionsPage`, gated behind `?ws=1`.
- [ ] **Phase F: Right-Pane Cards** — Assignment / Worktree / Git / Beads / Escalations cards consuming `/status`.
- [ ] **Phase G: Blank-Session Dialog + Flag Flip** — Extend `NewSessionDialog` with manual flow; end-to-end success-metric verification; flip `?ws=1` default in a follow-up PR.

## Phase Details

### Phase A: Core SessionIO Interface
**Goal**: The codebase exposes a complete, runtime-agnostic session IO contract on `OrchestrationPlaneAdaptor`, with every existing adapter compiling under a default-unsupported helper.
**Depends on**: Nothing (first phase)
**Bead Epic**: `gm-v01.2`
**Requirements**: CORE-01, CORE-02, CORE-03, CORE-04
**Success Criteria** (what must be TRUE):
  1. `core` package exports `SessionInputMode`, `SessionInput`, `SessionEvent`, and the extended `OrchestrationPlaneAdaptor` interface; `go build ./...` is green.
  2. Every existing adapter (native, docker, applescript, k8s, mcp, testadaptors) embeds `unsupportedSessionIO`; calling `SendInput`/`ResizeSession`/`StreamSession` on any of them returns `core.KindUnsupported`.
  3. `sessionID` remains opaque across the interface — no caller in the tree assumes it is a tmux pane id (verified by grep / test).
  4. `go test ./core/... ./internal/adapter/...` passes.
**Plans**: TBD

### Phase B: Native tmux SessionIO
**Goal**: An operator can stream tmux output and inject keystrokes through the new core interface against a real tmux backend, with deterministic resource cleanup.
**Depends on**: Phase A
**Bead Epic**: `gm-v01.3`
**Requirements**: NATIVE-01, NATIVE-02, NATIVE-03, NATIVE-04, NATIVE-05
**Success Criteria** (what must be TRUE):
  1. `Streamable.StreamPane` on tmux backend emits live pane bytes via `pipe-pane`; integration test reads a known echo round-trip end-to-end.
  2. Disconnect storm test (N subscribers attach/detach) leaves zero leaked named-pipe files on the host.
  3. `SendInput` literal mode round-trips quoted multi-line + unicode input; keys mode delivers `Enter`, `C-c`, `Up` correctly; signal mode maps to expected control key.
  4. Refcounted fan-out test: two SSE-style subscribers share one underlying tmux IO channel; channel torn down only when both detach.
**Plans**: TBD

### Phase C: HTTP Transport
**Goal**: The Go HTTP server exposes the three session endpoints (`/stream`, `/input`, `/status`) such that a curl-only operator can read live output, send input, and snapshot state.
**Depends on**: Phase B
**Bead Epic**: `gm-v01.4`
**Requirements**: HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05
**Success Criteria** (what must be TRUE):
  1. `curl -N /api/sessions/{id}/stream` receives a `snapshot` event then incremental `output` and `status` events for a live tmux session.
  2. `curl -X POST /api/sessions/{id}/input` with `{keys:"echo hi\n", mode:"literal"}` causes the tmux session to execute the command (validated via subsequent stream snapshot) and emits an audit event.
  3. `curl /api/sessions/{id}/status` returns JSON with populated `worktree`, `git`, `beads`, and `assignment` fields (or `assignment: null` for manual sessions).
  4. Non-`Streamable` backend (test stub) is served correctly via the 500ms capture-pane polling fallback.
  5. Confirm-nonce middleware rejects `/input` calls missing the nonce header; integration test covers reject + accept paths.
**Plans**: TBD

### Phase D: Terminal Pane
**Goal**: A browser-loaded xterm.js terminal at `/sessions/{id}/term` is a usable interactive terminal against any live tmux session.
**Depends on**: Phase C
**Bead Epic**: `gm-v01.5` (slice D)
**Requirements**: TERM-01, TERM-02, TERM-03, TERM-04, TERM-05
**Success Criteria** (what must be TRUE):
  1. Opening `/sessions/{id}/term` in a browser shows the session's backscroll (initial snapshot) and streams live output without manual refresh.
  2. Typing into the terminal sends keystrokes to the tmux session; pressing Enter / Ctrl-C / arrow keys works identically to a native tmux client.
  3. xterm.js + addon-fit dependency loads lazily — `/sessions` Table view does not include the xterm bundle in its initial chunk.
  4. SSE disconnect triggers an automatic reconnect without page reload.
**Plans**: TBD
**UI hint**: yes

### Phase E: Workspace Shell
**Goal**: An operator can toggle `/sessions` into Workspace mode and see the three-pane layout (rail · terminal · status) with the existing Table mode preserved.
**Depends on**: Phase D
**Bead Epic**: `gm-v01.5` (slice E)
**Requirements**: WS-01, WS-02, WS-03, WS-04
**Success Criteria** (what must be TRUE):
  1. `/sessions?ws=1` renders the three-pane `SessionsWorkspace`; `/sessions` without the flag still renders the unchanged Table view.
  2. Left rail lists live sessions grouped Live / Ended, with status dots and a `+New` action; selecting a session loads it into the middle pane.
  3. Middle pane shows the live terminal (reusing Phase D plumbing) with Terminal · Logs · Diff tab affordances.
  4. Mode toggle in the page header switches between Table and Workspace; URL reflects state (`?ws=1` or `?session=<id>`).
**Plans**: TBD
**UI hint**: yes

### Phase F: Right-Pane Cards
**Goal**: The Workspace right pane surfaces live assignment / worktree / git / beads / escalations information for the selected session, driven by `/status`.
**Depends on**: Phase E
**Bead Epic**: `gm-v01.5` (slice F)
**Requirements**: CARDS-01, CARDS-02, CARDS-03, CARDS-04, CARDS-05, CARDS-06, CARDS-07
**Success Criteria** (what must be TRUE):
  1. With a beads-driven session selected, the Assignment card shows the bead id + title; with a manual session, it shows "Manual session".
  2. Worktree card displays path / branch / ahead-behind counts that match `git status` on the underlying worktree.
  3. Git Status card lists porcelain entries that change in real time as the operator edits files in the terminal.
  4. Beads card lists every claim / close / comment the agent has produced in this session.
  5. Escalations card surfaces at least one escalation event from a contrived test scenario.
**Plans**: TBD
**UI hint**: yes

### Phase G: Blank-Session Dialog + Flag Flip
**Goal**: An operator can dispatch a blank (manual) session from the `NewSessionDialog`, land directly in Workspace mode with a live terminal, and the success metric is verified end-to-end; the `?ws=1` flag is then flipped to default-on.
**Depends on**: Phase F
**Bead Epic**: `gm-v01.6`
**Requirements**: BLANK-01, BLANK-02, BLANK-03
**Success Criteria** (what must be TRUE):
  1. `NewSessionDialog` offers a "Blank session in worktree" option that does not require a bead; submitting it provisions a worktree and a tmux session.
  2. End-to-end: an operator opens `/sessions`, toggles Workspace, dispatches EITHER a beads-driven OR a blank session, types in the terminal, and sees right-pane status (assignment, git, beads) update — verified by Playwright e2e and a manual walk-through.
  3. Follow-up PR flips the default so `/sessions` renders Workspace mode without `?ws=1`; Table view remains accessible via toggle.
  4. `go test ./...`, `pnpm -F web test`, and Playwright e2e all green at the flag-flip commit.
**Plans**: TBD
**UI hint**: yes

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| A. Core SessionIO Interface | 0/0 | Not started | - |
| B. Native tmux SessionIO | 0/0 | Not started | - |
| C. HTTP Transport | 0/0 | Not started | - |
| D. Terminal Pane | 0/0 | Not started | - |
| E. Workspace Shell | 0/0 | Not started | - |
| F. Right-Pane Cards | 0/0 | Not started | - |
| G. Blank-Session Dialog + Flag Flip | 0/0 | Not started | - |
