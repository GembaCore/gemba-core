# Dispatch vs Planning: two ranking systems, two jobs

Gemba has two ranking systems with distinct jobs: **Selection** answers "what
should this session work on next?", and **epic_order** answers "how should
the next sprint be composed?". They look superficially similar — both rank
work — but they operate at different time horizons, granularities, and cost
tiers, and they surface in different places. This page exists so the
architectural intent is discoverable before the code is.

## The two systems

| | **Selection** | **epic_order** |
|---|---|---|
| Granularity | Per-bead (one ready WorkItem at a time) | Per-epic (a candidate set staged for a sprint) |
| Latency | Microseconds (pure Go, in-process) | Seconds (LLM consult round-trip) |
| Cost | Free — no external calls | LLM dollars per consult (gated by `budget_policy`) |
| Determinism | Same inputs always produce the same sorted list | Nondeterministic — model output varies run to run |
| Surfaced via | `/coach` affinity grid + auto-dispatch loop | `/sprints` page + PM panel quick action (post-`gm-szz0.1`) |
| Inputs | Operational context: live sessions, workspace conflicts, owner claims, dispatch_status, runway, intent, fairness age | Operator-curated `EpicOrderInput` — candidate epics with derived signals, sprint budget, free-text guidance, recent completions, open escalations |
| Output | Sorted `[]Result` with per-gate Justification lines | JSONL stream of strategy / recommendation / warning / deferred / summary lines, each with confidence + rationale |
| Code path | `internal/planner/selection/` | `internal/skills/epic_order/` (PM persona's first skill) |

## When to use which

- **"What should I work on right now?"** → `/coach`. Selection has already
  filtered every ready bead through the six dispatch gates and ranked the
  survivors. The grid shows you the warmest session-bead pairings.
- **"How should we plan the next sprint?"** → `/sprints`. The PM persona's
  `epic_order` skill takes your candidate epics, your sprint budget, and
  your free-text guidance ("focus on unblocking UI work") and returns a
  ranked composition with confidence scores you can reason about.
- **"Which epics are ready vs blocked?"** — both pages will surface this
  once `gm-szz0.2` lands. Until then, `/coach` shows per-bead dispatch
  readiness (the structural truth for individual work items) and
  `/sprints` shows the operator-curated narrative (the planning truth for
  candidate epics).

## What each one cannot do

**Selection** has no concept of sprints, dollar budgets, narrative
rationale, or operator free-text guidance. It cannot tell you *why* a bead
is ranked where it is in prose — only which gates fired and which
sub-scores contributed. That is by design: the dispatch loop runs on every
session pulse, so it must stay deterministic and µs-fast.

**epic_order** ignores workspace conflicts, runway demotions, owner
claims, and per-bead leverage scoring. It does not know which sessions are
live or which beads are mid-flight. It reasons over the operator's
candidate set as given. It cannot run on a dispatch loop — every consult
costs LLM dollars and takes seconds.

## How they hand off

**Today: independent.** The `/coach` grid and the `/sprints` recommendations
are produced from disjoint inputs. The PM persona writes recommendations
that an operator applies; the dispatch loop ranks individual beads on its
own clock.

**Future (gm-szz0.2):** `epic_order`'s `EpicInput` will grow a
`StructuralReadiness` field populated from Selection's per-epic aggregate
(rolled up from the per-bead gate results). This lets the persona's
narrative recommendations cite real structural facts — "every bead in
gm-e3 passes the runway gate; gm-e6 has two beads soft-demoted by
intent" — instead of inferring them from operator-supplied summaries.
That bead is **deferred** and tracked separately; nothing in this page
has shipped yet relies on it.

## Why two systems instead of one

Structural correctness, speed, and determinism (Selection's strengths)
are a different problem from narrative reasoning over operator intent and
sprint budget (epic_order's strengths). One system trying to do both
would compromise on both: a dispatch loop that called an LLM every pulse
would burn tokens and add seconds of latency to a hot path; a planning
consult that re-derived workspace-conflict edges from scratch would be
slower, less correct, and would duplicate logic that Selection already
owns. Splitting the two lets each subsystem stay simple inside its own
contract.

## Code pointers

- Selection package overview: [`internal/planner/selection/doc.go`](https://github.com/MikeBengtson/gemba/blob/main/internal/planner/selection/doc.go)
- epic_order skill overview: [`internal/skills/epic_order/doc.go`](https://github.com/MikeBengtson/gemba/blob/main/internal/skills/epic_order/doc.go)
- Coach handler that surfaces Selection inputs: `internal/server/planner_coach.go`
- PM persona doc (epic_order surface): [Project Manager](../personas/project-manager.md)
