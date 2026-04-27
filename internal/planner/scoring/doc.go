// Package scoring implements the WP2.0 score components Selection
// (gm-v5z2.7) composes:
//
//   - Leverage (gm-v5z2.5): score by how many other beads this bead
//     unblocks. Pure function over a DependencyView.
//   - EpicAffinity (gm-v5z2.5): "finish what you started" bias —
//     score boosts candidates in the same parent epic as the
//     session's recently-closed beads, decays per-turn, hard 0
//     once a different epic has been contiguously worked.
//
// Spec: docs/design/work-planning.md §4 Layer 3.2 (epic-affinity)
// + §4 Layer 3.3 (Leverage).
//
// Pure: no I/O. Callers materialise the dependency view + the
// session's epic streak and pass them in.
package scoring
