# Autonomous dispatch — quickstart

> 10 minutes from zero to a server that picks up ready beads and
> dispatches them to sticky pooled sessions without a human drag.
> Two flows below: **Native** (local agent panes) and **Gas Town**
> (rig + polecat dispatch via `gt`).
>
> Deeper material: [Pool sizing and MaxParallel](../deployment/pool-sizing.md)
> and the [session-pool design](../design/session-pool.md).

## Concepts in 30 seconds

A **pool** is a bounded set of long-lived agent sessions running one
**persona**. Pool members carry warm context across beads — a recycle
clears the context window in place rather than tearing the session
down. The auto-dispatch daemon picks ready beads, scores them via
two-axis selection (conflict graph + concept affinity), and slings
them onto idle pool members up to the configured size.

Phase 0 (the default): no `pool.toml`, no daemons, every dispatch
spawns fresh. Behavior identical to `gemba serve` with no extra
config. **Pools are opt-in.**

Manual drag also auto-spawns: dropping an `epic`, `task`, `bug`, or
`feature` bead into the **In Progress** column on the Board fires a
session for that bead. `decision` and `chore` kinds change state but
do not spawn (decisions are documentation-shaped; chores are too
small to warrant a session). The kind set is enforced in
`web/src/components/board/dragToRestage.ts` (`AUTOSTART_KINDS`) and
deliberately overlaps the autodispatch routing defaults so the daemon
and a human drag agree on what counts as "do this now."

Wrapper drag cascades: dropping an `epic` or `milestone` into **In
Progress** starts a cascade rather than assigning the wrapper to an
agent. Gemba labels the wrapper `gemba:cascade-active`, stages backlog
descendants, and dispatches currently unblocked runnable leaf work up to
the pool limit for the selected agent type. As children move, ancestor
state is reconciled back onto the wrapper: in progress when any child
is active, done when all descendants are done.

Codex pools are intentionally one-shot. The `gemba-codex-driver` runs
`codex exec`, emits lifecycle frames, closes the bead on success, and
then exits. Auto-dispatch therefore cold-starts Codex sessions when no
active Codex work exists instead of trying to reuse a stale idle pane.

The **scope axis** above persona is named differently per adaptor:

