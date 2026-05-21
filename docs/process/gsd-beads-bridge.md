# GSD ↔ Beads bridge

GSD owns **the plan and the autonomous executor**. Beads owns **the
durable, queryable tracker**. Both stay loosely coupled via a naming
convention and a thin convention applied at execute time. No
bidirectional sync daemon.

## Mapping

| GSD concept          | Beads concept            | Notes |
|----------------------|--------------------------|-------|
| Project root         | Decision (root bead)     | One per strategic initiative (e.g. `gm-v01` for gemba-lite). |
| `ROADMAP.md` entry   | Milestone bead           | Child of the project Decision. |
| Phase (gsd phase dir)| Epic bead                | Child of the Milestone. One phase = one epic. |
| Task in `PLAN.md`    | Story bead               | Child of the Epic. Atomic unit gsd-executor commits. |
| Cross-AI review      | (no bead — gsd artifact) | Reviews live as gsd artifacts, not tracker rows. |
| ADR / SPEC           | `note`/`comment` on bead | Attach to the relevant Decision/Milestone bead. |

## Convention: every gsd task carries a bead reference

In `PLAN.md`, every task row includes a `bead:` field:

```markdown
### Task 1.2 — Wire `Streamable` interface to native tmux backend

- bead: gm-v01.3.4
- files: internal/adapter/native/backend/tmux.go, ...
- acceptance: tmux backend satisfies Streamable; unit test covers …
```

The gsd-executor (and the human, when stepping in) reads `bead:` and
runs three calls around the work:

1. **Claim** — `bd update <id> --claim` before starting.
2. **Note** — `bd note <id> "<commit-sha> <subject>"` after each atomic commit.
3. **Close** — `bd close <id>` when acceptance passes.

That's the whole bridge.

## Labels carried on every bead

Cumulative — beads doesn't inherit. Every story under gemba-lite
must carry:

- Initiative: `gemba-lite`
- Project-wide strategic tags as relevant: `architecture`, `native`,
  `sessions`, `commercial`
- Layer tag: `core` | `server` | `spa` | `tmux` | `docker` | `k8s`
- Type tag: `decision` | `milestone` | `epic` | `story`

This is what makes `bd query 'labels CONTAINS "gemba-lite" AND
labels CONTAINS "spa"'` return what you'd expect.

## Bootstrap workflow

1. **In repo**: `bd init --prefix <prefix> --non-interactive` (one-shot).
2. **Create root Decision**:
   `bd create "Decision: <statement>" --labels <initiative>,decision,...`
3. **Create Milestone** as child: `--parent <decision>`.
4. **Create Epics** under the Milestone, one per planned phase.
5. **GSD setup**: run `/gsd-ingest-docs` if `docs/design/` already
   carries the dispatch plan; else `/gsd-new-project`.
6. **GSD phase plans**: when `gsd-plan-phase` creates `PLAN.md`,
   open each task row and add `bead: <id>` after creating the
   corresponding story bead under the matching Epic.

## What gsd does NOT mirror into beads

- gsd verification logs, cross-AI review markdown, plan-checker reports
  → live as files under the phase dir; not beads rows.
- gsd's per-task atomic commits → captured as bead `note` entries, not
  as separate beads.

## Why this shape

- No daemon to keep in sync; each side's source of truth is unchanged.
- gsd remains usable on projects that don't use beads (just drop the
  `bead:` field).
- beads remains usable for ad-hoc tracking outside any gsd phase (just
  create stories without a `bead:`-referenced PLAN.md row).
- Queries that matter (`bd ready --parent <milestone>`, `bd query
  'labels CONTAINS gemba-lite AND status=in_progress'`) keep working.

## Reference: gemba-lite tree (seeded)

```
gm-v01      Decision: gemba-lite — workable native dispatch release …
└── gm-v01.1   Milestone: gemba-lite v1 — session workspace ship
    ├── gm-v01.2  Epic: Core SessionIO interface + adapter defaults
    │   ├── gm-v01.2.1  Story: Add SessionInput/SessionEvent/SessionInputMode types to core
    │   ├── gm-v01.2.2  Story: Extend OrchestrationPlaneAdaptor with SendInput/ResizeSession/StreamSession
    │   └── gm-v01.2.3  Story: Adapter sweep — embed unsupportedSessionIO in native, docker, applescript, k8s, mcp, testadaptors
    ├── gm-v01.3  Epic: Native tmux SessionIO implementation
    ├── gm-v01.4  Epic: HTTP transport — SSE stream + input + resize endpoints
    ├── gm-v01.5  Epic: SPA — SessionsWorkspace three-pane view
    └── gm-v01.6  Epic: Manual (blank-session) dispatch surface in NewSessionDialog
```
