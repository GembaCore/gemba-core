# Running Gemba against your work items

Gemba is a single-binary Go service that reads a WorkPlane (a work
tracker) and renders a Kanban board in your browser. The only hard
requirement is a data plane; **beads** (`bd`) fulfills that out of
the box, and the work items themselves are bd issues. You can point
Gemba at a beads rig two ways:

- **Mode A — `--beads-dir`**: Gemba shells out to the `bd` CLI for
  every read. Slower but portable; works anywhere you can run `bd`.
- **Mode B — `--dolt-url`**: Gemba opens a direct SQL connection to your
  Dolt server. Faster and no `bd` dependency; reads and writes are
  enabled when the Dolt user has write permission.
- **Beads-only**: add `--beads-only` when you want only Beads viewing
  and management. Gemba does not require a project, GitHub repository,
  native agent setup, or Gas Town orchestration in this mode.

Exactly one of the two must be passed (`gm-98l` enforces this at
startup).

Agent sessions are wired by default through native orchestration
(`--orchestration=native` if you want to spell it explicitly): live tmux
/ iTerm2 / Terminal.app-backed dispatch, no external daemon required.
Pass `--orchestration=none` when you want a read-only / human-driven
board with no session controls. See
[`docs/adaptors/native.md`](../adaptors/native.md) for the full
native-orchestration story. Gas Town / mock / Gas City / LangGraph /
etc. adaptors swap in with `--orchestration=<name>` when you want their
specific scheduling or test semantics.

## Prerequisites

