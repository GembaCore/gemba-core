# Parallelism in Gemba

Gemba runs work along two parallelism axes, and you control both
through `.gemba/agents.toml` plus the dispatcher's reuse policy.

## The two axes

**Inter-session parallelism** — multiple sessions, each carrying one
bead, all running concurrently. This is the historical default and
needs no opt-in. Spawn three sessions, run three beads.

**Intra-session parallelism** — a single session of a parallelism-
capable agent type carries multiple concurrent beads. The agent's
prompt orders the work to fan out internally. You opt in per agent
type.

Most CLI agents are single-stream and stay on the inter axis. Claude
Code in dangerous mode, Codex in batch mode, and shell-multiplexers
that orchestrate sub-tasks can declare themselves intra-parallel and
share one pane across multiple beads.

## Declaring capability

In `.gemba/agents.toml`:

```toml
[[agent]]
name           = "claude"
binary         = "claude"
preamble       = "claude_md"
hooks          = "claude_code"
intra_parallel = true
max_parallel   = 3
```

- `intra_parallel: bool` (default `false`) — does this agent type
  support multiple concurrent beads in one session?
- `max_parallel: int` — hard cap on concurrent beads per session.
  Required when `intra_parallel = true`. Ignored otherwise; the
  effective cap is always 1 for non-intra agents.

Validation runs at server startup:

- `intra_parallel = true` with `max_parallel <= 0` is a hard error.
- `max_parallel > 0` with `intra_parallel = false` (or absent) is a
  warning — the value is silently ignored, so the warning saves you
  from wondering why nothing parallelizes.

## How the dispatcher routes

When you POST to `/api/sessions` (or click "Start session" in the
SPA), the dispatcher decides where to land the new bead:

1. If you pass `pane_id` in the request body, that pane is used
   directly (operator override; bypasses the policy).
2. Otherwise the policy looks at every live session of the requested
   `agent_type` that's in `Ready`, `Working`, `Prompting`, or
   `Stalled` state.
3. From those, it picks the one with the **lowest in-flight count**;
   ties go to the **oldest StartedAt** (longest-running pane wins —
   it's presumably past initialization). The picked pane goes through
   to the adaptor as `gemba:reuse_pane_id`.
4. If no candidate has capacity, a fresh pane is spawned (the historic
   path).
5. Race fallback: if the picked pane fills between the policy snapshot
   and dispatch, the call retries with no reuse — you never see a
   4xx for a mechanical race.

The policy is deterministic. Two identical inputs route the same way
every time, which keeps the SPA's mental model legible.

## What the SPA shows

Each pane in the Sessions panel renders a small pill:

- `2/3` — pane currently runs 2 beads, cap is 3
- No pill — agent type's `intra_parallel` is `false` (always 1 bead)

A separate counter in the SPA chrome shows the **total in-flight
parallel beads across the whole installation** — the operator's
at-a-glance answer to "how parallel is the system right now."

Both surfaces update via SSE off the `session_parallel_changed`
event; no polling.

## Deconfliction is upstream

Whether the next bead lands intra- or inter-session does not weaken
any parallelism rule. File overlap, lock contention, dependency
ordering, parallel-group affinity — all of it applies *before*
dispatch. The dispatcher only ever sees a set of beads the
deconfliction layer has already approved as concurrent.

In practical terms: if two beads conflict, they will not run
concurrently — not in the same pane, not across panes. Adding intra-
parallelism does not introduce new ways to step on yourself.

## Tuning

- Start with `max_parallel = 2` for a new intra-parallel agent and
  watch the SPA pill under load. Bump up if the pane is rarely
  saturated; cap doesn't auto-tune.
- `max_parallel` is constant for a session's lifetime. Restart the
  session (or `gemba serve`) to change.
- There is no SPA control to edit `max_parallel`. It's a config-file
  edit on purpose — capacity is an architectural decision, not a
  knob you twiddle mid-run.

## What's not here

- **Auto-detection** of an agent's parallelism capability. You declare
  it; Gemba doesn't probe.
- **Dynamic capacity adjustment** based on observed performance.
- **Multi-tenant cap attribution**. The cap is per-session, full
  stop. If you need workspace-level limits, that's a different
  feature.

## Going deeper

The architectural contract — why deconfliction precedes dispatch, why
the cap lives at the agent-type layer, what the
`session_parallel_changed` event payload looks like — is in
[`docs/design/parallelism-boundary.md`](../design/parallelism-boundary.md).
