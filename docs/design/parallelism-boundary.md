---
title: "Parallelism boundary \u2014 deconfliction precedes dispatch"
decision: gm-vgyp
d: D7
ratified_at: 2026-04-27
---

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

The native adaptor models intra-parallelism as **multiple Session records sharing one pane**. Each `StartSession` call still represents one bead and produces its own logical Session id, but for an `intra_parallel=true` agent type a caller can hand the new bead a `gemba:reuse_pane_id` extension to co-locate it inside an existing pane instead of spawning a new one.

- `paneSessions map[string][]string` (replaces `paneActive`) tracks every Session sharing a pane.
- Cap enforcement: a reuse request is rejected when `len(paneSessions[paneID]) >= ResolvedMaxParallel(agent)`.
- Pane teardown (quit sequence + Kill + CLAUDE.md sentinel cleanup) only runs on the **last session out** — earlier `EndSession` calls just decrement.
- Each transition emits a `session_parallel_changed` event so the SPA can update pills without polling. Emission is via `Fanout.Publish`, the public injector for adaptor-synthesized events.

The dispatcher *policy* (try reuse before spawning new) is intentionally **not** baked into the OrchestrationPlane. The plane provides the primitives — `PaneInFlight(paneID) int`, the reuse extension, cap enforcement — and a router upstream (operator click in the SPA, future auto-dispatcher) composes them. See `TestThreeBeadsTwoSessionsRouting` for a reference policy implementation.

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
