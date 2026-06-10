// Package targets implements the target-overlap glob algorithm
// (gm-s47n.4.1) — given two WorkItems' target glob sets, decides
// whether they could touch the same concrete file path.
//
// The algorithm is the file-level half of the conflicts engine
// (gm-s47n.4 Layer 3). Distinct from:
//
//   - workspace-collision (gm-s47n.4.6) — two beads routed to the
//     same (repo, branch, worktree_path)
//   - semantic-conflict (gm-s47n.4.2) — two beads whose changes touch
//     dependents of each other's likely public symbols
//
// Three-layer decision tree, in order of precision:
//
//  1. EXACT — literal vs literal: overlap iff equal.
//  2. PREFIX TREE — directory prefix containment. "src/foo/**"
//     contains "src/foo/bar.go" even though the literals differ;
//     "src/foo/**" and "src/foo/bar/**" overlap because one prefix
//     contains the other.
//  3. SAFETY NET — when prefix analysis is inconclusive (mid-segment
//     wildcards on both sides), CompareWith resolves the ambiguity
//     by enumerating filesystem matches via the Filesystem interface
//     and intersecting. Compare alone returns Maybe.
//
// Pure function; no I/O in the core path. Callers that want a
// definitive bool route through CompareWith with a real filesystem.
//
// Spec: docs/design/work-planning.md §4 Layer 3 (when that lands).
package targets
