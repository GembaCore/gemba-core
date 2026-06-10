# Project: Gemba

## Core Value

Gemba is a single-binary Go HTTP server (`cmd/gemba-server`) that embeds a React/Vite SPA and orchestrates AI coding sessions through pluggable runtime adapters (tmux today; Docker, k8s, microVM later). The current milestone — **gemba-lite v1** — delivers a workable native (tmux-backed) dispatch release whose primary UX is a desktop-style **session workspace**: one three-pane browser view in which an operator dispatches either a beads-driven OR a manual blank session into a worktree, types into the live terminal, and watches assignment / git / beads status update in real time, all without leaving the browser.

## Current Milestone

**gemba-lite v1** — workable native dispatch with session workspace UX.

- Root Decision bead: `gm-v01`
- Milestone bead: `gm-v01.1`
- Default-off behind `?ws=1` flag during v1; flag-flip in a follow-up PR after slice G lands.

## Developer-Facing Success Metric

> An operator opens `/sessions`, toggles **Workspace** mode, dispatches either a beads-driven OR a manual blank session into a worktree, types into the live terminal via the browser (round-trip via `SendInput` → `tmux send-keys`), and sees the right-pane status update (assignment, git status, beads picked up) — all without leaving the browser. v1 ships behind a flag; default-flip in a follow-up PR.

## Target Runtime

- **Server:** Go HTTP server (`cmd/gemba-server`).
- **Frontend:** React/Vite SPA in `web/`, embedded into the Go binary at build time.
- **Distribution:** Single static binary via `Makefile` + `goreleaser`.
- **Tests:** `go test ./...`, `pnpm -F web test`, Playwright e2e.

## Decisions (LOCKED — sourced from `docs/process/gsd-beads-bridge.md`)

<decisions>

### D1 — Ownership split (LOCKED)
GSD owns the plan and the autonomous executor. Beads owns the durable, queryable tracker. **No bidirectional sync daemon.**

### D2 — Bridge is a naming convention (LOCKED)
GSD↔Beads integration is a thin naming convention applied at execute time. No background process, no schema mirroring.

### D3 — `bead:` field on every PLAN.md task (LOCKED)
Every task row in `PLAN.md` carries a `bead: <id>` reference (e.g. `bead: gm-v01.3.4`).

### D4 — Executor lifecycle around every task (LOCKED)
gsd-executor (or human stepping in) runs three calls per task:
- claim: `bd update <id> --claim` before starting
- note: `bd note <id> "<commit-sha> <subject>"` after each atomic commit
- close: `bd close <id>` when acceptance passes

### D5 — Concept-to-bead mapping (LOCKED, immutable)

| GSD concept            | Beads concept            |
|------------------------|--------------------------|
| Project root           | Decision (root bead)     |
| `ROADMAP.md` entry     | Milestone bead           |
| Phase (gsd phase dir)  | Epic bead                |
| Task in `PLAN.md`      | Story bead               |
| Cross-AI review        | (no bead — gsd artifact) |
| ADR / SPEC             | `note` on relevant bead  |

### D6 — Reviews and verification logs are gsd artifacts (LOCKED)
gsd verification logs, cross-AI review markdown, and plan-checker reports live as files under the phase dir; **not** as beads.

### D7 — Per-task atomic commits are bead `note` entries (LOCKED)
A single story bead accumulates one note per commit during execution. Atomic commits are **not** separate beads.

### D8 — Cumulative label discipline (LOCKED)
Beads does not auto-inherit. Every bead must carry:
- Initiative tag (e.g. `gemba-lite`)
- All parent strategic tags re-listed explicitly (`architecture`, `native`, `sessions`, `commercial`, …)
- Layer tag (`core` | `server` | `spa` | `tmux` | `docker` | `k8s`)
- Type tag (`decision` | `milestone` | `epic` | `story`)

</decisions>

## Out of Scope (v1)

- No new top-level nav surface. Workspace lives under `/sessions`.
- No new auth model. Streaming + input endpoints reuse `/api/sessions` middleware.
- No multi-cursor / collaborative editing. Single operator per browser session; tmux serializes concurrent input.
- No webhooks / external triggers beyond what beads provides today.
- Containerized backend (Docker/k8s/microVM) is **future scope**, called out for interface compatibility only (`docs/design/containerized-sessions.md`).

## Constraints (carried from specs)

- Gemba-lite spec (G-1..G-10): `docs/design/gemba-lite.md` (precedence 0).
- Containerized-sessions spec (C-1..C-21): `docs/design/containerized-sessions.md` (precedence 1, future-proofing only).

See `REQUIREMENTS.md` for the requirements derived from these constraints.
