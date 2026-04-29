# Agents, sessions, and agent types

Three nouns that get conflated in casual speech but are distinct
in Gemba's data model and runtime. Knowing which is which pays off
when reading config, debugging a row in `/sessions`, or writing a
new adaptor.

## The three layers

```
┌──────────────────────────────────────────────────────────────────┐
│  Agent (identity)                                                │
│  e.g. polecat-onyx, mike2, deployment-engineer-1                  │
│  • persists across sessions                                       │
│  • carries reputation, history, owner of WorkItems                │
│  • typed by AgentRef in core; surfaced under /api/v1/agents       │
└─────────────────────────────┬────────────────────────────────────┘
                              │ runs
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│  Session (runtime)                                                │
│  e.g. a91415c1                                                    │
│  • a tmux pane (or Docker container under containerised mode)     │
│  • short-lived: spawned to execute work, ends on exit             │
│  • state machine: ready → working → prompting → stalled →         │
│    completed                                                      │
│  • surfaced under /api/sessions and /sessions                     │
└─────────────────────────────┬────────────────────────────────────┘
                              │ runs
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│  Agent type (dialect)                                             │
│  e.g. claude, codex, aider-ollama, shell-only                     │
│  • the binary that runs inside the pane                           │
│  • declared in .gemba/agents.toml                                 │
│  • controls preamble strategy + hook profile                      │
└──────────────────────────────────────────────────────────────────┘
```

## A concrete example

Operator drags `gm-e4.2` to the In Progress column. The dispatcher:

1. **Picks an Agent**: `polecat-onyx` (identity — Gas Town persistent role).
2. **Spawns a Session**: a new tmux pane gets opened, assigned a
   fresh session id `a91415c1`, attached to that agent identity.
3. **Runs an agent type inside it**: per `agents.toml`, the
   `polecat` role pulls the `claude` agent type, so the pane
   exec's `claude --model claude-opus-4-7` with the gm-e4.2
   preamble injected via `CLAUDE.md`.
4. The Session row in `/sessions` shows: `agent=polecat-onyx`,
   `session_id=a91415c1`, `agent_type=claude`, `state=working`.

When the work finishes the Session ends. `polecat-onyx` (the agent
identity) sticks around — it might pick up the next bead in a brand-
new session a few minutes later.

## Why three layers and not two?

Two collapses would each lose something useful:

- **Collapse Agent + Session into one** — you'd lose the ability
  to track an identity across many sessions. Gas Town's
  reputation ledger, persona affinity scoring, and "give this
  bead back to the same operator who owned it last time" all
  depend on Agent persisting beyond any one Session.
- **Collapse Session + Agent type into one** — you'd lose the
  ability for the same agent (e.g. `mike2`) to spawn one Session
  running `claude` and another running `aider-ollama`. The agent
  type is a config concern; the Session is a runtime concern.

## Common phrases that look ambiguous

| Phrase | Means | Doesn't mean |
|---|---|---|
| "an agent session" | a Session attached to an Agent identity | a chat session distinct from "real" sessions |
| "spawn a worker" | spawn a Session (the spawned runtime) | a separate noun "Worker" |
| "the Claude agent" | the `claude` *agent type* in `.gemba/agents.toml` | a specific Agent identity called "Claude" |
| "polecat-onyx is working" | the Agent identity `polecat-onyx` has at least one Session in `state=working` | the Agent identity is itself the runtime |

## Where to look in the code

| Concept | Surface |
|---|---|
| Agent (identity) | `core.AgentRef` (`core/types.go`), `/api/v1/agents`, SPA `/agents/:id` (`web/src/pages/agents/AgentDetailPage.tsx`) |
| Session (runtime) | `core.Session`, `/api/sessions`, SPA `/sessions` (`web/src/pages/SessionsPage.tsx`), `internal/adapter/native/session.go` |
| Agent type (dialect) | `internal/adapter/native/agents/registry.go`, `.gemba/agents.toml`, [Agent setup guide](../getting-started/agent-setup) |

## See also

- [Agent setup](../getting-started/agent-setup) — how to configure
  agent types in `.gemba/agents.toml`.
- [Parallelism in Gemba](../getting-started/parallelism) — how
  per-agent-type `intra_parallel` + `max_parallel` interact with
  Sessions.
- [Native adaptor reference](../adaptors/native) — the spawn /
  pane-lifecycle implementation.
