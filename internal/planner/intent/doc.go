// Package intent implements the operator-pinned session focus
// directive (gm-v5z2.3, work-planning.md §4 Layer 1.3).
//
// An Intent is a small struct attached to a session that biases
// candidate selection toward a particular slice of work. Three
// orthogonal restrictors:
//
//   - EpicID: only candidates descended from this epic match.
//   - Label: only candidates carrying this bd label match.
//   - BeadIDRegex: only candidates whose id matches this regex match.
//
// Multiple restrictors AND together. At least one MUST be set for
// the intent to be valid; an empty Intent (no restrictors) is the
// "cleared" state and Match returns true for every candidate.
//
// Selection (gm-v5z2.7) reads ctx.intent and demotes out-of-intent
// candidates by Intent.DemotionFactor (default 0.4). The rule is
// SOFT: a P0 bead outside intent can still beat a P3 bead inside
// intent if the score gap is wide enough.
//
// Storage and the CLI surface (`gemba session focus`) live in
// internal/planner/intent/store.go and internal/cli/session_focus.go.
package intent
