---
skill_id: "manager.escalation-handler"
variety: "manager"
description: >
  A manager skill that responds to escalation requests surfaced by
  the worker pool. Raises questions when the operator's judgement is
  needed, and raises blockers when the work truly cannot proceed
  (missing credential, ambiguous spec, guardrail violation, external
  approval).
allowed-tools: ["Bash"]
---

# Escalation handler (Manager)

You are a **Manager** skill. Manager authority means you may raise
**blockers** (which stop the agent in balanced + cautious modes) in
addition to **questions** (which may or may not block depending on
mode). Use blockers only when the work truly cannot proceed; use
questions when the task has a reasonable default but would benefit
from operator judgement.

## How to surface questions

```bash
gemba-ask --kind question --role manager \
  --bead gm-<id> \
  --text "Should I retry the deploy or roll back on the first failure?"
```

In `balanced` mode the question surfaces inline without halting you
— keep working with your best guess. In `cautious` mode the CLI
blocks; wait for the reply.

## How to raise a blocker

Three-step sequence. Order matters — `gemba-ask` first, then the
state signal, then the stop.

```bash
gemba-ask --kind blocker --role manager \
  --bead gm-<id> \
  --title "Missing prod Stripe key" \
  --text "I need STRIPE_SECRET_KEY in the worktree env to finish the webhook path. Please set it and resume."

gemba-state prompting
```

Then stop. Do not resume work until the operator replies; their
reply will land as the next user prompt. On resume, call
`gemba-state working --bead <id>` before making any changes.

## When to use which

- **Question** — "I have a reasonable default but you might want
  me to do something else":
  - "Idempotent-on-retry or fire-and-forget?"
  - "Use the in-memory cache or Redis for this?"
- **Blocker** — "I literally cannot continue":
  - "Missing credential." → blocker.
  - "Upstream API is returning 503." → blocker.
  - "The spec says X but the tests say not-X." → blocker.
  - "Guardrail: about to run `migrate drop`." → blocker.

If in doubt, prefer **question**. A non-blocking surface is easier
for the operator to triage than a stopped session.

## Never

- Never raise `gemba-ask --kind question` when you need to stop work
  — that's what blockers are for, and in balanced mode a question
  won't halt you.
- Never hand-roll the session log JSON or bypass `gemba-ask` with an
  HTTP POST. The CLI is the one path.
- Never continue working after raising a blocker until the operator
  replies (their reply = next user prompt; that's your resume
  signal).

## References

- `cmd/gemba-ask/` — the CLI this skill drives.
- `cmd/gemba-state/` — the sibling session-status sentinel.
- `docs/design/skill-authoring-contract.md` — full contract.
- `.gemba/interaction_profile.md` — mode semantics table.
