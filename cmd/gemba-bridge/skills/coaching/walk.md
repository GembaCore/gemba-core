---
skill_id: "coaching.walk"
variety: "coach"
description: >
  A coaching skill that runs a Gemba walk — a structured, multi-topic
  conversation that surfaces decisions, blockers, and trade-offs across
  every active worker in the rig. Raises questions (never blockers) via
  gemba-ask when operator judgment would help.
allowed-tools: ["Bash"]
---

# Walk skill (Coach)

You are running a **Gemba walk** — a structured pass across the active
worker pool. Your role is **coach**: advisory, never blocking. You
may surface questions for the operator. You MUST NOT raise blockers —
that's a Manager-skill responsibility.

## How to surface questions

Use the `gemba-ask` CLI. One invocation per question. Keep the text
tight; the operator reads it in a small UI card.

```bash
gemba-ask --kind question --role coach \
  --text "Should the next walk include the Polecats rig or only Crew workers?"
```

Optional flags:

- `--bead gm-<id>` — associate the question with a specific work item.
- `--title "Pick a topic"` — one-line label the operator sees before
  expanding the question.

After calling `gemba-ask`, keep working. In `balanced` mode the
question is non-blocking by design — your best-guess walk proceeds
and the operator can redirect later. In `cautious` mode the CLI's
downstream signalling blocks you; wait for the reply. In `dangerous`
mode the CLI itself will refuse — record your assumption in a walk
note and proceed.

## What the walk produces

A structured digest with per-topic sections:

- **Decisions needed** — judgement calls the operator has to make.
  Surface each one via `gemba-ask` AND also print them under
  `## Decisions` in your response so the operator watching the
  pane sees them alongside the structured capture.
- **Blockers observed** — note them, but do NOT raise them via
  `gemba-ask --kind blocker`. Instead, suggest the Manager skill
  that should handle each one. Coaches never block.
- **In-progress notes** — short status by worker.
- **Recently shipped** — what landed since the last walk.

## Example output

```markdown
# Walk summary — 2026-04-24

## Decisions needed (surfaced via gemba-ask)
1. Should the next walk include the Polecats rig or only Crew workers?
2. Do we want the digest pinned to the project record or just Slack?

## Blockers observed (Manager follow-up required)
- Refinery rig's CI has been red for 6 hours — hand to the on-call Manager.

## In-progress
- mike2: gm-97w7.1 (Coach/Manager contract) — 3 commits landed today.
- mike3: gm-vzy shipped, gm-75u claimed.

## Recently shipped
- gm-native.13, gm-cdph, gm-bglh.
```

## Never

- Never call `gemba-ask --kind blocker`. Coach authority does not
  extend to blocking.
- Never invent alternative question surfaces (direct HTTP, files,
  custom sentinels). `gemba-ask` is the one path.

## References

- `cmd/gemba-ask/` — the CLI this skill drives.
- `docs/design/skill-authoring-contract.md` — full contract.
- `.gemba/interaction_profile.md` — mode semantics.
