---
title: "Design"
decision: none
---

# Design

Durable architectural decisions and design contracts. Each page
captures the *why* behind a part of the system that wouldn't be
obvious from the code alone — the design constraints, the rejected
alternatives, and the invariants downstream code can rely on.

> **Note on stand-alone reading.** Architecture decisions live in
> beads (`bd list type:decision`) and are referenced from the design
> docs here. Read the design doc for the contract; read the linked
> bead for the decision context that produced it.

## Foundations

- **[Parallelism boundary](parallelism-boundary)** — deconfliction
  precedes dispatch. The contract that lets `intra_parallel` agents
  share a pane without weakening any parallelism rule.
- **[Persona PPPP](persona-pppp)** — the four-axis persona model
  (Purview / Posture / Perspective / Pulse) that governs how each
  AI role behaves at runtime.
- **[Milestone convention](milestone-convention)** — how Gemba models
  milestones, why they're optional, and what their absence means
  for sprint composition.

## Surfaces

- **[Gemba walk](gemba-walk)** — the bounded review session: agenda
  aggregator, decision lifecycle, and the five-source design.
- **[Bead presentation](bead-presentation)** — what a WorkItem
  renders as in the SPA, and why each affordance is gated by
  capability.
- **[Beads-only operating mode](beads-only-mode)** — lets Gemba run as
  a Beads viewer and manager without project or orchestration setup.
- **[Work planning](work-planning)** — the planning loop: from
  freeform intent to bead graphs.
- **[Complexity-aware dispatch](complexity-aware-dispatch)** —
  estimates work depth/span and matches beads to agent/model
  capability envelopes before selection.
- **[Native support improvements](native-support-improvements)** —
  provider fidelity classes, terminal/session workspace UX, remote
  native pairing, source-control health, conversational controls, and
  native/tmux operator guidance.

## Operational

- **[Containerized sessions](containerized-sessions)** — the
  cwd-constraint contract for agents running inside containers.
- **[CWD constraint](cwd-constraint)** — defense in depth for keeping
  agents inside their workspace.
- **[E2E library](e2e-library)** — the playwright fixture matrix
  (smoke / chrome / route / realtime / modes / deep).
- **[Skill authoring contract](skill-authoring-contract)** — how
  persona Skills declare inputs / outputs / capability gates.

## Process

- **[Decision process](decision-process)** — how Gemba captures,
  ratifies, and supersedes design + implementation decisions. The
  `D#` numbering convention, the doc ↔ bead linkage, and the
  draft → in_review → ratified ceremony.

## Where next

- Operator-facing guides: see [Getting Started](../getting-started/).
- Per-adaptor authoring docs: see [Adaptors](../adaptors/).
