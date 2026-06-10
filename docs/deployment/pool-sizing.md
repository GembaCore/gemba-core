# Pool sizing and MaxParallel

> Operator guide for the auto-dispatch sticky session pool (gm-s47n.12).
> Authoritative spec: `docs/design/session-pool.md` (especially §3.3,
> §6, §8.1, §10.1).

The auto-dispatch daemon constructs one **pool** per `(rig, persona)`
tuple declared in `pool.toml`. A pool is a bounded set of long-lived
sessions running one persona; members carry continuous context across
beads, so a recycle (the in-place `/clear`) is the only way to drop
that context, not an exit-and-respawn.

This document covers the operator surface: when to enable pools,
how `pool.size` interacts with `MaxParallel`, what the cascade
defaults look like, and how to read `/api/pools` when the live state
disagrees with what you configured.

## When to enable pools (vs Phase 0 fresh-spawn-per-bead)

The default is **Phase 0**: no `pool.toml`, no daemons, every
dispatch spawns a fresh session. Behavior identical to today's main.
Enable a pool when one of these is true:

- The same persona is going to run a stream of beads in the same
  workspace and the cold-spawn cost (~5–10s of claude boot per bead)
  is dominating throughput.
- The persona benefits from accumulating warm context across beads
  (concept overlap, file familiarity, last-N bead memory).
- You have stable rigs where the operator + agent have a working
  rapport that survives bead-to-bead transitions.

Do NOT enable a pool when:

- The persona is rarely invoked (the pool member sits idle and the
  reaper's `idle_ceiling_minutes` reaps it before the next bead).
- The persona's beads are highly heterogeneous (concept_drift will
  trigger recycles often enough to defeat the warm-context point).
- The host is tight on `MaxParallel` and a manual operator drag is
  more important than auto-dispatch — a saturated pool will starve
  the manual-drag flow unless `reserved_for_manual` is tuned (see
  next section).

## How `pool.size` interacts with `MaxParallel`

`MaxParallel` lives in `.gemba/agents.toml`:

```toml
[[agent]]
name = "claude"
intra_parallel = true
max_parallel = 4
```

This is the **per-host concurrent-pane cap**. Pool sizing is
best-effort against this cap, not a parallel allocation:

```
effective_size = min(declared_size, MaxParallel - reserved_for_manual)
```

`reserved_for_manual` defaults to **1** — at least one pane slot is
held back from the pool so a human operator's manual drag is never
starved by a saturated pool. Tunable via `[pool] reserved_for_manual = N`.

Worked example, `MaxParallel = 4`:

| `[pool] reserved_for_manual` | `pool.gemba.engineer-claude.size` | `effective_size` | Clamp activated? |
|---|---|---|---|
| 1 (default) | 2 | 2 | No |
| 1 (default) | 3 | 3 | No |
| 1 (default) | 4 | 3 | **Yes** (clamped from 4 → 3) |
| 1 (default) | 5 | 3 | **Yes** (clamped from 5 → 3) |
| 0 | 4 | 4 | No (manual drag may starve) |
| 2 | 3 | 2 | **Yes** (clamped from 3 → 2) |

When the clamp activates, gemba logs a `WARN` at startup naming
declared, cap, and effective sizes. The startup banner also surfaces
the effective size next to the clamp note:

```
▶ pool[gemba/engineer-claude]: size=3 (clamped from 5 by MaxParallel) floor=0.50 recycle_after_beads=5 idle_ceiling_min=30
```

The SPA's pool state endpoint (`GET /api/pools`) surfaces both
`size_target_declared` and `size_target_effective` so the clamp is
observable post-startup without grepping the slog stream.

> **"I configured size=5 but only see 3 pool members."** Check the
> `MaxParallel` clamp first. The startup banner and `WARN` log line
> name the cap; the SPA's `/api/pools` response shows declared vs
> effective. If the clamp is firing, raise `[[agent]] max_parallel`
> in `.gemba/agents.toml` or lower `pool.size`.

## Per-pool vs per-rig defaults — the cascade

Most operators set the rig-level defaults once and override only
the pools that need tuning:

```toml
[pool]
default_size = 0           # opt-in; explicit pool blocks override
default_persona = ""       # routing fallback (see next section)
default_floor = 0.5        # auto-dispatch score floor
reserved_for_manual = 1    # pool slots held back for manual drag

[pool.gemba.engineer-claude]
size = 3
floor = 0.4                # this pool overrides the rig default
recycle_after_beads = 5    # safety belt; 0 disables
idle_ceiling_minutes = 30  # reaper threshold

[pool.gemba.pm-claude]
size = 1
# floor / recycle_after_beads / idle_ceiling_minutes inherit defaults
```

Cascade, lowest precedence to highest:

1. **Scope-level default**: `[pool] default_*` keys
2. **Per-pool override**: `[pool.<scope>.<persona>]` keys
   (legacy `[pool.<rig>.<persona>]` form still accepted with a
   deprecation `WARN` for one release; see gm-s47n.16 §2 — `scope`
   is the data-model name, `rig` was its original gt-specific
   spelling. New writes should use `scope`.)

