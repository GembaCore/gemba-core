// Package runway implements the per-session runway estimator
// (gm-v5z2.4, work-planning.md §4 Layer 5.1).
//
// Runway is the planner's read-only answer to "how big a bead can
// this session realistically take next?" It composes the existing
// session-health telemetry into a single bucket — small / medium /
// large — that maps directly onto the EstimatedSize bucket on each
// bead (gm-v5z2.1). Selection (gm-v5z2.7) compares the two and
// demotes oversized candidates by a tunable factor.
//
// Pure function. No I/O. The CLI surface in
// internal/cli/session_status.go materialises inputs from the
// caller-supplied envelope.
package runway
