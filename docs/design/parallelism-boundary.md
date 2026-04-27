# Parallelism boundary — deconfliction precedes dispatch

Source: gm-root.16 / gm-root.16.1.

Gemba has two parallelism axes:

1. **Inter-session parallelism.** Multiple sessions, each carrying one bead, all running concurrently. This is the historical default.
2. **Intra-session parallelism.** A single session of a parallelism-capable agent type carries multiple concurrent beads (the prompt orders the agent to fan work out internally).

The capability is declared per agent type in `.gemba/agents.toml`:

- `intra_parallel: bool` — opt-in flag (default false).
- `max_parallel: int` — hard cap on concurrent beads per session. Required when `intra_parallel=true`; ignored otherwise (effective cap is always 1).

## The invariant

**Deconfliction runs before dispatch, regardless of which axis carries the next stream.**

Two beads that conflict (file overlap, lock contention, dependency ordering, parallel-group affinity, etc.) MUST NOT be dispatched concurrently — whether the next stream lands in a new session or alongside an existing bead in an intra-parallel session. The deconfliction layer is agent-agnostic and sits *upstream* of the dispatcher.

This means:

- The dispatcher never sees two conflicting beads at the same time.
- Whether a bead ends up in a fresh session or shares one with another bead is purely a routing decision after deconfliction has already approved it.
- Adding intra-parallelism does not weaken any existing parallelism rule.

## Dispatch order

```
ready beads
     │
     ▼
┌─────────────────────┐
│ deconfliction layer │  ← all parallelism rules apply here, once
└─────────┬───────────┘
          │  (approved-concurrent set)
          ▼
┌─────────────────────┐
│ dispatcher routing  │
│  • prefer existing  │  ← intra-parallel sessions with capacity
│    capable session  │
│  • else spawn new   │  ← fall back to inter-session
└─────────┬───────────┘
          ▼
       sessions
```

## What this implies for the native adaptor

- The `paneActive` map is no longer "1 bead per pane" — for `intra_parallel=true` agents it tracks a counted set capped at `MaxParallel`.
- The dispatcher's routing tries to reuse an active session of the right agent type with `count < MaxParallel` before spawning a new pane.
- Each transition (count up on assignment, count down on completion) emits a `session_parallel_changed` event so the SPA can update pills without polling.

The Layer 2 routing change is tracked separately (gm-root.16.1 follow-up); this doc is the contract that follow-up implements.

## What this rules out

- **Per-session deconfliction.** Putting parallelism rules inside an executor that only sees one session's beads is a drift bug — it would let two conflicting beads slip through if they happen to land in different sessions.
- **Probing capability at runtime.** Operators declare `intra_parallel`. We don't infer it from telemetry.
- **Dynamic cap adjustment.** `max_parallel` is constant for a session's lifetime. Restart to change.

## Event contract

Kind: `session_parallel_changed`

Payload (JSON):

```json
{
  "session_id": "tmux:gm-abc.1:1714169123456789",
  "agent_type": "claude",
  "in_flight": 2,
  "max_parallel": 3,
  "delta": 1
}
```

- `in_flight` — current concurrent bead count on this session.
- `max_parallel` — the resolved cap (always 1 for non-intra agents).
- `delta` — `+1` on assignment, `-1` on completion. Skips zero (no event for no-op transitions).

A reference fixture lives at `internal/adapter/native/testdata/session_parallel_changed.json`.
