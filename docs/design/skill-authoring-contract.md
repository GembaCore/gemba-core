# Skill authoring contract (Coach / Manager)

**Status:** accepted (gm-97w7.1)
**Parent:** [gm-97w7](../../.gemba/interaction_profile.md) — Session state machine + interaction_profile

## What this doc is for

Every skill that ships with Gemba — bundled defaults under
`cmd/gemba-bridge/skills/` and operator-authored skills that land in
`.gemba/skills/` — routes operator-facing attention through a single
structured surface:

**The `gemba-ask` CLI.**

A skill instructs the agent to call `gemba-ask` once per question or
blocker:

```bash
gemba-ask --kind question --role coach   --text "Default to test key or fail hard?"
gemba-ask --kind blocker  --role manager --text "Need STRIPE_SECRET_KEY to finish webhook path."
```

The binary writes a typed `GembaAsk` frame to the session log. The
bridge translator turns that into an `escalation_opened` event with
the kind (`question` / `blocker`), channel (`tool_call`), and
urgency (`advisory` / `blocking`) already stamped. The native
OrchestrationPlane stores it in the escalation index and surfaces it
via `/api/escalations`.

The operator responds either in the pane (the agent sees the reply as
a `UserPromptSubmit`) or via the SPA (the server replays the reply
through `POST /api/escalations/{id}/respond`).

### Why a CLI — not markdown scraping, not an MCP tool today

| Option                               | Pros                                         | Cons                             |
|--------------------------------------|----------------------------------------------|----------------------------------|
| `gemba-ask` CLI (this contract)      | Matches `gemba-state` precedent. Typed capture. Zero parsing. Works for any agent with shell access. | None vs scraping. Requires agent to run shell. |
| Transcript scraping `## Questions`   | Human-readable inline.                        | Fragile; agent-specific; lossy. Format drift drops escalations. |
| MCP `ask_question` / `raise_blocker` | Native tool use for MCP clients. Schema-validated args. | Heavier; requires MCP server impl. Lands as follow-up. |

The CLI form is the primary surface today; the MCP variant is a
future enhancement that will reuse the same `(kind, channel=tool_call,
urgency)` shape.

## Role authority

Each skill declares a `variety` in its front matter: `coach` or
`manager`. This aligns with the Persona Purview model in
[`persona-pppp.md`](persona-pppp.md) / gm-2yg.

| variety | May emit `--kind question` | May emit `--kind blocker` |
|---------|----------------------------|---------------------------|
| coach   | yes                        | **no**                    |
| manager | yes                        | yes                       |

**Coaches never block.** The `gemba-ask` CLI rejects
`--role coach --kind blocker` at the process boundary; the bridge
translator also drops any `GembaAsk` frame with that combination, so
a hand-crafted frame can't bypass the rule.

## Blocking policy — mode, not skill

The skill decides **whether to emit**. The `interaction_mode`
(`dangerous | balanced | cautious` from `.gemba/agents.toml`) decides
**whether it blocks**. `gemba-ask` captures the active mode in the
frame payload and the translator computes urgency from `(kind, mode)`
via the matrix in `.gemba/interaction_profile.md`:

| mode      | kind=question  | kind=blocker |
|-----------|----------------|--------------|
| dangerous | drop (CLI rejects) | drop (CLI rejects) |
| balanced  | advisory       | blocking     |
| cautious  | blocking       | blocking     |

Skills do not branch on mode. They emit; the stack decides.

## What the skill instructs the agent to do

A well-formed Coach skill tells the agent:

> Call `gemba-ask --kind question --role coach --text "<your question>"`
> whenever you would like operator judgment on a decision. In
> `balanced` mode keep working with your best guess; in `cautious`
> mode the CLI's side effect will halt you — wait for the reply.

A well-formed Manager skill tells the agent:

> For questions, call `gemba-ask --kind question --role manager
> --text "…"`. For blockers, call
> `gemba-ask --kind blocker --role manager --text "…"` and then
> `gemba-state prompting` before waiting. Coaches cannot raise
> blockers; Manager skills escalating a guardrail violation MUST
> use blocker kind.

