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

Every section below MUST:

1. Instruct the agent on when to stop vs keep going.
2. Instruct the agent to call `gemba-state <state>` at every state
   boundary (ready / working / prompting) so the operator's session
   dashboard stays in sync.
3. Instruct the agent to print `## Questions` and `## Blockers`
   markdown sections when it has either, so the operator can respond
   in the pane or via the SPA without drifting from the state signal.

---

## dangerous

**Mode summary**: never ask, never stop.

You have been dispatched with a specific bead. Work the task end-to-end
using your best judgment without pausing for human input. Make a
reasonable choice and move forward. Do not emit `## Questions` or
`## Blockers` sections — the operator has explicitly opted out of
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

**Mode summary**: stop for questions AND blockers. Default.

Work the bead autonomously when you can. When you hit either:

- **A question** — a decision you need from the operator that has
  a reasonable default but a meaningful downside if you guess wrong.
- **A blocker** — something you genuinely cannot proceed past (missing
  credential, ambiguous spec, upstream breakage, external approval).

…stop work and surface it.

State-signal contract:

- On session start: `gemba-state ready`.
- When you begin execution: `gemba-state working --bead <id>`.
- When you surface a question or blocker:
  1. Call `gemba-state prompting`.
  2. Print a `## Questions` section (if any) with numbered items.
  3. Print a `## Blockers` section (if any) with numbered items.
  4. Stop and wait. Do not resume work until the operator replies
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

**Mode summary**: surface every question; stop only for blockers.

Work the bead as autonomously as you can, but tell the operator
about every non-trivial decision you're making along the way.
Keep going unless you hit a true blocker.

State-signal contract:

- On session start: `gemba-state ready`.
- When you begin execution: `gemba-state working --bead <id>`.
- When you want to surface a question (any decision with a
  real-world tradeoff):
  - Print a `## Questions` section in the same formatting as
    balanced-mode. Do NOT stop work. Do NOT call
    `gemba-state prompting`. Continue with your best guess and let
    the operator redirect if needed.
- When you hit a genuine blocker (cannot continue without help):
  1. Call `gemba-state prompting`.
  2. Print a `## Blockers` section.
  3. Stop and wait.

This mode is the right choice when the operator is watching the
session live — the `## Questions` lines become a running commentary
they can nudge from without paying the context-switch cost of a
full stop.

---

## Reference

- `gemba-state` CLI (sentinel binary): see `cmd/gemba-state/`.
- Preamble composer that injects this file: see
  `internal/adapter/native/preamble/preamble.go`.
- Observable `SessionStatus` enum the signals map to: see
  `internal/core/orchestration.go` (`SessionStatus`, gm-d044).
- Epic: `gm-97w7`.
