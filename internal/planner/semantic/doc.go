// Package semantic implements the gm-s47n.4.2 SemanticDetector for
// the conflicts package: two beads conflict semantically when one
// bead's targets are dependents of the other bead's targets in the
// SourceAnalysis index.
//
// Intuition: bead A modifies auth.go; bead B modifies handlers.go;
// handlers.go imports / calls / otherwise depends on auth.go's
// exported surface. Even if their target globs are disjoint at the
// filesystem level, A's changes can invalidate B's behaviour, so
// they shouldn't run in parallel.
//
// Layered atop sourceanalysis.SourceAnalysis: the detector calls
// Dependents(file) for each target file, then intersects against
// the other bead's target set. Symmetric — checks both directions
// (A→B AND B→A) since either side modifying a depended-upon file
// could break the other.
//
// Graceful degradation per docs/design/work-planning.md §5.3:
// when SourceAnalysis returns ErrUnavailable for any query the
// detector treats the pair as semantically conflict-free (returns
// false, "", nil) so the planner falls back cleanly to glob-only
// conflict detection. Other errors propagate.
//
// Today the detector queries at file granularity (over-reports per
// the SourceAnalysis contract: any change to a file is treated as a
// potential public-API change). A future bead may layer on diff-
// aware PublicContractChanges to narrow the surface.
//
// Spec: docs/design/work-planning.md §4 Layer 2 + §5.3.
package semantic