| Adaptor | Scope axis | Editor label |
|---------|-----------|--------------|
| Native  | implicit (singleton — your local server) | hidden |
| Gas Town | `rig` (gt's existing concept) | "Rig" |

Internally the data model uses `scope` for both; the SPA editor at
`/settings/pools` adapts its labels.

---

## Native quickstart

You'll need:

- `gemba` built (`make build` produces `./bin/gemba`).
- A ratified gemba project (`/new` flow). As of `gm-root.24`, ratify
  auto-seeds `.gemba/agents.toml` with a default `claude` agent and
  copies the three bundled personas into `.gemba/personas/`. If
  you're on a project ratified before that landed, those files are
  yours to write — examples below.

### 1. Pick a persona

The three bundled personas (auto-seeded into new projects):

```bash
ls .gemba/personas/
# deployment-engineer.toml  documentarian.toml  project-manager.toml
```

The persona id is the filename stem (no `.toml`). The rest of this
section uses `deployment-engineer` as the worked example —
substitute the persona id that matches the work shape you're
dispatching.

### 2. Write `pool.toml`

The minimal opt-in config:

```toml
# pool.toml — minimal native pool
[pool]
default_persona = "deployment-engineer"
default_floor = 0.5      # auto-dispatch score floor; below = wait
reserved_for_manual = 1  # always keep one pane slot for human drag

[pool.routing]
# Bead kind → persona. Beads not in this map fall through to default_persona.
epic = "deployment-engineer"
task = "deployment-engineer"
bug  = "deployment-engineer"
decision = "project-manager"

[pool.local.deployment-engineer]
# `local` is the implicit scope on native; the editor hides this
# axis but the data model uses it. Per-pool overrides below
# cascade against the [pool] defaults above.
size = 2                  # 2 long-lived members
agent_type = "claude"
floor = 0.4               # this pool dispatches a bit more eagerly
recycle_after_beads = 5   # safety belt; recycle every 5 beads
idle_ceiling_minutes = 30 # reaper drains members idle longer than this
```

### 3. Make sure `MaxParallel` accommodates the pool

The pool size is **clamped at config load** by:

```
effective_size = min(declared_size, MaxParallel - reserved_for_manual)
```

`MaxParallel` lives in `.gemba/agents.toml`:

```toml
[[agent]]
name = "claude"
intra_parallel = true
max_parallel = 4   # <- must be at least size + reserved_for_manual
```

For our example: `size=2 + reserved_for_manual=1 = 3` → `max_parallel`
must be ≥ 3. If the clamp activates you'll see a `WARN` line at
startup.

### 4. Start the server

```bash
./bin/gemba serve \
  --beads-dir .beads \
  --orchestration=native \
  --pool-config ./pool.toml
```

Expected banner additions:

```
pool: local/deployment-engineer  size=2 (effective)  agent=claude  floor=0.40
```

### 5. Verify

Two ways:

**a. Drop a ready epic into beads** with no assignee. Wait one tick
(default 10 s). The daemon picks it up, dispatches to an idle pool
member. Watch `/sessions` in the SPA.

**b. SPA observability:**

```bash
curl http://127.0.0.1:7666/api/pools | jq
```

Expected shape:

```json
{
  "pools": [
    {
      "scope": "local",
      "persona": "deployment-engineer",
      "size_target_declared": 2,
      "size_target_effective": 2,
      "size_actual": 2,
      "idle": 1,
      "working": 1,
      ...
    }
  ]
}
```

### 6. Edit via the UI (optional)

Open `/settings/pools` in the SPA. The native flow shows server
defaults, routing, pools (no scope axis), and a live TOML preview.
Save writes back to the path the server was launched with; a
**restart-to-apply** banner appears under Save. Hot-reload is a
follow-up (`gm-s47n.x`).

---

## Gas Town quickstart

You'll need:

- `gt` CLI on `PATH` (`brew install GembaCore/tap/gastown` or your
  org's install path).
- `gemba` built.
- At least one Gas Town rig configured (next step bootstraps your
  first one if you don't have any).

### 1. Bootstrap your first rig (skip if `gt rig list` is non-empty)

If `gt rig list --json | jq length` returns `0`, you have no rigs yet.
Create one:

```bash
gt rig create gemba           # the rig name; pick anything
```

This stands up the rig's Dolt-backed beads database and registers
the role topology (mayor / deacon / witness / refinery / crew /
polecat pools). Confirm with `gt rig list`.

You can also do this from the SPA's `/settings/pools` editor — the
"+ New rig" button opens a confirmation modal that runs the same
`gt rig create` command (gm-s47n.17).

### 2. List your rigs and personas

```bash
gt rig list --json | jq -r '.[].name'
# gemba
# lume
# sb

gt agents --json | jq -r '.[] | "\(.rig)/\(.role)/\(.name)"' | sort -u
# gemba/crew/mike
# gemba/witness/...
# lume/crew/...
```

The persona id in `pool.toml` matches gt's role/agent identifier.
For this example we'll use `engineer` in the `gemba` rig.

### 3. Write `pool.toml`

Notice the scope is the rig name (real, multi-rig case):

```toml
# pool.toml — Gas Town pool
[pool]
default_persona = "engineer"
default_floor = 0.5
reserved_for_manual = 1

[pool.routing]
epic = "pm"           # epics → PM persona
task = "engineer"
bug  = "engineer"

[pool.gemba.engineer]
size = 3
agent_type = "claude"
floor = 0.4
recycle_after_beads = 5
idle_ceiling_minutes = 30

[pool.gemba.pm]
size = 1              # one PM persona slot in the gemba rig
agent_type = "claude"
floor = 0.5

[pool.lume.engineer]
size = 1              # one engineer slot in the lume rig
agent_type = "claude"
```

### 4. Start the server

```bash
./bin/gemba serve \
  --beads-dir .beads \
  --orchestration=gastown \
  --pool-config ./pool.toml
```

Expected banner:

```
pool: gemba/engineer  size=3 (effective)  agent=claude  floor=0.40
pool: gemba/pm        size=1 (effective)  agent=claude  floor=0.50
pool: lume/engineer   size=1 (effective)  agent=claude  floor=0.50
```

### 5. Verify

Same `/api/pools` curl as native, plus:

```bash
gt convoy list --json | jq
# Active gt convoys (one per dispatched bead)

gt polecat list --all --json | jq
# All polecats; pool members appear here as gt polecats
```

### 6. Edit via the UI (optional)

`/settings/pools` in the SPA. The Gas Town flow:

- Imports rigs + personas + scheduler config from `gt` (read-only
  panel at the top).
- Server defaults + routing + per-pool sections all stored in
  `pool.toml` (gemba's daemon config).
- "+ New rig" / "+ New polecat" / "Edit gt scheduler" buttons
  shell to `gt` CLI when wired (the buttons land in
  `gm-s47n.17`; until then, run those `gt` commands in your
  shell and click **Refresh from gt**).

### 7. Mind the agent skill contract (`bead-done` emit)

The agent must emit `gemba-state bead-done` **after `gt done` (or
the equivalent end-of-bead skill) reports success**. This is the
"I'm going idle, not exiting" signal that lets the session
transition to `SessionReady` (pool member retention) instead of
being torn down. Without it the lifecycle plumbing exists but
never fires and every bead cold-spawns.

Two paths to wire this:

**a. Native source path** (durable, recommended): the `gt done`
subcommand in the gastown CLI source is being updated (tracked as
`gm-s47n.20`) to emit `gemba-state bead-done --bead <id>` from its
post-success branch. Until that ships, use option (b).

**b. Operator-local slash command** (works today): polecats that
finish their bead via a `/done` slash command can have the slash
command's bash post-step do the emit. Add this to the bottom of
your `.claude/commands/done.md` (which is operator-local —
`.claude/` is gitignored — so paste this into your own copy):

## Mock mode (dry-run)

`gemba serve --orchestration=mock` runs the entire dispatch loop
**without spawning a real claude session**. The Go mock plane
(`internal/adapter/mock/`, gm-root.28) implements the full
`OrchestrationPlaneAdaptor` contract — minted Session lifecycle,
`SessionReady` recycling, conflict-graph arbitration — but per-bead
work is performed by an in-process template library that:

- writes a fixed set of files from a content registry
- shells to `npm install`, `npm run build`, etc. as needed
- closes the bead via `bd close`
- emits `gemba-state bead-done` so the bridge transitions to
  `SessionReady`

When to reach for it:

- **Workflow validation.** Verify your pool config + persona routing +
  bead frontmatter (`template:`/`testid:`/`files:`) before running
  with real LLM calls.
- **Dispatch-loop debugging.** Mock mode is fast and deterministic; if
  the daemon misbehaves under mock, the issue is in the orchestration
  layer, not in the agent runtime.
- **CI-style integration tests.** The temperature-spa acceptance test
  (`gm-root.27`) uses mock mode as its default backend.

**Caveats:**

- Mock-mode does NOT validate that an agent could actually perform
  the bead's DoD. It only writes from a pre-shipped template.
- Beads not matching any template (by frontmatter or title-keyword
  fallback) cause the session to enter `SessionFailed` — there's no
  smart fallback. Add new templates in `internal/adapter/mock/
  templates.go` to extend coverage.
- Operators running mock-mode on a real project should treat any
  closed bead as "the dispatch loop is healthy," not as "the work
  is done."

Setup is the same as native — write `pool.toml`, then:

```bash
./bin/gemba serve --beads-dir .beads --orchestration=mock --pool-config ./pool.toml
```

The mock plane preseeds one idle session per persona discovered in
`<project>/.gemba/personas/` so the daemon dispatches immediately.

```markdown
## Post-success: emit bead-done for pool retention

After `gt done` reports success, emit the `bead-done` token so the
gemba bridge transitions this session to `SessionReady` (pool
retention). Best-effort: silent fall-through outside a gemba native
session.

​```bash
BEAD_ID="$(git rev-parse --abbrev-ref HEAD 2>/dev/null | sed -n 's,^bead/,,p;s,^polecat/[^/]*/,,p')"
if [ -n "$BEAD_ID" ]; then
  gemba-state bead-done --bead "$BEAD_ID" 2>/dev/null || true
else
  gemba-state bead-done 2>/dev/null || true
fi
​```
```

Make sure the slash command's `allowed-tools` frontmatter
includes `Bash(gemba-state:*)` and `Bash(git rev-parse:*)`.

The bridge then runs the §5.4.2 worktree-cleanliness check; on a
clean worktree the session goes idle and the autodispatch daemon
hands it the next bead without a cold spawn.

If the agent invokes `gt done` directly (not via a slash command),
the durable fix lives in the gt CLI source — see `gm-s47n.20`.

---

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| Banner shows pool with `size_target_effective=0` and a clamp WARN | `MaxParallel - reserved_for_manual` is below your declared size. Raise `[[agent]] max_parallel` in `agents.toml` or lower `pool.<scope>.<persona>.size`. |
| Beads sit unrouted (no autodispatch) | `pool.routing` doesn't cover their `kind` and there's no `default_persona`. Add a routing rule or set `default_persona`. |
| Dispatch fires but always cold-spawns | `size = 0` → no pool exists. Or (Gas Town) `gemba-state bead-done` not emitted by the agent skill. |
| `WARN config: rig is now scope; please rename …` | Your `pool.toml` uses the legacy `[pool.<rig>.<persona>]` form. The decoder accepts both for one release; rename to `[pool.<scope>.<persona>]` to silence the warning. |
| `OutcomeBelowFloor` in logs | Selection score is below `default_floor` / `pool.<…>.floor`. Either lower the floor, or wait for higher-affinity beads. |
| Manual drag starves under heavy auto-dispatch | Raise `reserved_for_manual`. The clamp keeps `reserved_for_manual` slots permanently outside the pool. |

## Where next

- [Pool sizing and MaxParallel](../deployment/pool-sizing.md) — full
  cascade rules, observability fields, recycle protocol, all the
  per-pool tunables.
- [Two-axis work planning and dispatch](../design/work-planning.md)
  — the parent design behind the daemon's selection algorithm.
- [Sticky session pool + idle lifecycle](../design/session-pool.md)
  — the lifecycle contract (`SessionReady`, `bead-done`, recycle,
  reaper, cleanliness invariant).
- [Pool config editor](../design/pool-editor.md) — the SPA editor
  spec and adaptor-aware ownership split.
