// Package conflicts is the bead-set conflict scorer (gm-s47n.4.3).
//
// Given a slice of Bead values (the planner's projection of WorkItem +
// targets + optional metadata), Conflicts returns:
//
//   - a Graph of pairwise conflict edges, each tagged with one or
//     more Reasons (target-overlap from gm-s47n.4.1, semantic from
//     gm-s47n.4.2, workspace-collision from gm-s47n.4.6)
//   - a parallel-safe Batches() partition: ordered list of bead sets
//     where each set is internally conflict-free, computed greedily
//     so a planner can dispatch one batch at a time without two
//     beads in the same batch racing each other
//
// Layer 3 of the work-planning subsystem (docs/design/work-planning.md
// §4 Layer 3 + §5). Pure function: no I/O in the core path. Optional
// dependencies (SemanticDetector, WorkspaceCollisionDetector,
// targets.Filesystem) are interfaces; callers wire what they have
// today and Conflicts skips the rest.
//
// Distinct from the workitems CLI's "blocks/blocked" relationship —
// that's an authoring-time declaration; this is a planner-derived
// pairwise risk surface.
package conflicts
