---
title: "Milestone convention"
decision: none
backfill: pending
---

# Milestone convention

**Status:** accepted (gm-root.3.1)
**Parent epic:** [gm-root.3](../../.beads/) — Milestones as first-class stage-gate construct in Gemba

## Why this doc exists

Gemba treats "milestone" as a first-class WorkItem kind — distinct from
an epic, a task, or a bug — because milestones gate work across phases
and need to be filterable, queryable, and addressable like any other
WorkItem. The underlying WorkPlane adaptor (today: `bd`) doesn't have a
native milestone type, so Gemba encodes the distinction with a label
convention that projects both ways through the adaptor boundary.

This document is the canonical source of truth for that convention.
Adaptor code and any downstream feature (UI badges, roll-ups,
phase-gating logic) MUST follow it.

## The rule

A bead is a **milestone** iff it carries the label `type:milestone`.

That's the whole convention. Everything below is a consequence of this
one rule.

## Where it lives in the stack

### Core

`internal/core/types.go`

```go
// KindMilestone is Gemba-native: there is no native "milestone" type in
// bd. The Beads adaptor encodes a milestone as `-t epic` + label
// "type:milestone" and projects that convention onto KindMilestone on
// read. Filtering WorkItemFilter.Kinds to {KindMilestone} returns only
// the label-flagged beads.
const KindMilestone = "milestone"
```

`WorkItem.Kind` is a free `string` field. `KindMilestone` is the
canonical token; other kinds (`"task"`, `"epic"`, `"bug"`, ...) flow
through unchanged from the adaptor's native issue_type.

### bd adaptor read

`internal/adapter/bd/types.go`

```go
const milestoneLabel = "type:milestone"

// inside bead → WorkItem projection:
if hasLabel(b.Labels, milestoneLabel) {
    kind = core.KindMilestone
}
```

The label wins over `b.IssueType` for Kind selection — a bd issue whose
native type is `"epic"` becomes `Kind=KindMilestone` on projection iff
it carries `type:milestone`. The native type `"epic"` is preserved on
the read side only as the underlying bd storage detail; the core
surface doesn't see it.

### bd adaptor write

When `CreateWorkItem` receives `wi.Kind == core.KindMilestone`, the
adaptor rewrites the bd command:

```
bd create -t epic -l type:milestone[,…] <title> …
```

i.e. native type is forced to `"epic"` and `type:milestone` is appended
to the label set (idempotently — never duplicated).

`UpdateWorkItem` does not currently translate Kind because
`core.WorkItemPatch` has no `Kind` field. Transitioning a bead in or
out of milestone status is done by patching `Labels` directly; the
read-side projection will pick up the change on the next GET.

### Filter semantics

`WorkItemFilter.Kinds = [core.KindMilestone]` selects only beads that
project to milestone kind. The bd adaptor pushes this down as a native
`--label type:milestone` filter when it's the only kind in the set; for
multi-kind requests it falls back to the in-process `matchesFilter`
predicate so cross-kind selections still work.

## Hierarchy

Milestones can appear anywhere in the parent/child graph:

- a milestone can parent epics (the common case: "MVP ships" owns the
  epics that make MVP happen);
- a milestone can be a leaf of an epic (e.g. a deliverable named
  inside a broader roadmap epic);
- a milestone can stand alone with no parent.

The convention does not impose a shape. Any UI feature that cares about
phase-gating is free to require a specific hierarchy, but the core
contract does not.

## Non-goals

- **No milestone-specific UI yet.** This document locks the data-model
  contract. Badges, roll-ups, phase-banner rendering are later work
  tracked under gm-root.3's children past .3.4.
- **No cross-adaptor federation.** The label convention is specific to
  bd. Jira / GitHub / Linear adaptors, when they land, can use their
  native milestone primitive and project onto `KindMilestone` without
  needing a label.
- **No progress roll-ups.** "N of M child beads complete" logic is out
  of scope; milestones are an identity + filter primitive here, not a
  progress primitive.

## Migration note

Existing epics that used to carry a `MILESTONE:` title prefix were
relabeled with `type:milestone` ahead of this convention landing (see
gm-root.1). New milestones MUST go through the label path; future tools
should not resurrect the title-prefix convention.

## References

- Parent epic: `gm-root.3` — Milestones as first-class stage-gate construct
- Core constant: `internal/core/types.go` (`KindMilestone`)
- Adaptor read: `internal/adapter/bd/types.go` (`milestoneLabel`, projection)
- Adaptor write: `internal/adapter/bd/workplane.go` (`CreateWorkItem`)
- Tests: `internal/adapter/bd/workplane_test.go`
  (`TestBeadsMilestoneLabelProjectsToKindMilestone`,
  `TestListWorkItems_MilestoneKindFilter`)
