---
title: "Sticky session pool + idle lifecycle for two-axis dispatch"
decision: gm-s47n.10
---

# Sticky session pool + idle lifecycle for two-axis dispatch

> Status: **ratified** — 2026-04-29 (decision bead: see gm-s47n.10's
> ratification record). All §12 questions resolved; implementation is
> unblocked.
> Owner: gemba mayor
> Scope: the bridge between the two-axis dispatch design (`work-planning.md`,
> gm-s47n) and the native orchestration adaptor. Specifies how sessions become
> long-lived pool members, when they go idle vs recycle vs end, and how the
> already-shipped `planner.autodispatch.Daemon` (gm-s47n.6.3) gets wired
> against this lifecycle.

## §1. Why this exists

`work-planning.md` §6.2 specifies an auto-dispatch loop where:

1. A session "becomes idle" after finishing a bead.
2. The planner reads its `OperationalContext` and the ready set.
3. Layer 5 Selection picks the next bead.
4. If a hard recycle threshold is hit, the session is recycled (`gt handoff`)
   *in place* — same pool slot, fresh context.
5. Otherwise, the bead is slung onto the same session.

This loop assumes **sessions outlive individual beads**. The `planner.
autodispatch.Daemon` shipped under gm-s47n.6.3 reflects that assumption: its
`IdleSessionLister` interface returns sessions ready for new work; its
`SessionDispatcher.Dispatch(sessionID, beadID)` contract takes both an
existing session id and a bead, not just a bead.

The native adaptor today does not honor this assumption. `internal/adapter/
native/end.go:34` does graceful quit + worktree release the moment a bead
ends. There is no `SessionReady` transition; sessions are bead-scoped. So
the daemon's `ListIdle` would always return `[]` and the loop would never
fire — even with the kill switch off and ready beads available.

This document specifies the lifecycle change needed to close that gap, plus
the wiring + config + observability pieces required to ship it without
regressing today's manual-drag flow.

A previous attempt (`gm-e7.8`, closed) tried to bypass the lifecycle gap by
building a parallel "bead-poller" that spawned a fresh session per ready
bead. That collapses both axes of the planner: no warm context (every spawn
is a cold start) and no conflict awareness (selection is bypassed). The
spec explicitly calls this out as the failure mode worth engineering
against (`work-planning.md` §1). This doc is the corrected approach.

## §2. Vocabulary

| Term | Definition |
|------|------------|
| **Pool** | A bounded set of long-lived sessions running one **persona**, scoped to a rig. Members carry continuous context across beads. |
| **Pool key** | The `(rig, persona)` tuple that uniquely identifies a pool. Within a single gemba server instance the rig is implicit, so the pool key reduces to the persona id. The persona's TOML config carries its `agent_type`, system prompt, and skill list — agent_type is therefore implied by the persona, not a separate axis. |
| **Pool member** | One session inside a pool. Has stable `session_id` + `pane_id`. Status transitions per §4. |
| **Pool size** | Configured target count of members for a pool. Daemon maintains the pool at this size when under load; idle members beyond size are reaped (§4.4). |
| **Idle session** | A pool member with `Status = SessionReady` — completed its last bead, pane alive, awaiting dispatch. |
| **Live session** | A pool member with `Status = SessionWorking` (or `SessionPrompting`). |
| **Recycle** | Reset a session's context window without tearing down its pane. Native equivalent of `gt handoff`. Same pool slot, new context, new session profile. |
| **End** | Tear down the session and free its slot. Used for terminal failures and explicit operator-stop. Distinct from "go idle." |
| **Bead boundary signal** | A signal from the agent side (`gemba-state bead-done`) that the current bead is complete and the agent is going idle, not exiting. |
| **Auto-dispatch floor** | The minimum Layer 5 Selection score below which the daemon does nothing. Prevents low-confidence picks. |
| **Cold start** | A pool member that just spawned and hasn't yet completed any bead. Special-cased in selection (no profile yet). |

## §3. The pool model

### §3.1 Pool members are sessions, not panes

A pool member is a `core.Session` row. Its identity is the session id; its
backing resource is the pane (and worktree). When the session is recycled,
a new session id is minted but the pane survives (`gt handoff` semantics).
This means a pool slot's history is a sequence of session ids over the
same pane lifetime, with explicit recycle events between them.

This matters for the session profile (`gm-s47n.2`): the profile is keyed
on `session_id`, so a recycle resets the profile. That's intentional — the
recycle's whole purpose is to drop the old context. The retro pipeline
(`gm-s47n.8`) gets a clean boundary to grade against.

### §3.2 Pool key is `(rig, persona)`

A rig may host multiple pools — one per persona. A persona uniquely
implies its `agent_type`, system prompt, and skill list (see
`internal/persona/` and `.gemba/personas/*.toml`). Two sessions running
*different* personas have distinct system prompts and therefore distinct
warm contexts; treating them as one pool slot would silently invalidate
the affinity model the moment a recycle swapped the persona.

