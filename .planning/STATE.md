# STATE — Gemba

> Project memory. Updated by gsd workflows. Last update: 2026-05-21.

## Project Reference

- **Project name:** Gemba
- **Core value:** Single-binary Go HTTP server + embedded React/Vite SPA orchestrating AI coding sessions via pluggable runtime adapters; current focus is a desktop-style session workspace over native tmux.
- **Current milestone:** gemba-lite v1 — workable native dispatch with session workspace UX.
- **Milestone bead:** `gm-v01.1`
- **Decision bead (root):** `gm-v01`
- **Success metric:** Operator opens `/sessions`, toggles Workspace, dispatches a beads-driven OR blank session into a worktree, types into the live terminal (round-trip `SendInput` → `tmux send-keys`), and watches right-pane status update — all in-browser. v1 ships behind `?ws=1` flag; default-flip in follow-up PR.

## Current Position

- **Phase:** Phase A — Core SessionIO Interface (not started)
- **Plan:** none yet (next step: `/gsd-plan-phase A`)
- **Status:** Roadmap created; planning not started.
- **Progress bar:** `[          ]` 0/7 phases complete.

## Performance Metrics

- Phases planned: 7
- Phases complete: 0
- v1 requirements mapped: 33 / 33 (100%)
- Bead epics seeded: 5 under `gm-v01.1` (gm-v01.2 → gm-v01.6)

## Accumulated Context

### Decisions (LOCKED — see PROJECT.md `<decisions>`)

1. GSD owns plan + executor; Beads owns tracker. No bidirectional sync daemon.
2. Bridge is a naming convention; every `PLAN.md` task carries `bead: <id>`.
3. gsd-executor lifecycle per task: claim → note (per atomic commit) → close.
4. Mapping: project root → Decision; ROADMAP entry → Milestone; phase → Epic; PLAN task → Story.
5. Reviews / verification logs are gsd artifacts, NOT bead rows.
6. Per-task atomic commits are bead `note` entries, NOT separate beads.
7. Cumulative labels mandatory on every bead.

### Architectural Constraints Carried Forward

- **gemba-lite (precedence 0):** G-1..G-10 from `docs/design/gemba-lite.md`. Mapped to v1 requirements.
- **containerized-sessions (precedence 1):** C-1..C-21 from `docs/design/containerized-sessions.md`. Out of scope for v1 but inform interface design (opaque `sessionID`, `SessionEvent` schema, k8s future-proofing audit).

### Todos / Next Actions

- [ ] Run `/gsd-plan-phase A` to produce `PLAN.md` for Phase A.
- [ ] Before plan execution starts, ensure each PLAN.md task references its corresponding bead story (create stories under `gm-v01.2` as needed).
- [ ] Track `?ws=1` flag-flip as a Phase G deliverable; do NOT flip earlier.

### Blockers

None.

### Open Questions

None at roadmap time. Per-phase plans will surface implementation-level questions.

## Session Continuity

- **Last action:** Roadmap + requirements + project memory created from synthesized intel (`SYNTHESIS.md`, `decisions.md`, `constraints.md`).
- **Source intel:** `.planning/intel/SYNTHESIS.md` (entry point).
- **Bead tree:** Already seeded — `gm-v01` (Decision) → `gm-v01.1` (Milestone) → `gm-v01.2`..`gm-v01.6` (Epics). See `docs/process/gsd-beads-bridge.md` §"Reference: gemba-lite tree (seeded)".
- **Next session:** Begin `/gsd-plan-phase A`.