Skills MAY additionally print a `## Questions` / `## Blockers`
markdown section mirroring the captured items so operators watching
the pane see them inline. Gemba does not parse this echo —
`gemba-ask` is authoritative — but matching text helps operators who
respond directly at the terminal.

## What the skill must NOT do

- Do not emit blockers from a Coach skill. If you need to block, the
  skill is a Manager skill — update its front matter instead of
  bypassing the contract.
- Do not issue `gemba-ask` calls in `dangerous` mode. The CLI rejects
  this; the profile instructs the agent to record an assumption and
  proceed.
- Do not hand-roll the session log JSON. Always go through the CLI —
  it stamps `ts`, `event_id`, mode, and session id consistently.
- Do not build your own question-capture surface (an HTTP POST, a
  custom file write, a third-party tool). The goal of this contract
  is one surface; contributing to it is cheap, forking it is not.

## Interaction with `gemba-state`

The interaction profile tells the agent when to call
`gemba-state prompting` around a surfaced item:

- In `balanced` mode: `gemba-state prompting` only around blockers.
  Questions surface without halting the agent.
- In `cautious` mode: `gemba-state prompting` around anything
  surfaced.

The two signals are complementary:

- `gemba-ask` writes the escalation content (what the operator sees
  and responds to).
- `gemba-state prompting` communicates "the agent is currently
  blocked on operator input" so the session dashboard renders the
  right badge.

An agent that forgets to call `gemba-state prompting` still gets the
correct `blocking` flag on the escalation — the CLI's payload is
authoritative for urgency. The gemba-state call is for the session
badge, not the escalation.

## Front-matter shape for bundled skills

Bundled skills live as markdown files with a YAML front-matter
block:

```markdown
---
skill_id: "pm.stage_epics"
variety: "manager"      # coach | manager
description: >
  Reviews the unstaged-epic queue, proposes a staging order, and
  raises blockers for any dependency conflicts.
---

You are the Project Manager for this workspace.

When you identify a dependency conflict that prevents a clean
staging order, call:

    gemba-ask --kind blocker --role manager \
              --bead <epic_id> \
              --text "<one-line description of the conflict>"

Then call `gemba-state prompting` and wait for the operator.

<body of the skill prompt>
```

The authoring lint (gm-97w7.1 follow-up) asserts:

- `variety` is `coach` or `manager`.
- If `variety == "coach"`, the body does not contain
  `gemba-ask --kind blocker`.
- The body references `gemba-ask` at least once, so a skill that
  surfaces anything goes through the structured surface.

## Response pathway

Operators resolve escalations in one of two places:

- **In the pane**: operator types a reply at the agent's prompt.
  Claude sees it as the next `UserPromptSubmit`; the correlator
  (gm-native.13) resolves the oldest open escalation for the
  session.
- **In the SPA**: operator uses the escalation inbox. The server
  calls `ResolveEscalation` which routes by `channel`:
  - `channel=notification` → `Backend.SendKeys` with a
    "yes"/"no"/modify reply (from gm-native.13).
  - `channel=tool_call` → `Backend.SendKeys` writes the operator's
    reply as the next user prompt. No yes/no framing; the body is
    the reply.

Skill authors don't handle either pathway — the adaptor does. The
contract only asks the skill to call `gemba-ask` with a well-formed
request.

## References

- [`interaction_profile.md`](../../.gemba/interaction_profile.md)
  — mode semantics.
- [`persona-pppp.md`](persona-pppp.md) — Coach / Manager authority
  model (PPPP, gm-9rv).
- `cmd/gemba-ask/` — the sentinel CLI binary.
- `cmd/gemba-state/` — the sibling session-status sentinel.
- Bead: gm-97w7.1 — Coach/Manager skill contract + unified
  escalations surface (Option C).
- Parent epic: gm-97w7 — Session state machine + interaction_profile.
