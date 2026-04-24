# Skill authoring contract (Coach / Manager)

**Status:** accepted (gm-97w7.1)
**Parent:** [gm-97w7](../../.gemba/interaction_profile.md) — Session state machine + interaction_profile

## What this doc is for

Every skill that ships with Gemba — bundled defaults under
`cmd/gemba-bridge/skills/` and operator-authored skills that land in
`.gemba/skills/` — routes its operator-facing output through two and
only two markdown sections:

- **`## Questions`** — things the skill would like the operator to
  answer.
- **`## Blockers`** — things the skill cannot proceed past without
  operator action.

The transcript scanner (gm-97w7.1) reads those sections off the
assistant's last turn on every Stop hook, stamps them as typed
escalations (`kind=question` or `kind=blocker`), and surfaces them
through `/api/escalations`. The operator answers either in the pane
(the agent sees it as a new `UserPromptSubmit`) or in the SPA (the
server replays the reply via `POST /api/escalations/{id}/respond`).

Whether a given emission **blocks** the agent is computed by the
scanner from `(kind, interaction_mode)` — see the matrix in
[`.gemba/interaction_profile.md`](../../.gemba/interaction_profile.md).
Skills do not decide whether to block; they decide whether to emit.

## Role authority

Each skill declares a `variety` in its front matter: `coach` or
`manager`. This aligns with the Persona Purview model in
[`persona-pppp.md`](persona-pppp.md) / gm-2yg.

| variety | May emit `## Questions` | May emit `## Blockers` |
|---------|-------------------------|------------------------|
| coach   | yes                     | **no**                 |
| manager | yes                     | yes                    |

**Coaches never block.** A Coach skill that emits a `## Blockers`
section is a skill-authoring bug — the scanner will log it and
suppress the section rather than open an escalation.

Manager skills may emit either kind independently. A single response
may carry both `## Questions` and `## Blockers`.

## What the skill writes

The skill's prompt instructs the agent to use these exact section
headings and numbered-list formatting. The scanner matches on the
literal heading text.

```markdown
## Questions

1. Should I default to the Stripe test key or fail if it is missing?
2. Does the webhook path need to be idempotent on retry?

## Blockers

1. I need the prod Stripe key to finish the webhook path. Please set
   STRIPE_SECRET_KEY in the worktree env and resume.
```

Rules the scanner enforces:

- Heading is exactly `## Questions` or `## Blockers` (case-sensitive,
  two hash marks, single space, no trailing punctuation).
- Items are an ordered list (`1. `, `2. `, …). Bullet lists
  (`- `, `* `) are ignored so the scanner doesn't gobble narrative
  prose that happens to follow a Questions heading.
- One-line-per-item is the intent; multi-line items with
  continuation indentation are allowed but the scanner folds them
  into a single string.
- Sections may appear in any order; only the last occurrence of each
  heading within the final assistant turn is scanned, so drafts
  earlier in the turn are harmless.

## What the skill must NOT do

- Do not invent alternative headings (`### Questions`, `## Asks`,
  `## TODO`). Only `## Questions` and `## Blockers` are recognised.
- Do not emit either section from a Coach skill if it would be a
  blocker-shaped ask. If you need to block, the skill is a Manager
  skill — update its front matter instead of bypassing the contract.
- Do not write `## Questions` in `dangerous` mode. The profile
  instructs the agent to record an assumption and proceed instead.
  Emitting anyway counts as a skill-authoring bug; the scanner
  suppresses it and logs.
- Do not use `## Questions` / `## Blockers` as conversational
  scaffolding. The scanner is greedy — anything under those headings
  becomes an escalation.

## Interaction with `gemba-state`

The interaction profile tells the agent when to call
`gemba-state prompting` around a surfaced section. Skills
themselves do not call `gemba-state`; the agent orchestrates that
based on the profile injected into its preamble.

The scanner uses the session's `interaction_mode` (not the
gemba-state call) to decide `blocking`. This means an agent that
forgets to call `gemba-state prompting` still gets the correct
`blocking` flag stamped on the escalation — the two signals are
complementary, not redundant.

## Front-matter shape

Bundled skills live as markdown files with a YAML front-matter
block:

```markdown
---
skill_id: "pm.stage_epics"
variety: "manager"      # coach | manager
description: >
  Reviews the unstaged-epic queue, proposes a staging order,
  and raises ## Blockers for any dependency conflicts.
---

You are the Project Manager for this workspace. …

<body of the skill prompt>
```

The authoring lint (gm-97w7.1 follow-up) asserts:

- `variety` is `coach` or `manager`.
- If `variety == "coach"`, the body does not instruct the agent to
  emit `## Blockers`.
- The body references `## Questions` and/or `## Blockers` verbatim
  (so the scanner's regex matches against the wording the skill
  author intended).

## Response pathway

Operators resolve escalations in one of two places:

- **In the pane**: operator types a reply at the agent's prompt.
  Claude sees it as the next `UserPromptSubmit`; the correlator
  (gm-native.13) resolves the oldest open escalation for the session.
- **In the SPA**: operator uses the escalation inbox. The server
  calls `ResolveEscalation` which routes by `channel`:
  - `channel=notification` → `Backend.SendKeys` with a "yes"/"no"/
    modify reply (existing path from gm-native.13).
  - `channel=transcript` → `Backend.SendKeys` writes the operator's
    reply as the next user prompt. No yes/no framing; the body is
    the reply.

Skill authors do not need to handle either pathway — the adaptor
does. The contract only asks the skill to produce a well-formed
`## Questions` / `## Blockers` section.

## References

- [`interaction_profile.md`](../../.gemba/interaction_profile.md)
  — mode semantics and formatting contract.
- [`persona-pppp.md`](persona-pppp.md) — Coach / Manager authority
  model (PPPP, gm-9rv).
- Bead: gm-97w7.1 — Coach/Manager skill contract + unified
  escalations surface (Option C).
- Parent epic: gm-97w7 — Session state machine + interaction_profile.
