---
title: "Sticky session pool + idle lifecycle for two-axis dispatch"
decision: gm-s47n.10
---

# Sticky session pool + idle lifecycle for two-axis dispatch

> Status: **draft / for review** — 2026-04-29
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
| **Pool** | A bounded set of long-lived sessions of one `agent_type`, scoped to a rig. Members carry continuous context across beads. |
| **Pool key** | The `(rig, agent_type)` tuple that uniquely identifies a pool. `(gemba, claude)` is one pool; `(gemba, codex)` is another. |
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

### §3.2 Pool key is `(rig, agent_type)`

A rig may host multiple pools — one per agent type (`claude`, `codex`,
`gemini`, etc.). The daemon picks dispatches against pools whose
`agent_type` matches the candidate bead. Beads without an explicit agent
type fall through to a configured default pool.

The pool key is *not* `(rig, agent_type, persona)`. Personas are dispatch-
time decoration on top of an agent type; conflating them with pool
identity would explode pool counts unnecessarily. A persona switch on a
recycled session is a re-priming, not a slot change.

### §3.3 Pool sizing

Two configuration shapes, in priority order:

1. **Per-rig, per-agent-type explicit:**
   ```toml
   [pool.gemba.claude]
   size = 3
   recycle_after_beads = 5    # safety belt; 0 disables
   idle_ceiling_minutes = 30  # idle beyond this → reap

   [pool.gemba.codex]
   size = 1
   ```

2. **Rig-level default:**
   ```toml
   [pool]
   default_size = 0   # opt-in; explicit pool blocks override
   ```

`size = 0` means "no pool" — today's behavior, every dispatch spawns
fresh. `size = N` means "maintain N pool members for this `(rig,
agent_type)`" subject to the manifest's `MaxParallel` constraints.

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
2. Send the recycle keystroke sequence to the pane. For claude this is
   `/clear` (or whatever flushes the in-memory transcript). For shell-
   like agents, a re-exec.
3. Reset the worktree:
   - `git -C <worktree> reset --hard` (drop any uncommitted changes;
     the bead's done so this is destructive-by-design).
   - `git -C <worktree> clean -fd` (remove untracked artifacts).
   - `git -C <worktree> checkout <base_branch>` (return to base).
   - `git -C <worktree> pull --ff-only` (sync remote).
4. Mint a new `session_id` for the next chapter. Reset session profile
   and last-heartbeat. Status returns to `Initializing`.
5. Re-deliver the boot preamble (from `internal/adapter/native/preamble`)
   so the new context starts properly primed.
6. Emit a `session.recycled` `OrchestrationEvent` with the prior session
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
   (clear + git reset). If the pool churns through 50 beads/day, the
   difference is 4 minutes/day per pool slot. Across a 10-slot fleet
   that's 40 minutes/day of idle agent time.

2. **Worktree continuity.** Even after `git reset --hard`, the worktree
   has cached node_modules, Go module caches, gitnexus index, etc. Cold-
   spawning means re-warming these. Recycle keeps them warm.

3. **Pool identity stability.** Operators thinking about "claude slot 1
   in gemba" want that slot to have a stable identity. Recycle preserves
   the slot; respawn destroys it. This matters for the SPA's agent
   context strip (`work-planning.md` §6.1).

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
            "gemba:agent_type":      d.agentTypeFor(sessionID),
            "gemba:nonce":           newAutodispatchNonce(),
            "gemba:reuse_pane_id":   paneID,  // ← THE pool semantic
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

Configurable per-pool. Default 0.5.

### §8.2 Pool-occupancy ceiling

A daemon-level cap distinct from `MaxConcurrent`:
`pool_occupancy_ceiling` is the maximum fraction of pool members that
may be `Working` simultaneously. Default 1.0 (use the whole pool).
Setting to 0.75 keeps a quarter of the pool idle as a buffer for
manual drags and high-priority interrupt work.

### §8.3 Cold-start grace

A pool member whose session profile is empty (no beads completed
yet) gets a one-time grace period before Layer 5 Selection scores it.
During grace it is preferred for dispatch (it has nothing to lose by
taking new work). Implementation: `Affinity` returns a synthetic
mid-band score for cold-start members, so the fairness boost +
priority dominate the pick.

## §9. Failure modes

| Mode | Detection | Recovery |
|------|-----------|----------|
| Idle session's pane dies (manual close) | Pane-watcher observes EOF on the bridge tailer | Transition to `Failed`, reap, daemon spawns a fresh member next tick |
| Agent emits `bead-done` but bead is not actually closed in beads | Reconcile loop reads `bd show <bead_id>` after each `bead-done` | If still open, transition to `Stalled` instead of `Ready`; log the divergence |
| Recycle git reset fails (uncommitted hooks, dirty submodules) | Recycle returns error from §5.2 step 3 | Daemon falls back to End + spawn-fresh on this pool slot |
| Pool grows past `MaxParallel` from manifest | Spawn rejects with cap error | Daemon logs, waits — pool size is best-effort against MaxParallel |
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

These need explicit decisions before §11.1 ships.

1. **`bead-done` source-of-truth.** Does the agent emit it autonomously
   (skill triggers on close), or does gemba poll for bead-state changes
   and infer it? The §4.2 design assumes autonomous emit. Risk: agent
   forgets and the session sits in `Working` forever. Mitigation:
   reconcile loop that compares `Session.ActiveTurnID` to bead state
   every 60s.

2. **Recycle's worktree reset is destructive by design.** What if the
   bead leaves work uncommitted (the agent crashed mid-push)? §5.2
   step 3 throws it away. Alternative: refuse to recycle when the
   worktree is dirty, end the session instead. Safer; means more cold
   spawns.

3. **Pool sizing under MaxParallel.** Manifest declares per-host
   `MaxParallel`. If `pool.size > MaxParallel`, who wins? Today the
   adaptor would reject the spawn. Proposal: `effective_size = min(pool.size, MaxParallel)` clamped at config load time, with a startup warning.

4. **Cold-start grace duration.** §8.3 specifies a "one-time" grace.
   Should it be N beads (e.g. 1) or T minutes? Affects how aggressively
   a fresh pool member competes against a warm one for the first work.

5. **Per-persona pools, or per-agent-type only?** §3.2 argues for the
   latter. But a "PM persona" and an "engineer persona" have wildly
   different prompts and might warrant separate pools even on the
   same agent type. Defer to follow-up if Phase 1 surfaces evidence.

6. **Auto-dispatch floor scope.** Is it global, per-pool, or per-rig?
   §8.1 says per-pool. Risk: too many knobs. Counterargument: a
   conservative ops team wants a high floor on production; a hacking
   team wants 0 on dev. Per-rig at minimum.

7. **What does manual end on a recycled-many-times session do?** The
   current pane has held maybe 20 session ids over its lifetime.
   Operator clicks End in the SPA. Do we end the *current* session
   (recycle to fresh slot) or end the *pool slot* (tear down the
   pane)? Proposal: SPA's End ends the slot; SPA's "recycle" button
   recycles. Two distinct verbs.

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

**Review checklist for the architect:**

- [ ] Pool key is `(rig, agent_type)` — not adding persona dimension
- [ ] Lazy pool growth (not eager) is the default
- [ ] `bead-done` is autonomous from the agent (not polled by gemba)
- [ ] Recycle is destructive on dirty worktrees by design (§12.2 open)
- [ ] Migration phase 0 = zero delta from today's main
- [ ] Open questions §12 are addressed (or explicitly deferred) before §11.1 ships