Concrete consequence: a "PM persona" pool is separate from an "engineer
persona" pool, even when both run on `claude`. Beads route to a persona
via a **three-layer cascade**, lowest precedence to highest:

1. `[pool] default_persona = "<id>"` — server-level fallback.
2. `[pool.routing.<kind>] = "<persona-id>"` — per-bead-kind mapping.
3. Bead extras `persona` field — explicit override on the bead itself.

Example:
```toml
[pool]
default_persona = "engineer-claude"
[pool.routing]
epic = "pm-claude"
bug = "engineer-claude"
decision = "pm-claude"
```

If no layer resolves a persona, the daemon refuses to autodispatch
the bead — it waits for manual drag. Logged as `OutcomeNoPersona` so
operators can see which beads are sitting unrouted.

A daemon instance is constructed **per pool**, not globally. Each
daemon's `IdleSessionLister` returns only its pool's idle members and
its `ReadySetReader` returns only beads bound to that pool's persona.
This isolates pools from each other: a slow PM-persona daemon does not
gate engineer-persona dispatches, and the conflict graph stays scoped
(beads for different personas can still surface workspace conflicts via
the live-session lister, which returns sessions across all pools — see
§6.2).

### §3.3 Pool sizing

Two configuration shapes, in priority order:

1. **Per-rig, per-persona explicit:**
   ```toml
   [pool.gemba.pm-claude]
   size = 2
   recycle_after_beads = 5    # safety belt; 0 disables
   idle_ceiling_minutes = 30  # idle beyond this → reap

   [pool.gemba.engineer-claude]
   size = 3

   [pool.gemba.engineer-codex]
   size = 1
   ```

2. **Rig-level default:**
   ```toml
   [pool]
   default_size = 0           # opt-in; explicit pool blocks override
   default_persona = ""       # optional fallback when a bead has no persona
   ```

`size = 0` means "no pool" — today's behavior, every dispatch spawns
fresh. `size = N` means "maintain N pool members for this `(rig,
persona)`" subject to the manifest's `MaxParallel` constraints (see
clamp behavior immediately below).

#### `MaxParallel` clamp

The orchestration manifest declares a per-host `MaxParallel` (the
hard cap on concurrent agent panes the host can support). Pool
sizing is best-effort against this cap, not a parallel allocation:

```
effective_size = min(declared_size, MaxParallel - reserved_for_manual)
```

Where `reserved_for_manual` defaults to 1 — at least one pane slot
is held back from the pool so a human operator's manual drag is not
starved by a saturated pool. Tunable via `[pool] reserved_for_manual = N`.

The clamp runs **once at config load**, not per-dispatch. If the
clamp activates, gemba logs a `WARN` line at startup naming the
declared size, the cap, and the effective size. Operators see the
warning the next time they `gemba serve` — there's no silent
degradation. The SPA's pool state endpoint (§10.1) also surfaces
`size_target_declared` distinct from `size_target_effective` so the
clamp is observable post-startup.

**Documentation requirement:** because the clamp is the single most
common source of "I configured size=5 but only see 3 pool members"
confusion, every place `MaxParallel` and `pool.*.size` appear must
cross-reference each other:

- `internal/config/serve.go` — TOML schema comments must explain
  both knobs together
- `docs/user-guide.md` (or equivalent operator-facing doc) — a
  "Pool sizing and MaxParallel" subsection
- The startup banner — log the effective pool size next to
  `MaxParallel` so it's visible without `grep`

This is part of the gm-s47n.12 DoD, not an aspirational future bead.

The daemon does NOT spawn pool members eagerly. Initial pool growth is
**lazy on first dispatch**: the first time the daemon picks a bead for a
`(rig, agent_type)` that has no idle session, it spawns one. Subsequent
dispatches reuse via `gemba:reuse_pane_id`. This avoids paying the spawn
cost for pools that never see traffic.

Eager initialization (`pool.eager = true`) is a follow-up knob — useful
when an operator wants the rig "warmed up" before opening the SPA, but
not the default because it conflicts with the rig's cold-start gate
(gm-ygwe).

## §4. Idle session lifecycle

### §4.1 Status transitions

Today (bead-scoped):

```
Initializing → Working → (Completed | Failed | Stalled)
```

Proposed (pool member):

```
Initializing → Working → Ready → Working → Ready → … → (Completed | Failed)
                              ↑                ↓
                              ←──── Recycle ───┘
