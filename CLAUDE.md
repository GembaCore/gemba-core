# Project Instructions for AI Agents

<!-- gastown-beads:start -->
## Beads & Dolt — Gastown Conventions (All Agents)

This rig uses the **gastown beads** workflow on a centralized Dolt server. Follow these rules every time you create or query beads, or diagnose Dolt issues.

### Title schema (mandatory prefix by issue type)

- `Decision: <statement>` — top-level architectural decision (root of a tree)
- `Milestone: <title>` — milestone under a decision
- `Epic: <title>` — epic under a milestone
- `Story: <title>` — story under an epic; description usually in `As a …, I can …` + `AC:` form

### ID format

Dotted hierarchy assigned by `bd` from the rig prefix + 4-char root suffix:

```
gm-o9t8           ← root Decision
gm-o9t8.1         ← Milestone
gm-o9t8.1.2       ← Epic
gm-o9t8.1.2.3     ← Story
```

### Labels are cumulative

`bd` does **not** auto-inherit parent labels. On every `bd create` you must pass:

1. The initiative tag (e.g. `gemba-remote`, `3signals`)
2. All parent beads' labels (re-list them explicitly)
3. The issue's own type label (`decision` / `milestone` / `epic` / `story`)
4. Topic refinement tags (e.g. `cli`, `server`, `dolt`, `agent`, `oss`, `proprietary`)

Common strategic tags worth carrying on every bead in a line: `architecture`, `saas`, `multi-tenant`, `commercial`.

### Querying

```bash
bd query 'labels CONTAINS "<initiative>"'                              # everything in the initiative
bd query 'labels CONTAINS "<initiative>" AND labels CONTAINS "oss"'    # OSS surface only
bd ready --parent <milestone-id>                                       # ready stories under a milestone
```

### Data plane — use `gt dolt`, not raw Dolt or `bd dolt push`

There is **one** centralized `dolt-sql-server` on **:3307** in `~/gt/.dolt-data/` serving every rig database (gemba, hq, gastown, ai_intelligence_system, …). Per-rig `.beads/embeddeddolt/` paths are legacy.

Authoritative commands:

| Task | Command |
|---|---|
| Show centralized server status | `gt dolt status` |
| List rig databases | `gt dolt list` |
| Push to DoltHub remotes (canonical) | `gt dolt sync` |
| Pull from remotes | `gt dolt pull` |
| Clear read-only state | `gt dolt recover` |
| Align per-rig metadata.json | `gt dolt fix-metadata` |
| Surgical history compaction | `gt dolt rebase` |
| Drop history to single commit (NUCLEAR) | `gt dolt flatten` |
| Server logs | `gt dolt logs` |
| Goroutine dump for hangs | `gt dolt dump` |

**Do not** reach for `bd dolt push`, raw `dolt remote`, or `dolt push` to push rig data. If you see `bd dolt push` warnings, they're almost always coming from the legacy embedded-dolt sidecar; investigate before acting.

### Diagnostic-first protocol

Before restarting Dolt or rewriting state:

1. `gt dolt dump` — capture goroutine stacks (safe, does not kill the process).
2. `gt dolt status` + `gt dolt logs` → save output to `/tmp/dolt-<symptom>-$(date +%s).log`.
3. `gt escalate -s HIGH "Dolt: <symptom>"` (or `CRITICAL` for total outage).

**Never** `rm -rf ~/.dolt-data` or `~/gt/.dolt-data`. **Never** skip step 1 — blind restarts destroy the evidence Dolt hangs require to debug.

### Test-database hygiene

Orphan databases (`testdb_*`, `beads_t*`, `beads_pt*`, `doctest_*`) accumulate on the production server. Run `gt dolt cleanup` if you see them; never `rm` directly.
<!-- gastown-beads:end -->
