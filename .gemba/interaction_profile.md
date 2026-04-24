# Interaction profile

Operator-facing contract that tells Gemba-dispatched agents how to
behave when they have questions, hit blockers, or want to pause.

The profile has three named sections — **dangerous**, **balanced**,
**cautious**. The section that gets injected into a given session is
picked by that session's agent-type config in `.gemba/agents.toml`:

```toml
[[agent]]
name             = "claude"
interaction_mode = "balanced"  # dangerous | balanced | cautious
```

Gemba's preamble composer (`internal/adapter/native/preamble/`) reads
this file, selects the matching section, and appends it as a single
envelope layer in front of the bead / DoD.

## Mode semantics at a glance (gm-97w7.1)

The three modes form a ladder from "least blocking" to "most blocking":

| mode      | Manager blockers | Manager questions | Coach questions |
|-----------|------------------|-------------------|-----------------|
| dangerous | non-blocking     | non-blocking      | non-blocking    |
| balanced  | BLOCKING         | non-blocking      | non-blocking    |
| cautious  | BLOCKING         | BLOCKING          | BLOCKING        |

"Non-blocking" in `dangerous` means the agent never emits a
`## Questions` or `## Blockers` section at all — it records the
assumption and proceeds. In the other modes non-blocking means the
section is printed and surfaced to the operator, but the agent does
not call `gemba-state prompting` and does not stop work.

Coaches never emit `## Blockers` regardless of mode — that's a role
authority rule enforced by the skill-authoring contract
(docs/design/skill-authoring-contract.md), not by this profile.

Every section below MUST:

1. Instruct the agent on when to stop vs keep going.
2. Instruct the agent to call `gemba-state <state>` at every state
   boundary (ready / working / prompting) so the operator's session
   dashboard stays in sync.
3. Instruct the agent to print `## Questions` and `## Blockers`
   markdown sections when the mode allows them, so the scanner
   (gm-97w7.1) can surface them as typed escalations.

---

## dangerous

**Mode summary**: never ask, never stop.

You have been dispatched with a specific bead. Work the task
end-to-end using your best judgment without pausing for human input.
Make a reasonable choice and move forward. Do not emit `## Questions`
or `## Blockers` sections — the operator has explicitly opted out of
interruption.

State-signal contract:

- On session start, call `gemba-state ready` as soon as you've read
  the bead.
- Immediately after, call `gemba-state working --bead <id>` and begin
  execution.
- You MUST NOT transition to `prompting`. If you would normally ask,
  record the assumption in a one-line commit-message note and proceed.
- When the DoD is met (or you've determined the task cannot complete
  without violating a guardrail), stop executing and let the session
  end normally — do not call `gemba-state stalled` or `prompting`.

Risks (the operator has accepted these by choosing this mode):

- Destructive decisions are made without review.
- Divergence between what the operator wanted and what you did is
  only caught by the post-hoc review of the diff.

---

## balanced

**Mode summary**: stop for **Manager blockers only**. Questions (from
either role) surface inline but do not halt the agent.

Work the bead autonomously when you can. When you hit either:

- **A question** — a decision that would benefit from operator
  judgment but has a reasonable default. Print it under `## Questions`
  and keep going with your best guess.
- **A blocker** — something you genuinely cannot proceed past (missing
  credential, ambiguous spec, upstream breakage, external approval).
  Manager-level skills only; Coach skills cannot raise blockers.

State-signal contract:

- On session start: `gemba-state ready`.
- When you begin execution: `gemba-state working --bead <id>`.
- When you raise a `## Questions` section: print it and continue
  working. Do NOT call `gemba-state prompting`. Do NOT stop.
- When you raise a `## Blockers` section (Manager only):
  1. Call `gemba-state prompting`.
  2. Print the `## Blockers` section with numbered items.
  3. Stop and wait. Do not resume work until the operator replies
     (their reply will show up as a new user prompt).
- When the operator answers and you resume, call
  `gemba-state working --bead <id>` again before making changes.

Formatting contract:

```markdown
## Questions

1. First question, stated as a direct sentence ending with a "?".
2. Second question…

## Blockers

1. Short description of the blocker. Name the specific thing you
   need (e.g. "I need the prod Stripe key to finish the webhook
   path"). Do not bury the ask inside narrative.
```

---

## cautious

**Mode summary**: stop for anything surfaced. Any `## Questions` or
`## Blockers` section — from Coach OR Manager — halts the agent until
the operator replies.

Work the bead as autonomously as you can, but surface every
non-trivial decision. Do not guess through a question; stop and ask.

State-signal contract:

- On session start: `gemba-state ready`.
- When you begin execution: `gemba-state working --bead <id>`.
- When you raise a `## Questions` or `## Blockers` section:
  1. Call `gemba-state prompting`.
  2. Print the section(s) with numbered items.
  3. Stop and wait for the operator.
- When the operator answers and you resume, call
  `gemba-state working --bead <id>` again.

This is the right mode when you want a tight feedback loop on a
risky task — every coach nudge and manager gate interrupts, at the
cost of slower autonomous progress.

---

## Reference

- `gemba-state` CLI (sentinel binary): see `cmd/gemba-state/`.
- Preamble composer that injects this file: see
  `internal/adapter/native/preamble/preamble.go`.
- Observable `SessionStatus` enum the signals map to: see
  `internal/core/orchestration.go` (`SessionStatus`, gm-d044).
- Transcript scanner + typed escalation surface: see
  `docs/design/skill-authoring-contract.md` and gm-97w7.1.
- Epic: `gm-97w7`.