- `gemba` binary on `PATH` — build from source with `make build` and the binary lands at `./bin/gemba`.
- **For Mode A**: [`bd`](https://github.com/steveklabnik/beads) ≥ a version that emits M1-compatible JSON, and a beads rig at `<path>` (a directory containing `.beads/<name>.db` or a rig root whose `.beads/` subdirectory holds the database).
- **For Mode B**: a running Dolt server reachable via `mysql://…`. The default Gas Town Dolt runs on `:3307` and serves every rig's beads database off the same port; see `gt dolt status`.

## Mode A — `--beads-dir` (bd CLI adaptor)

```bash
gemba serve --beads-dir ~/gt/gemba --auth none
# equivalent (the resolver accepts the .beads subdir too):
gemba serve --beads-dir ~/gt/gemba/.beads --auth none
```

`--auth none` is the default and only needs to be spelled out when binding to a non-loopback interface. On a loopback bind (the default `127.0.0.1:7666`) you can omit it.

### What you should see

Startup banner on stderr:

```
▶ gemba <version>  listen=127.0.0.1:7666  auth=none
▶ workplane: beads <ver> (mode=bd-cli dir=/Users/you/gt/gemba)
▶ manifest: 5 states, 3 core + <n> extension edges, feature flags: sprint=no budget=no
```

Then open <http://127.0.0.1:7666/board> — you should land on the board
populated with every bead from the rig. Gemba normalizes adaptor states
into canonical state categories, then presents the execution board as
**Ready**, **In Progress**, and **Done**. The **Ready** column combines
accepted work (`unstarted`, shown as a **Next up** pill) and work staged
by propulsion (`staged`, shown as a **Staged** pill). **Backlog** is
hidden by default because `/refine` is the triage surface; **Canceled**
is available through list/filter views rather than as a default board
lane.

## Mode B — `--dolt-url` (direct Dolt SQL)

```bash
gemba serve --dolt-url 'mysql://root@127.0.0.1:3307/gemba' --auth none
# with credentials:
gemba serve --dolt-url 'mysql://gemba:$DOLT_PW@127.0.0.1:3307/gemba' --auth none
```

The dbname is the snake_case rig name — e.g. `gemba`, `lume_spark_api`, `second_brain`. Pick the database that holds the rig you want to view.

### What you should see

```
▶ gemba <version>  listen=127.0.0.1:7666  auth=none
▶ workplane: beads <ver> (mode=dolt-sql url=mysql://root@127.0.0.1:3307/gemba)
▶ manifest: 5 states, 3 core + <n> extension edges, feature flags: sprint=no budget=no
```

Any password in the URL is redacted from the banner and logs. Mode B is
writable by default: create, edit, delete, state changes, labels, parents,
milestones, and decision beads go through the Dolt SQL adaptor. To force a
no-write posture, add `--beads-read-only`; the Status pane will show
**Beads-read-only** and the API will return `read_only` for every mutation.

## Beads-only mode

Use Beads-only when the Beads database is the product surface: planning,
review, cleanup, wrapper authoring, and graph inspection without
dispatch.

```bash
gemba serve --beads-only --beads-dir ~/gt/gemba --auth none
gemba serve --beads-only --beads-url 'mysql://root@127.0.0.1:3307/gemba' --auth none
gemba serve --beads-only --beads-dir ~/gt/gemba --beads-history .gemba/session-manifest.jsonl
gemba serve --beads-read-only --beads-url 'mysql://reader@127.0.0.1:3307/gemba' --auth none
```

The SPA opens on `/board` in **Cascade** view. Cascade shows the
milestone to epic to bead hierarchy first, which is usually the clearest
way to read an existing Beads database. Switch to **Flat** when you want
milestones, epics, decision beads, and work beads in one dense ordered
list, or open **Graph** to inspect dependencies. Graph's funnel menu can
filter by milestone, epic scope, title/id search, state, or kind. The
**Order** control applies to Flat, Cascade, and the regular board:
modified, created, edited, or ID order. Edited currently uses the
persisted `updated_at` timestamp until the Beads source exposes a
distinct edit timestamp.

### Major Beads-only surfaces

| Surface | What it can do |
| --- | --- |
| **Plan / Board** | Read the project as milestone → epic → bead in Cascade, switch to Flat for a dense inventory, focus by Milestone or Epic, sort by modified/created/edited/ID, and update Beads state by dragging cards when writable. |
| **Refine** | Groom parked **Deferred** work in a dense table before it belongs on the active execution board. This is the best place for cleanup, prioritization, wrapper assignment, and planning edits. |
| **Graph** | Inspect dependencies and relationships. Filter by milestone, epic scope, title/id search, state, or kind; aggregate by item or epic; and highlight cycles or critical path chains. |
| **Details in the RHP** | Open beads, epics, milestones, and decisions for inspection and editing without losing the current Board, Refine, or Graph context. |
| **Status** | Check Beads health, current database/source, remote configuration, writable/read-only mode, and available repair or setup actions. |
| **Beads history** | Review the session manifest as plain-English history for create, edit, delete, and state-change events. |

Available Beads-only actions:

- create, edit, and hard-delete beads;
- create and edit milestone and epic wrapper beads;
- create and edit decision beads;
- select milestone and decision tag pills during authoring;
- drag cards to update Beads state without dispatching sessions;
- use the Deferred state for parked work that belongs in Refine rather
  than the active execution board;
- inspect Beads health in Status, including current database, remote
  configuration, and repair/setup actions where the source supports
  them;
- review the RHP **Beads history** tab, which renders the JSONL session
  manifest in plain English;
- use the Graph view for dependency and relationship inspection,
  including critical-path and cycle highlighting.

### Beads-read-only mode

`--beads-read-only` is a stricter variant of Beads-only. It is for
publishing, review, audits, shared screenshots, and Docker/container
deployments where operators should inspect Beads without changing them.

What stays available:

- Cascade and Flat Beads views;
- Graph dependency inspection with milestone, epic, search, state, and
  kind filters;
- Status, Beads health, current DB/source, and remote setup checks;
- Beads history display for any existing manifest entries;
- deep links to bead, epic, milestone, and decision detail tabs.

What is blocked:

- create, edit, hard-delete, drag-to-state-change, and wrapper/tag
  authoring writes;
- JSONL history append for failed write attempts;
- all session, dispatch, Gemba Walk, review/triage, and orchestration
  surfaces, because read-only implies Beads-only.

The Status pane shows **Beads-read-only** instead of **Beads-only**.
URL sources are not intrinsically read-only; use Dolt credentials or the
Gemba flag to choose that posture explicitly. With a local `--beads-dir`,
adding `--restart` lets Gemba restart bd's Dolt server with `bd --readonly
dolt start` so the lower layer also enforces the policy. Without
`--restart`, Gemba still applies a hard server/adaptor write gate while
ordinary Beads reads continue through the standard `bd` commands.

Sessions, agent status, dispatch controls, Gemba Walk, review/triage,
and orchestration setup are hidden because they require an execution
plane.

## Troubleshooting

**`--beads-dir ...: no .beads/ directory found`** — the path exists but isn't a rig root. Point `--beads-dir` at a directory whose `.beads/` subdirectory contains `<name>.db`, or at the `.beads/` directory itself.

**`--beads-dir and --dolt-url are mutually exclusive`** — pass exactly one. The two modes select different adaptor implementations; Gemba doesn't mix them.

**Board shows a "degraded" banner** — the adaptor is reachable but returned an `adaptor_degraded` error. For Mode A this is usually `bd` missing from `PATH` or the rig being mid-`bd dolt pull`; for Mode B it means the Dolt server answered but the read failed (check `gt dolt status`).

**Board shows "No beads yet"** — the adaptor works but the rig is empty. Run `bd create` or point at a different rig.

**Beads-only Status shows remote setup needed** — the Beads database is
loaded but the remote check did not find a configured or reachable Dolt
remote. Use the Status dropdown to rerun health checks or apply the
available setup/fix action for that source.

**`connection refused` during Mode B startup** — Dolt isn't running on the host/port in your URL. Start it with `gt dolt start`, then retry.

**Non-loopback bind rejected** — `--listen 0.0.0.0` requires `--auth token` or `--auth oidc` (loopback-only is the default safety posture). See `gemba serve --help`.

## Where next

- The full **[Configuration reference](configuration)** covers every
  flag, environment variable, and config file Gemba reads, with the
  order of precedence and the schema for each `.gemba/*` file.
- **[Parallelism in Gemba](parallelism)** is the next thing operators
  reach for once the basics work.
