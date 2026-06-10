// Package dispatch persists dispatch decisions — the moment a coach
// (or, later, the auto-dispatch daemon) picks a bead for a session
// (gm-s47n.6.2, work-planning.md §5 Layer 5).
//
// Each row captures the *prediction*: which bead was picked, which
// session got it, the affinity score the planner showed at that
// moment, the conflict edges visible in the ready set, and the set
// of alternatives that were on the grid. The retrospective
// (gm-s47n.8.x) joins these rows against scorer_grades on bead_id
// to grade the planner — "did high-predicted-affinity picks land
// with low divergence?"
//
// The package is intentionally narrow: a Decision struct + Insert /
// Get / List on a *sql.DB. Callers (the coach HTTP handler, the
// auto-dispatch daemon when it lands) materialise the snapshot at
// pick time and hand it here. No I/O concerns leak out.
package dispatch