```

`Ready` is the new long-tenured idle state. Transitions:

| From | To | Trigger | Driver |
|------|----|---------|--------|
| `Working` | `Ready` | `bead-done` boundary signal received from agent | native bridge |
| `Ready` | `Working` | `StartSession` called with `gemba:reuse_pane_id` matching this session's pane | native start.go |
| `Ready` | `Initializing` | Recycle invoked (new session id minted on same pane) | native recycle hook |
| `Ready` | `Completed` | Idle ceiling exceeded → reaper drains the pool member | reaper goroutine |
| `Ready` | `Failed` | Pane dies while idle (manual close, OOM) | pane-watcher |
| `Working` | `Completed` | Operator explicit end (current behavior preserved) | end.go |
| `Working` | `Failed` | Stall / agent crash mid-bead | end.go |

### §4.2 The bead-done boundary signal

Today, agents signal status transitions via `cmd/gemba-state` tokens:
`ready`, `working`, `prompting`, etc. A new token `bead-done` is added.
The agent emits this when its current bead is closed (e.g. immediately
after `bd close <id>`); the bridge translates it to an
`OrchestrationEvent{Kind: "session_state_reported", Payload["state"] = "bead-done"}`
which `state_events.go:handleStateEvent` maps to `SessionReady` AND
clears the session's `ActiveTurnID`.

`bead-done` is distinct from existing `ready`:

- `ready` (today) means "agent is at a prompt, no work in flight." Used
  during boot before the preamble lands.
- `bead-done` means "agent finished a bead and is going idle as a pool
  member." Implies the bead is closed in beads + the worktree is in a
  clean state for the next bead.

The agent-side helper `gt done` (or skill) is updated to emit
`bead-done` after completing the merge-queue submission, replacing the
current `gt done` → terminate-session sequence.

### §4.3 Worktree retention

When a session goes idle, its worktree is **retained, not released**.
This is the warm-context invariant. A pool member's session profile
(concepts, files touched, last beads) is meaningful only if the worktree
is still on disk.

When the session is recycled, the worktree may be reset (`git clean -fd
&& git checkout main && git pull`) but the pane and its claude process
are preserved. The recycle protocol (§5) details the exact reset.

When the session is reaped (idle ceiling exceeded) or ended (manual stop
/ failure), the worktree IS released. This matches today's end-of-session
cleanup at `end.go:104` and reuses the existing release path.

### §4.4 Idle-pane reaper

A goroutine started at server boot (one per server, not per pool) ticks
every minute and reaps pool members whose `last_heartbeat` (or, fallback,
`Status=Ready` since timestamp) exceeds `idle_ceiling_minutes`. Reaping
calls `EndSession(ctx, sessionID, SessionEndCompleted, nonce)` which
follows today's destructive cleanup path.

The reaper is the safety belt. Without it, a forgotten session can hold
a worktree + pane indefinitely, especially on dev laptops where the
operator forgot to stop the server. `idle_ceiling_minutes = 30` is the
default; production deployments with cheap pane resources can raise it.

`recycle_after_beads = N` is a sibling safety belt: even without health
threshold trips, recycle a session after every N beads to bound profile
staleness. `0` disables it; `5` is a reasonable default. Recycles from
this knob are logged distinctly from health-driven recycles so operators
can see which is firing.

## §5. Recycle protocol

### §5.1 Recycle triggers (re-stated from `work-planning.md` §4.5 / §5.5)

The daemon's existing `Recycler` interface is called when `ShouldRecycle`
returns true. Triggers:

- `context_pressure > 0.85` AND incoming bead's affinity is below the
  ready-set median.
- `concept_drift > 0.7` AND incoming bead has < 0.3 concept overlap with
  session lifetime.
- `time_on_task > 4h` AND incoming bead starts a new concept area.
- `bead_count >= recycle_after_beads` (this doc's safety belt; not in
  `work-planning.md` because it's lifecycle, not health).

Recycles never fire mid-bead. The decision happens at the
`Ready → Working` boundary, immediately before the daemon would call
`SessionDispatcher.Dispatch`.

### §5.2 The native recycle operation

A new `Recycle(ctx, sessionID)` method on the native adaptor. Sequence:

1. Validate the session is in `Status=Ready`. Mid-bead recycle is
   rejected; this is a contract assertion.
2. **Verify worktree is clean.** Run `git -C <worktree> status
   --porcelain`; if any output, the worktree is dirty. **Refuse to
   recycle.** Convert the recycle request into an end-and-respawn:
   call `EndSession(SessionEndCompleted)` on the slot, log a
   `WARN session.recycle.refused.dirty_worktree` event, and let
   the next dispatch tick spawn a fresh pool member. **Never
   destructively reset a dirty worktree** — uncommitted work is
   the operator's, not the planner's, to decide what to do with.
   This is a hard invariant: a dirty worktree at recycle time
   indicates the prior session ended without honoring the §5.4
   cleanliness contract, and the safe action is to surface it via
   the cold-spawn cost rather than silently `git reset --hard`.
3. Send the recycle keystroke sequence to the pane. For claude this is
   `/clear` (or whatever flushes the in-memory transcript). For shell-
   like agents, a re-exec.
4. Resync the (clean) worktree to its base:
   - `git -C <worktree> checkout <base_branch>` (return to base; only
     safe because step 2 confirmed clean).
   - `git -C <worktree> pull --ff-only` (sync remote).
5. Mint a new `session_id` for the next chapter. Reset session profile
   and last-heartbeat. Status returns to `Initializing`.
6. Re-deliver the boot preamble (from `internal/adapter/native/preamble`)
   so the new context starts properly primed.
7. Emit a `session.recycled` `OrchestrationEvent` with the prior session
   id, the new session id, the trigger reason, and the prior session's
   profile snapshot. The retro pipeline reads this to grade recycle
   timing.

The pane id and worktree path are stable across the recycle. `paneSessions[pane_id]`
in `internal/adapter/native/parallel.go` is updated to swap the prior
session id out for the new one in place.

### §5.3 Why recycle, not respawn

Three reasons:

1. **Spawn cost amortization.** A claude pane costs ~5–10 seconds of boot
   time before the first prompt is accepted. A recycle costs ~1 second
   (clear + git checkout + pull). If the pool churns through 50
   beads/day, the difference is 4 minutes/day per pool slot. Across a
   10-slot fleet that's 40 minutes/day of idle agent time.

2. **Worktree continuity.** The worktree has cached `node_modules`, Go
   module caches, gitnexus index, etc. Cold-spawning means re-warming
   these. Recycle keeps them warm. (Note: the *git* state is reset to
   base; the *filesystem* state — caches outside the git index —
   survives.)

3. **Pool identity stability.** Operators thinking about "PM-persona
   slot 1 in gemba" want that slot to have a stable identity. Recycle
   preserves the slot; respawn destroys it. This matters for the
   SPA's agent context strip (`work-planning.md` §6.1).

### §5.4 End-of-bead worktree cleanliness invariant

The recycle protocol's refuse-on-dirty stance (§5.2 step 2) places a
hard contract on the prior session: **when a session emits
`bead-done`, its worktree MUST be clean** — every change for the bead
is committed and pushed; no untracked files except those covered by
`.gitignore`; no detached HEAD; no in-progress merge or rebase.

Two layers enforce this. Both must be present (defense in depth):

#### §5.4.1 Agent-side: the `bead-done` skill commits + pushes

The agent's `gt done` (or equivalent end-of-bead skill) is updated to
run, in order:

1. `git status --porcelain` — if dirty, run `git add -A && git
   commit -m "<deterministic auto-commit message; format below>"`.

   **The auto-commit message is deterministic, not LLM-generated.**
   Format:
   ```
   chore(<bead-id>): auto-commit before bead-done

   Uncommitted changes captured by §5.4.1 fallback. The agent's
   normal commit flow did not run for these; review before
   treating as intent.
   ```
   Auto-commits should be *rare* — they fire when the agent skipped
   a normal commit. The deterministic message acts as a flag in
   `git log`: "this commit is suspicious, review it." Retro grading
   greps for `auto-commit before bead-done` to count contract
   violations and tune the §5.4.1 contract over time. LLM-generated
   commit messages are right for normal commits but wrong here:
   we're surfacing a contract violation, not summarizing intent.
2. `git push origin <branch>` — push to the upstream the bead is
   bound to (typically `main` for direct-merge beads, the merge
   queue branch for `--merge=mr` beads).
3. After successful push, emit `gemba-state bead-done`. Only then
   does the bridge transition the session to `SessionReady`.

If any step fails (e.g. push rejected by hook, commit fails), the
agent does NOT emit `bead-done`. It surfaces the failure as an
operator-visible escalation (`escalation.bead_done_blocked`) and
the session stays in `SessionWorking`. The reconcile loop (§9)
catches this if the agent itself crashes during the sequence.

The system-prompt-level instructions (CLAUDE.md, persona TOMLs)
already include language like "work is not complete until `git
push` succeeds" — this contract makes that language load-bearing.

#### §5.4.2 Bridge-side: verify before transitioning

When the bridge receives a `bead-done` token, it does NOT
immediately transition to `SessionReady`. Instead:

1. Run `git -C <worktree> status --porcelain`.
2. If output is empty (clean), proceed with the transition.
3. If output is non-empty (dirty), refuse the transition and
   emit `escalation.bead_done_with_dirty_worktree` instead.
   The session stays in `SessionWorking`; an operator-visible
   escalation surfaces the divergence. The agent skill failed to
   honor §5.4.1 — that's a bug worth surfacing, not silently
   masking.

This belt-and-suspenders pattern means a buggy or hand-rolled
agent skill cannot poison the pool. The invariant survives skill
regressions.

#### §5.4.3 Why so strict

The pool model trades cold-spawn cost for warm-context retention.
If recycle silently `git reset --hard`s away uncommitted work,
the operator's trust in the system collapses on the first lost
edit. The pool becomes worse than no pool. Forcing the cleanliness
contract upstream (commit + push before going idle) means the
recycle path stays simple and the operator's trust stays intact.

## §6. Daemon integration

The four interfaces the existing `planner.autodispatch.Daemon` requires
map onto the new lifecycle as follows:

### §6.1 `IdleSessionLister` → `SessionReady` filter

```go
type idleListerImpl struct{ op core.OrchestrationPlaneAdaptor; profiles ProfileStore }