A daemon is constructed per `(scope, persona)` with `effective_size > 0`.
Pools with `size = 0` (default) construct no daemon — the
`engineer-codex` persona, for example, can sit unconfigured and a
manual drag still works fine against it.

## Three-layer persona routing cascade

A bead's persona is resolved at dispatch time by walking three
layers, **highest precedence first**:

1. **Bead extras `persona` field** — explicit override on the bead
   (`bd update gm-foo --custom persona=pm-claude`).
2. **`[pool.routing.<kind>]` map** — per-bead-kind default routing.
3. **`[pool] default_persona`** — server-level fallback.

Example:

```toml
[pool]
default_persona = "engineer-claude"

[pool.routing]
epic = "pm-claude"
bug = "engineer-claude"
decision = "pm-claude"
```

A bead of kind `epic` routes to `pm-claude` unless its extras carry
an explicit `persona = "..."` override. A bead of kind `feature`
falls through to the rig default `engineer-claude`.

If no layer resolves a persona, the daemon **refuses** to autodispatch
the bead — it sits in the ready set until a manual drag picks it up.
This is logged as `OutcomeNoPersona` so operators can find unrouted
beads at a glance.

> **"Beads sit unrouted in the ready set."** Check that either
> `[pool] default_persona` is set, or that `[pool.routing.<kind>]`
> covers the bead's kind, or that the bead itself carries a `persona`
> extra. The daemon is intentionally conservative here — better to
> wait for an operator than to fan beads to the wrong persona.

## Auto-dispatch floor

Spec §8.1: the floor is the minimum Layer 5 Selection score below
which the daemon refuses to dispatch. Default `0.5`; tuned per-pool:

```toml
[pool]
default_floor = 0.5

[pool.gemba.engineer-claude]
floor = 0.4   # this pool dispatches more aggressively
[pool.gemba.production-claude]
floor = 0.7   # this pool is conservative
```

When to lower the floor:

- The pool is for hacking / exploration; you want it dispatching
  any reasonable pick rather than sitting idle.
- The ready set is small and high-affinity matches are rare; a 0.5
  floor would block every dispatch.

When to raise the floor:

- The pool is for production beads; you want only high-confidence
  picks to fire automatically.
- You're seeing the daemon dispatch picks that an operator would
  override — raise the floor until the noise stops.

Below-floor blocks surface as `OutcomeBelowFloor` in the daemon's
action log; the bead is left for manual drag or a future tick when
a higher-affinity session asks for it.

## End-of-bead cleanliness contract (§5.4)

Pool members must end every bead with a clean worktree. The agent's
`gt done` (or equivalent) skill MUST:

1. `git status --porcelain` — if dirty, commit with a deterministic
   message: `chore(<bead-id>): auto-commit before bead-done`. Auto-
   commits are *suspicious* and grep-able; review before treating
   as intent.
2. `git push origin <branch>` — work is not complete until push
   succeeds.
3. Emit `gemba-state bead-done` only after the push succeeds.

The bridge verifies cleanliness before transitioning the session to
`SessionReady`. If the agent emits `bead-done` with a dirty
worktree, an `escalation.bead_done_with_dirty_worktree` is raised
and the session stays in `SessionWorking` for operator triage.

If recycle later fires on a worktree that drifted dirty since
`bead-done` (e.g. operator hand-edited a file in the idle window),
the recycle is **refused** and converted to End — the pool will
spawn a fresh slot on the next dispatch tick. **The recycle path
never `git reset --hard`s.** Operators who lose work to a silent
reset lose trust in the pool, so the cleanliness invariant is pulled
upstream into the agent skill, not papered over downstream.

## Troubleshooting

| Symptom | Likely cause | Action |
|---|---|---|
| "size=5 but only 3 pool members" | `MaxParallel` clamp | Raise `[[agent]] max_parallel` or lower `pool.size`. Check startup banner + `/api/pools.size_target_declared`. |
| "beads sit unrouted" | Persona routing missing | Set `[pool] default_persona`, add a `[pool.routing.<kind>]` row, or set bead extras `persona = "..."`. |
| "daemon dispatches noisy picks" | Floor too low | Raise `floor` per-pool until the noise stops. |
| "daemon never dispatches" | Floor too high, OR no `bead-done` emit, OR no resolved persona | Check `OutcomeBelowFloor` / `OutcomeNoPersona` in the daemon log. Try lowering floor or fixing routing. |
| "session keeps recycling" | Health thresholds tripping (context_pressure or concept_drift) | Tune `recycle_after_beads` to bound profile staleness; check session profile for runaway concept drift. |
| "pool member never spawns" | `[pool.<rig>.<persona>] size = 0` (default) | Set `size = N` to opt the pool in; daemon spawns lazily on first dispatch. |
| "pool member reaped overnight" | `idle_ceiling_minutes` reached | Raise `idle_ceiling_minutes` (default 30) for low-traffic rigs; production rigs with cheap pane resources can go to several hours. |

## See also

- `docs/design/session-pool.md` — the authoritative design spec
- `docs/design/work-planning.md` — parent two-axis dispatch design
- `internal/config/pool.go` — TOML schema source of truth
- `internal/planner/autodispatch/daemon.go` — daemon implementation
