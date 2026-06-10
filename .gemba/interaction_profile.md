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

"Non-blocking" in `dangerous` means the agent never surfaces anything
at all — it records the assumption and proceeds. In the other modes
non-blocking means the item is captured and shown to the operator,
but the agent does not call `gemba-state prompting` and does not
stop work.

Coaches never raise blockers regardless of mode — that's a role
authority rule enforced by the skill-authoring contract
(docs/design/skill-authoring-contract.md), not by this profile.

## How to surface — the `gemba-ask` CLI

Skills surface operator attention by calling the `gemba-ask` CLI
once per question or blocker. The binary writes a typed frame that
Gemba turns into an escalation; no markdown parsing is involved.

```bash
gemba-ask --kind question --role coach   --text "Default to test key or fail hard?"
gemba-ask --kind blocker  --role manager --text "Need STRIPE_SECRET_KEY in env."
```

Flags:

- `--kind question | blocker` (required)
- `--role coach | manager` (required; Coach + blocker is rejected)
- `--text "<body>"` (required; what the operator will read)
- `--bead gm-<id>` (optional; the work item this ask belongs to)
- `--title "<short title>"` (optional; operator-facing one-liner)

The CLI reads `GEMBA_SESSION_ID` and `GEMBA_INTERACTION_MODE` from
its env (set by the adaptor at spawn time) to stamp the frame; it
will refuse to run in `dangerous` mode.

Every section below MUST:

1. Instruct the agent on when to stop vs keep going (per the matrix
   above).
2. Instruct the agent to call `gemba-state <state>` at every state
   boundary (ready / working / prompting) so the session dashboard
   stays in sync.
3. Instruct the agent to call `gemba-ask` per surfaced item, matching
   the mode's blocking policy. Optionally, print the same text as
   `## Questions` / `## Blockers` markdown in the visible response
   so operators watching the pane see it alongside the structured
   capture.

---

## dangerous

**Mode summary**: never ask, never stop.

You have been dispatched with a specific bead. Work the task
end-to-end using your best judgment without pausing for human input.
Make a reasonable choice and move forward. Do not call `gemba-ask`
— the operator has explicitly opted out of interruption.

State-signal contract:

- On session start, call `gemba-state ready` as soon as you've read
  the bead.
- Immediately after, call `gemba-state working --bead <id>` and begin
  execution.
- You MUST NOT transition to `prompting`. If you would normally ask,
  record the assumption in a one-line commit-message note and proceed.
- When the DoD is met (or you've determined the task cannot complete
  without violating a guardrail), stop executing and let the session
  end normally.

Risks (the operator has accepted these by choosing this mode):

- Destructive decisions are made without review.
- Divergence between what the operator wanted and what you did is
  only caught by the post-hoc review of the diff.

---

## balanced

**Mode summary**: stop for **Manager blockers only**. Questions (from
either role) surface as non-blocking escalations.

Work the bead autonomously when you can. When you hit either:

- **A question** — a decision that would benefit from operator
  judgment but has a reasonable default. Call
  `gemba-ask --kind question --role <coach|manager> --text "…"`
  and keep going with your best guess.
- **A blocker** — something you genuinely cannot proceed past (Manager
  role only; Coaches never raise blockers).

State-signal contract:

- On session start: `gemba-state ready`.
- When you begin execution: `gemba-state working --bead <id>`.
- When you raise a question: call `gemba-ask --kind question …` and
  continue working. Do NOT call `gemba-state prompting`. Do NOT stop.
- When you raise a blocker (Manager only):
  1. Call `gemba-ask --kind blocker --role manager --text "…"`.
  2. Call `gemba-state prompting`.
  3. Stop and wait. Do not resume work until the operator replies
     (their reply will show up as a new user prompt).
- When the operator answers and you resume, call
  `gemba-state working --bead <id>` again before making changes.

Optional in-pane echo:

You may also print a `## Questions` or `## Blockers` markdown section
mirroring the asks you just captured, so a human watching the pane
sees them inline. Gemba does not parse this echo — the CLI call is
authoritative — but matching text helps operators who respond
directly at the terminal.

---

## cautious

**Mode summary**: stop for anything surfaced. Any `gemba-ask` call —
Coach or Manager, question or blocker — halts the agent until the
operator replies.

Work the bead as autonomously as you can, but surface every
non-trivial decision. Do not guess through a question; stop and ask.

State-signal contract:

- On session start: `gemba-state ready`.
- When you begin execution: `gemba-state working --bead <id>`.
- When you raise any ask:
  1. Call `gemba-ask --kind <question|blocker> --role <coach|manager> --text "…"`.
  2. Call `gemba-state prompting`.
  3. Stop and wait for the operator.
- When the operator answers and you resume, call
  `gemba-state working --bead <id>` again.

This is the right mode when you want a tight feedback loop on a
risky task — every coach nudge and manager gate interrupts, at the
cost of slower autonomous progress.

---

## Reference

- `gemba-state` CLI (session-status sentinel): see `cmd/gemba-state/`.
- `gemba-ask` CLI (question / blocker sentinel): see `cmd/gemba-ask/`.
- Preamble composer that injects this file: see
  `internal/adapter/native/preamble/preamble.go`.
- Observable `SessionStatus` enum the state signals map to: see
  `internal/core/orchestration.go` (`SessionStatus`, gm-d044).
- Escalation schema + response path: see
  `docs/design/skill-authoring-contract.md` and gm-97w7.1.
- Epic: `gm-97w7`.