func (i *idleListerImpl) ListIdle(ctx context.Context) ([]planner.OperationalContext, error) {
    sessions, err := i.op.ListSessions(ctx, core.SessionFilter{
        Status: []core.SessionStatus{core.SessionReady},
    })
    if err != nil { return nil, err }
    // For each session, build OperationalContext via planner.ReadOperationalContext
    // (existing function in internal/planner/operational_context.go).
    out := make([]planner.OperationalContext, 0, len(sessions))
    for _, sess := range sessions {
        ctx, err := planner.ReadOperationalContext(ctx, sess.ID, /* readers */)
        if err != nil { continue }
        out = append(out, *ctx)
    }
    return out, nil
}
```

### §6.2 `LiveSessionLister` → `SessionWorking` filter

Symmetrical to §6.1 but with `Status: []SessionStatus{SessionWorking, SessionPrompting}`.
Used by the daemon's conflict graph to find beads currently in flight so
their workspace conflicts block dispatch of conflict-adjacent ready
beads.

### §6.3 `SessionDispatcher` → `StartSession` with `gemba:reuse_pane_id`

```go
func (d *dispatcher) Dispatch(ctx context.Context, sessionID string, beadID core.WorkItemID) error {
    // Look up the session's pane id
    sessions, _ := d.op.ListSessions(ctx, core.SessionFilter{...})
    paneID := paneIDForSession(sessions, sessionID)
    if paneID == "" { return errors.New("dispatcher: pane not found for idle session") }

    prompt := core.SessionPrompt{
        Extension: map[string]any{
            "gemba:bead_id":         string(beadID),
            "gemba:persona_id":      d.poolPersona,            // bound to this daemon's pool
            "gemba:agent_type":      d.poolAgentType,          // derived from persona at construction
            "gemba:nonce":           newAutodispatchNonce(),
            "gemba:reuse_pane_id":   paneID,                   // ← THE pool semantic
            "gemba:autodispatch":    "1",
        },
    }
    _, err := d.op.StartSession(ctx, string(beadID), prompt)
    return err
}
```

This routes the new bead onto the same pane the idle session lives on,
which the native adaptor already supports via the cap-checking branch
at `internal/adapter/native/start.go:140-204`.

### §6.4 `SessionRecycler` → native recycle hook

```go
func (r *recycler) Recycle(ctx context.Context, sessionID string) error {
    return r.op.RecycleSession(ctx, sessionID)  // new adaptor method (§5.2)
}
```

The new `RecycleSession` method joins the `OrchestrationPlaneAdaptor`
interface as an optional capability. Adaptors that don't implement it
return `KindUnsupported`; the daemon's recycle gate becomes a no-op
for those adaptors (the `Recycler` field on the daemon is already
optional — see `internal/planner/autodispatch/daemon.go:155`).

## §7. Manual + auto coexistence

### §7.1 The SPA drag still works

The drag-to-start flow at `web/src/pages/BoardPage.tsx:onDragEnd` is
unchanged. PATCH the bead state, then POST `/api/sessions`. The handler
at `internal/server/sessions.go:startBeadSession` already calls
`pickReusePane` to find an idle session of the right agent type — under
the new lifecycle, idle pool members will satisfy this picker, so a
manual drag picks up an idle pool member transparently. No SPA changes
needed.

### §7.2 Pool depletion semantics

When the daemon (or a manual drag) wants to dispatch a bead and the
pool has no idle member:

1. **If pool size is below configured target**, spawn a fresh pool
   member (lazy growth, §3.3). The new session boots and accepts the
   bead — same as today's manual flow.
2. **If pool size is at target and all members are working**, the
   daemon waits for the next tick. The conflict graph already handles
   this: the bead is reported as `OutcomeBlockedByGate` with reason
   "no idle session in pool."
3. **If pool size is over target** (operator shrunk the config at
   runtime), the daemon dispatches normally and the reaper drains the
   excess on the next idle window.

### §7.3 Race resolution: drag + auto picking same bead

The claims index (`internal/planner/claims/`) already enforces single-
assignee with an RWMutex. If a daemon tick and a manual drag both try
to claim the same bead, whichever wins the mutex commits; the loser
gets a clean rejection with reason `bead_already_claimed`. The daemon
treats this as `OutcomeError` with the typed reason; the SPA shows a
brief toast.

This is unchanged from today; the section is here only to confirm the
existing mechanism is sufficient under the new lifecycle.

### §7.4 Operator verbs: `End` vs `Recycle`

A pool slot's pane can hold many session ids over its lifetime
(every recycle mints a new id on the same pane). When the operator
clicks something in the SPA's session card, two distinct intents
must be supported. The SPA exposes them as **two distinct buttons,
both nonce-gated**:

| Button | Intent | Effect |
|--------|--------|--------|
| `End` | "Stop this thing." | Tears down the pool slot: graceful pane shutdown, worktree release, slot removed from pool. Daemon may spawn a fresh slot on next dispatch (subject to lazy-growth rules). Existing button — semantics preserved. |
| `Recycle` | "Reset the chapter, keep the slot." | Calls the §5.2 protocol: clean-worktree check → keystroke clear → resync → new session id minted. Pane stays alive, profile resets. New button. |

`End` matches today's operator mental model (the existing button keeps
its existing semantics). `Recycle` is a new explicit verb for
"contaminated context, fresh start, same slot." Both go through
`requireConfirmNonce` at the HTTP layer.

Wire shape:
- `DELETE /api/sessions/{id}?mode=canceled` — existing, ends slot.
- `POST /api/sessions/{id}/recycle` — new, runs §5.2 protocol.

`POST /api/sessions/{id}/recycle` follows the same response shape as
`POST /api/sessions` (returns the new `core.Session` with the freshly-
minted id) so the SPA can swap the card content without a refresh.

## §8. Soft gates beyond what the daemon already does

### §8.1 Auto-dispatch floor

`work-planning.md` §6.2 step 4 specifies an `auto_dispatch_floor`
(default 0.5): the daemon does nothing if the top selection's score
falls below this. This gate is added to the daemon's `Tick` between
the recycle check and the dispatch:

```go
if top.scores.Combined < d.AutoDispatchFloor {
    return Action{Outcome: OutcomeBelowFloor, Reason: "score below floor"}
}
```

**Configured per-pool with a rig-level default cascade**, mirroring
`pool.size`:

```toml
[pool]
default_floor = 0.5
[pool.gemba.engineer-claude]
floor = 0.4   # this pool overrides the default
```

The cascade keeps surface area small: most operators set
`default_floor` once at the rig level and forget; power users
override per pool when they want a hacking pool to dispatch
aggressively (low floor) and a production pool to be conservative
(high floor).

### §8.2 Pool-occupancy ceiling

A daemon-level cap distinct from `MaxConcurrent`:
`pool_occupancy_ceiling` is the maximum fraction of pool members that
may be `Working` simultaneously. Default 1.0 (use the whole pool).
Setting to 0.75 keeps a quarter of the pool idle as a buffer for
manual drags and high-priority interrupt work.

### §8.3 Cold-start grace

A pool member whose session profile is empty (zero beads completed)
gets a **one-bead grace**: it is preferred for dispatch on its first
bead, then competes normally on affinity from the second bead onward.
Implementation: when `len(profile.LastBeads) == 0`, `Affinity`
returns a synthetic mid-band score so the fairness boost + priority
dominate the pick. From the second bead the real session profile
exists and grace ends.

**Why N=1 beads, not T minutes**: time-based grace is the wrong
abstraction. A fresh pool member that sits idle for 30 minutes is no
warmer than one that sits idle for 30 seconds — neither has any
profile. The grace's purpose is "give it a first bead to load
context"; once it has one bead, it has *some* profile and the
affinity model can do its job. T-minute grace would either expire
without the member doing any work (defeating the purpose) or last
arbitrarily long during low-traffic periods.

## §9. Failure modes

| Mode | Detection | Recovery |
|------|-----------|----------|
| Idle session's pane dies (manual close) | Pane-watcher observes EOF on the bridge tailer | Transition to `Failed`, reap, daemon spawns a fresh member next tick |
| Agent emits `bead-done` but bead is not actually closed in beads | Reconcile loop reads `bd show <bead_id>` after each `bead-done` | If still open, transition to `Stalled` instead of `Ready`; log the divergence |
| Recycle requested on dirty worktree (§5.2 step 2) | Bridge runs `git status --porcelain`, output non-empty | Refuse recycle. Convert to End + spawn-fresh. Log `WARN session.recycle.refused.dirty_worktree`. The dirty worktree means the prior session violated §5.4 — surface it as a cold-spawn cost rather than mask it |
| Agent emits `bead-done` with dirty worktree (§5.4.2) | Bridge cleanliness check fails | Refuse the `Working → Ready` transition. Emit `escalation.bead_done_with_dirty_worktree`. Session stays in `Working` for operator triage |
| Pool grows past `MaxParallel` from manifest | Effective size clamped at config load with WARN; runtime pushes through clamp logged + dropped | Pool sizing is best-effort against `MaxParallel`. The clamp is not silent — see §3.3 documentation requirement |
| Operator changes `pool.size` at runtime | Config reload notices delta | Reaper drains excess on next idle window; daemon spawns more on next dispatch tick |
| Session sits in `Ready` past `idle_ceiling_minutes` | Reaper (§4.4) | Reap (graceful end + worktree release) |
| Server restarts with idle pool members alive in tmux | New server reattaches via tmux session list | Treat reattached panes as `SessionReady`; profile is rebuilt from beads `last_beads` field |
| Daemon recycles session, recycle succeeds, but next dispatch fails | New session id minted, no bead delivered | Session sits in `Initializing` until next tick; daemon retries |

## §10. Observability

### §10.1 Pool state endpoint

`GET /api/pools` returns:

```json
{
  "pools": [
    {
      "rig": "gemba",
      "agent_type": "claude",
      "size_target": 3,
      "size_actual": 3,
      "idle": 1,
      "working": 2,
      "members": [
        {"session_id": "...", "pane_id": "...", "status": "ready", "last_bead": "gm-foo", "beads_done_this_member": 4, "last_recycle_at": "2026-04-29T17:00:00Z"},
        ...
      ]
    }
  ],
  "captured_at": "2026-04-29T18:42:00Z"
}
```

Read-only, no nonce. SPA's `/sessions` page surfaces this above the
existing session list.

### §10.2 Dispatch decision log

The daemon's existing `Action` events (`internal/planner/autodispatch/
daemon.go:79`) are persisted via `dispatch.Store`. We extend the
`Action` payload with `pool_member_id`, `recycle_triggered`, and
`floor_blocked` so the retro pipeline can grade pool decisions.

### §10.3 Recycle audit trail

`session.recycled` events are persisted to the `session_recycles`
dolt table:

```sql
CREATE TABLE session_recycles (
  id          VARCHAR(64) PRIMARY KEY,
  pool_key    VARCHAR(128) NOT NULL,  -- "rig:agent_type"
  pane_id     VARCHAR(64)  NOT NULL,
  prior_session_id VARCHAR(64) NOT NULL,
  new_session_id   VARCHAR(64) NOT NULL,
  reason      VARCHAR(64)  NOT NULL,  -- "context_pressure" | "concept_drift" | "time_on_task" | "bead_count_safety_belt"
  prior_profile_json TEXT,
  recycled_at TIMESTAMP    NOT NULL,
  INDEX (pool_key, recycled_at)
);
```

Used by the retro pipeline to grade recycle timing: did the next bead
on this slot do better after the recycle? If not, the threshold is
miscalibrated.

## §11. Migration plan

### §11.1 Phase 0 — pool size 0 = today's behavior

The lifecycle change in §4 is opt-in. With `pool.default_size = 0`
(the default), no `bead-done` token is emitted (agent skill defaults
to `gt done` → end-session) and the daemon is not constructed. Zero
behavioral delta from today's main.

### §11.2 Phase 1 — opt-in rig, size = 1

A single rig (probably `mike2` or a fresh test rig) sets
`pool.gemba.claude.size = 1`. The daemon runs against it; one pool
slot. Validates the lifecycle change end-to-end without fleet-level
risk. Bake for ~1 week.

### §11.3 Phase 2 — scale up

Production rigs adopt `size = 2` or `3`. The auto-dispatch floor is
tuned based on Phase 1 telemetry. Recycle thresholds are tuned based
on Phase 1 retros.

### §11.4 Phase 3 — gt parity (gm-e7.9)

The gt orchestration adaptor implements its own session lifecycle
including `RecycleSession` (likely shelling to `gt handoff`). Pool
semantics extend to gt-managed sessions. Today's stub-only state
becomes parity-with-native.

## §12. Open questions

### Resolved (architect, 2026-04-29)

1. ✅ **`bead-done` source-of-truth.** Autonomous emit from the agent
   skill. Reconcile loop (60s) is the safety net for crashes mid-emit.
   Reflected in §4.2 and §9.

2. ✅ **Recycle on dirty worktree.** **Refuse, do not destructively
   reset.** End the session and let a fresh spawn replace the slot.
   This pulls the cleanliness invariant upstream into the agent's
   `bead-done` skill (§5.4) — commit + push are mandatory before
   going idle. Reflected in §5.2 step 2 and §5.4.

3. ✅ **Pool key is `(rig, persona)`, not `(rig, agent_type)`.** A
   persona uniquely implies its agent_type and system prompt;
   different personas on the same agent_type warrant separate pools
   because their warm contexts are not interchangeable. Reflected
   in §2 vocabulary, §3.2, §3.3 TOML schema, §6.3 dispatcher.

4. ✅ **`MaxParallel` clamp.** Clamp `pool.size` to `MaxParallel -
   reserved_for_manual` at config load with a startup `WARN`.
   `reserved_for_manual` defaults to 1 to ensure manual drag is
   never starved. Documentation requirement explicit in §3.3 — TOML
   schema comments, user guide section, and startup banner all
   surface the clamp. Part of gm-s47n.12 DoD.

### Resolved (architect, 2026-04-29 — round 2)

5. ✅ **Cold-start grace = 1 bead.** Time-based grace is the wrong
   abstraction (a member that sits idle isn't getting warmer). After
   one bead the session has *some* profile and competes normally on
   affinity. §8.3 updated.

6. ✅ **Auto-dispatch floor is per-pool with a rig-level default
   cascade.** Mirrors `pool.size`. TOML:
   ```toml
   [pool]
   default_floor = 0.5
   [pool.gemba.engineer-claude]
   floor = 0.4   # per-pool override
   ```
   Most operators set `default_floor`; power users override per
   pool. §8.1 updated.

7. ✅ **Two distinct SPA verbs:** `End` (existing button — preserves
   today's slot-tear-down semantics) and `Recycle` (new — calls §5.2
   in place on the same slot). Both nonce-gated. SPA gets a recycle
   button next to End.

8. ✅ **Three-layer persona routing cascade**, lowest precedence to
   highest:
   1. `[pool] default_persona` — server fallback
   2. `[pool.routing.<kind>]` — per-bead-kind mapping
   3. Bead extras `persona` field — explicit override on the bead
   ```toml
   [pool]
   default_persona = "engineer-claude"
   [pool.routing]
   epic = "pm-claude"
   bug = "engineer-claude"
   decision = "pm-claude"
   ```
   No layer resolves → autodispatch refuses; manual drag still
   works. §3.2 updated.

9. ✅ **Auto-commit message is deterministic.** Format:
   ```
   chore(<bead-id>): auto-commit before bead-done

   Uncommitted changes captured by §5.4.1 fallback. The agent's
   normal commit flow did not run for these; review before
   treating as intent.
   ```
   Auto-commits should be rare; the deterministic message makes
   them grep-able for retro grading and signals "review this."
   §5.4.1 updated.

**§12 closed.** All design questions resolved. gm-s47n.11 and
gm-s47n.12 may proceed.

## §13. Appendix: code touchpoints

Estimated impact surface for the implementation beads (gm-s47n.11,
gm-s47n.12). Concrete file:line will be refined in those beads' PR
descriptions; this is for design-review sizing.

| File | Change |
|------|--------|
| `internal/adapter/native/end.go:34` | Branch on `bead-done` vs end-session; preserve pane on the former |
| `internal/adapter/native/start.go:140-204` | Pane-reuse path validates `SessionReady` source session |
| `internal/adapter/native/state_events.go:22` | Handle `bead-done` token → `SessionReady` |
| `internal/adapter/native/recycle.go` | NEW: implement `RecycleSession` per §5.2 |
| `internal/adapter/native/reaper.go` | NEW: idle-ceiling reaper goroutine (§4.4) |
| `internal/adapter/native/preamble/` | Re-deliver preamble on recycle |
| `cmd/gemba-bridge/state.go` | Accept `bead-done` token |
| `cmd/gemba-state/main.go` | Add `bead-done` subcommand |
| `core/orchestration.go` | Add `RecycleSession(ctx, sessionID)` to `OrchestrationPlaneAdaptor` (optional capability) |
| `core/state.go` | No change (`SessionReady` already exists at line 411) |
| `internal/server/sessions.go` | New `/api/pools` handler; existing `/api/sessions` paths unchanged |
| `internal/server/pools.go` | NEW: pool state read endpoint (§10.1) |
| `internal/cli/serve.go:234` | Construct + Run the daemon when `pool.size > 0` |
| `internal/server/autodispatch_wire.go` | NEW: implement the four daemon adapter interfaces (§6) |
| `internal/config/serve.go` | New `[pool.*]` config schema |
| `docs/design/work-planning.md` | Cross-reference this doc from §6.2 (forward-link) |
| Schema migration | NEW: `session_recycles` table (§10.3) |

Net new files: ~6. Touched existing files: ~10. Estimated total LOC:
800–1200 across both implementation beads (gm-s47n.11 ~600, gm-s47n.12
~400) plus tests.

---

**Architect resolutions captured (2026-04-29):**

- [x] Pool key is `(rig, persona)` — persona is the right granularity
- [x] Lazy pool growth (not eager) is the default
- [x] `bead-done` is autonomous from the agent (not polled by gemba)
- [x] Recycle **refuses** on dirty worktree — cleanliness invariant
      enforced upstream by the `bead-done` skill (§5.4)
- [x] `MaxParallel` clamp with WARN; `reserved_for_manual = 1` default;
      documentation required in TOML comments + user guide + banner
- [x] Migration phase 0 = zero delta from today's main
- [x] Cold-start grace = 1 bead (not time-based)
- [x] Auto-dispatch floor = per-pool with rig-level default cascade
- [x] Manual-end gets two SPA verbs: `End` (slot teardown) +
      `Recycle` (in-place §5.2)
- [x] Persona routing = three-layer cascade
      (default → kind table → bead extras override)
- [x] Auto-commit message is deterministic with §5.4.1 marker

**All §12 questions resolved. Status: ratified.**
