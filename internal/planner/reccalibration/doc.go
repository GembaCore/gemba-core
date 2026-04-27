// Package reccalibration implements the §7.5 recommendation
// calibration loop (gm-v5z2.8 part a, work-planning.md §7.5).
//
// The retrospective in §7.1 grades the *score* against outcomes.
// This package adds the second feedback loop: grade the
// *recommendation* against operator overrides. Every coach-mode
// pick records (recommended_top_bead, picked_bead, score_delta,
// per-component snapshot, optional operator_reason). When the
// operator agrees with the planner the row is degenerate; when
// they disagree the row carries real signal.
//
// Aggregate spots four systematic patterns (defaults below tunable
// per rig once the operator-review queue lands):
//
//   - SystematicIntentMiss — when intent was set, operator
//     consistently picks outside the planner's top-3.
//   - SystematicLeverageMiss — operator prefers high-blocks-weight
//     beads the planner ranked lower.
//   - SystematicRunwayOvertrust — operator overrides the runway
//     demotion frequently; the runway estimator is too pessimistic.
//   - OverrideTop3 — operator regularly picks outside the planner's
//     top-3 even without intent set; the score mix is off.
//
// Aggregation is a pure function: callers supply Samples and
// optional thresholds. Like sizecalibration, this package does not
// own data fetching — the CLI accepts a JSON snapshot so the loop
// can run offline against curated samples until the live retro
// pipeline wires in.
//
// Auto-mode rows are excluded from the aggregate — the planner
// dispatched without operator input there, so an "override" doesn't
// mean anything. Callers tag the Mode field on each Sample.
package reccalibration
