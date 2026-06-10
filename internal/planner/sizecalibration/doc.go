// Package sizecalibration implements the bead-size calibration
// loop (gm-v5z2.8 part b, work-planning.md §7.6).
//
// The estimated_size heuristic in §4 Layer 0 (small / medium /
// large, computed from description-length × DoD-line-count) is a
// rough first guess. The retrospective grades it by comparing
// predicted size against actual time-to-close — and when the
// median actual duration drifts > 2x in either direction for a
// given bucket, this package's Aggregate returns a Suggestion
// that shifts SizeHeuristicThresholds so future estimates land
// closer to reality.
//
// Calibration is per-rig (different codebases have different
// description norms); per-author drift detection is the next
// slice (more signal needed before splitting the sample stream).
//
// The aggregation is a pure function: callers supply Samples and
// (optional) target durations + current thresholds. The package
// does not own data fetching — the retrospective pipeline pulls
// closed beads from the bead history and feeds them in. This is
// what lets the CLI run offline against a JSON snapshot without
// needing a live dolt connection.
package sizecalibration
