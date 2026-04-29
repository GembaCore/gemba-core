---
title: "Decision process"
decision: gm-d1m1
d: D6
ratified_at: 2026-04-29
---

# Decision process

How Gemba captures, ratifies, and supersedes design + implementation
decisions.

## Why a separate process at all

Definition-of-Done blocks on a task bead are *implementation* scope —
"what does done look like for this slice of work." Decisions are
*guidepost* scope — "what shape does the system have, going
forward."

Conflating the two hides decisions inside task descriptions, makes
them un-discoverable later, and weakens the claim that "we decided X
on date Y, here is why" — the kind of claim a future reader needs
when they wonder if a constraint is still load-bearing.

Decisions get a separate type, a separate numbering convention, and
a separate ceremony.

## Lifecycle

```
   draft ─────▶ in_review ─────▶ ratified (closed)
                    │
                    └────────▶ rejected   (closed)
```

A ratified decision is **never re-opened in place**. To change
course, file a new D# that explicitly supersedes the prior one.

| Stage | bd status | Label | Trigger |
|---|---|---|---|
| draft | `open` | `d:draft` | author creating the bead |
| in_review | `open` | `d:in-review` | author signals "ready" + files a `gate` dep |
| ratified | `closed` | `d:ratified` | the gate resolves with approval |
| rejected | `closed` | `d:rejected` | the gate resolves against the proposal |
| superseded | `closed` (was ratified) | `d:superseded` | a new D# files a `supersedes` edge |

## Numbering — `D#`

- Every decision bead's title carries a `D#:` prefix (`D1:`, `D2:`,
  …, `D14:`). No zero-padding, no hyphen — matches the milestone
  convention (`M1`, `M2`).
- A `d:#` label mirrors the number for query (`bd query "label=d:14"`
  resolves the bead by handle).
- Numbers are monotonically assigned at creation time and **never
  reused**. A superseded decision keeps its number.
- The bead's own id (`gm-xxxxx`) is canonical for tooling; `D#` is
  the human handle used in code comments, commit messages, and
  design-doc frontmatter.

## Linkage — design doc ↔ decision bead

Every file under `docs/design/*.md` carries YAML frontmatter:

```yaml
---
title: Parallelism boundary
decision: gm-root.16          # bead id; OR "none"
d: D9                         # human handle when applicable
ratified_at: 2026-04-27        # set on close; mirrors bead history
---
```

Every decision bead's description carries a `Doc:` line pointing at
the matching path under `docs/design/`, OR `Doc: none` when the
decision is meta and produces no doc.

A linter (`make lint-decisions`) validates both directions in CI:
every doc has frontmatter, every decision has a `Doc:` line, every
back-reference resolves. The linter is wired to run on every PR
that touches `docs/design/` or modifies a decision bead.

## Authoring a new decision

1. Pick the next `D#`. Run:
   ```
   bd query "label~d:" -a --json | jq '[.[] | .labels[] | select(startswith("d:")) | sub("d:";"") | tonumber] | max + 1'
   ```
2. Create the bead:
   ```
   bd create --type decision \
     --title "D#: <topic>" \
     --priority 1 \
     --labels "decision,d:#,d:draft,fed:safe,risk:low,surface:docs"
   ```
3. Write the description. Required sections:
   - **Goal** — one paragraph
   - **Why now** — context that makes the decision necessary
   - **Decision** — what's being chosen, with the rejected
     alternatives if relevant
   - **DoD** — when do we consider this decision implemented
   - **Out of scope** — what this decision deliberately does *not*
     cover
   - **Doc:** — path to the design doc this decision will produce,
     or `Doc: none`
4. (Optional but encouraged) File the companion design doc with the
   matching `decision:` frontmatter; commit both in the same PR.

## Moving to in_review

1. `bd label remove <id> d:draft`
2. `bd label add <id> d:in-review`
3. File a `gate`-typed bead as a dependency, naming the
   approver(s) as assignees. The gate's resolution drives the next
   step.

## Ratifying

1. The approver closes the gate with the consensus / approval note.
2. ```
   bd close <decision-id> -m "Ratified — <one-paragraph rationale>"
   ```
3. ```
   bd label remove <id> d:in-review
   bd label add <id> d:ratified
   ```
4. The companion design doc is published (or already merged) with
   `ratified_at: <date>` filled in.

## Rejecting

1. The gate closes against the proposal.
2. ```
   bd close <decision-id> -m "Rejected — <alternative chosen>"
   ```
3. `bd label add <id> d:rejected`. No design doc; the rejection
   note IS the artifact. Future readers find it via
   `bd query "label=d:#"`.

## Superseding

A ratified decision is never re-opened in place. To change course:

1. File a new `D#` with the new direction.
2. ```
   bd dep add <new-id> supersedes:<old-id>
   ```
3. Mark the old decision: `bd label add <old-id> d:superseded`.
4. Update the companion design doc's frontmatter:
   ```yaml
   superseded_by: <new-bead-id>
   ```

## Worked examples

### D4 — ratified

`gm-sf51: D4: Insights data source — Prometheus proxy vs in-process
retention.` Ratified 2026-04-29 with close-reason naming Path A
(Prometheus proxy) as the chosen path. Companion doc not yet
published — the implementation epic that depends on it (`gm-e9m0`)
will produce it.

### D6 — ratified

`gm-d1m1: D6: Decision-capture convention.` This document.
Ratified 2026-04-29 once the implementation epic `gm-gas6` proved
the convention end-to-end (5 decisions migrated, 16 design docs
frontmatter'd, linter built + wired into CI). The epic was both
the gate AND the proof — atypical, but appropriate for a
meta-decision whose ratification *is* its successful application.

### Rejected example

(none yet)

### Superseded example

(none yet)

## Where next

- The implementation epic for this convention: `gm-gas6`.
- Decision beads: `bd query "label=decision" -a`.
- The linter: `make lint-decisions` (lands as part of `gm-gas6.3`).
