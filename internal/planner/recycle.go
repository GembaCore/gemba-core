// Auto-recycle decision (gm-s47n.6.6). Pure function; spec §5.5.
//
// Recycle the session BEFORE taking a new bead when ANY of three
// conditions hold:
//
//   1. context_pressure > 0.85 AND incoming bead's affinity is below
//      the median for ready beads.
//      "The session isn't perfectly primed for this bead anyway,
//      so the cold-start cost is real."
//
//   2. concept_drift > 0.7 AND incoming bead shares < 0.3 concept
//      overlap with session lifetime.
//      "The session has wandered topics; this bead is on yet
//      another topic — recycle so the next agent starts clean."
//
//   3. time_on_task > 4h AND incoming bead is the start of a new
//      concept area.
//      "Long-running sessions accumulate scaffolding the next
//      concept area doesn't benefit from. The handoff is cheaper
//      than an extra hour of context drag."
//
// Never recycle mid-bead. The auto-dispatch loop calls
// ShouldRecycle at the boundary between completing one bead and
// accepting the next; the decision controls whether the planner
// dispatches the next bead to this session or to a freshly handed-
// off one.

package planner

import "sort"

// Recycle thresholds — spec §5.5. Constants so callers + tests
// reference the canonical values without re-typing them.
const (
	RecyclePressureThreshold         = 0.85
	RecycleDriftThreshold            = 0.70
	RecycleConceptOverlapMaxForDrift = 0.30
	RecycleTimeOnTaskHours           = 4
)

// RecycleReason is the structured why behind a recycle decision.
// One value per spec rule plus "ok" when no rule fires.
//
// Stable wire shape: emitted to logs + the dispatch decision
// audit trail (gm-s47n.6.2). Don't rename without bumping the
// audit-log version.
type RecycleReason string

const (
	RecycleOK                  RecycleReason = "ok"
	RecyclePressureBelowMedian RecycleReason = "context_pressure_high_affinity_below_median"
	RecycleDriftAndMismatch    RecycleReason = "concept_drift_high_overlap_low"
	RecycleTimeOnTaskNewArea   RecycleReason = "time_on_task_new_concept_area"
)

// RecycleDecision is the pure-function output. Action is the
// operator-facing answer ("recycle" / "keep"); Reason names which
// rule fired (or "ok" when none did). DriverScores carries the
// numbers behind the call so coach-mode UI / audit logs can show
// the operator exactly why the planner chose what it chose.
type RecycleDecision struct {
	Recycle      bool           `json:"recycle"`
	Reason       RecycleReason  `json:"reason"`
	DriverScores RecycleDrivers `json:"drivers"`
}

// RecycleDrivers is the numeric snapshot used for the decision —
// kept on the result struct so the operator-facing surface can
// render "pressure 0.91 (rule fires when > 0.85)" without
// recomputing.
type RecycleDrivers struct {
	ContextPressure  float64 `json:"context_pressure"`
	ConceptDrift     float64 `json:"concept_drift"`
	TimeOnTaskHours  float64 `json:"time_on_task_hours"`
	IncomingAffinity float64 `json:"incoming_affinity"`
	MedianAffinity   float64 `json:"median_affinity"`
	ConceptOverlap   float64 `json:"concept_overlap"`
}

// RecycleInputs bundles everything ShouldRecycle needs. Each
// field is optional; missing data leans toward "keep the session"
// — the planner should never recycle on incomplete signal because
// the cold-start cost is real and the worst observable outcome is
// just a slightly-warm session taking a slightly-mismatched bead.
type RecycleInputs struct {
	// Health is the SessionHealth snapshot from gm-s47n.5.1. nil
	// → all driver thresholds skip.
	Health *SessionHealth

	// IncomingBead is the candidate bead the planner is about to
	// dispatch against this session. Empty Concepts means rule 2
	// can't compute the overlap; rule 3 falls back to "this is
	// the start of a new area" if the session profile has any
	// concepts at all (the bead's empty list cannot intersect).
	IncomingBead AffinityBeadInputs

	// IncomingAffinityScores are the precomputed Affinity scores
	// for (IncomingBead, this session). Saves the caller the
	// recompute when ShouldRecycle runs alongside the affinity
	// pass. Combined drives rule 1.
	IncomingAffinityScores AffinityScores

	// ReadySetAffinities is the combined-affinity score of every
	// ready bead against this session. Used to compute the median
	// for rule 1. Empty slice → rule 1 cannot fire.
	ReadySetAffinities []float64

	// SessionConcepts is the lifetime concept profile. Used to
	// compute the new-area / overlap drivers for rules 2 + 3.
	// nil → rules 2 and 3 fall back to the safest answer
	// (no concepts known, so the bead is "all new" — defended
	// below in evaluateConceptOverlap).
	SessionConcepts map[ConceptTag]float64
}

// ShouldRecycle returns the structured decision for the given
// inputs. Pure: same inputs always produce the same output. Safe
// for concurrent use.
//
// The function never panics on missing data — it leans toward
// "keep the session" whenever a driver is unavailable, since the
// recycle decision discards a real warm context. Operators tune
// thresholds via per-rig settings; the v1 constants live in this
// file and match spec §5.5.
func ShouldRecycle(in RecycleInputs) RecycleDecision {
	out := RecycleDecision{Reason: RecycleOK}
	out.DriverScores = computeDrivers(in)

	// Rule 1: high pressure + below-median affinity.
	if in.Health != nil && in.Health.ContextPressure > RecyclePressureThreshold {
		median, ok := medianAffinity(in.ReadySetAffinities)
		if ok && in.IncomingAffinityScores.Combined < median {
			out.Recycle = true
			out.Reason = RecyclePressureBelowMedian
			return out
		}
	}

	// Rule 2: high drift + low overlap.
	if in.Health != nil && in.Health.ConceptDrift > RecycleDriftThreshold {
		overlap := out.DriverScores.ConceptOverlap
		if overlap < RecycleConceptOverlapMaxForDrift {
			out.Recycle = true
			out.Reason = RecycleDriftAndMismatch
			return out
		}
	}

	// Rule 3: long time-on-task + new concept area. "New area"
	// means the bead has any concept the session has never seen.
	if in.Health != nil && out.DriverScores.TimeOnTaskHours > RecycleTimeOnTaskHours {
		if isNewConceptArea(in.IncomingBead.Concepts, in.SessionConcepts) {
			out.Recycle = true
			out.Reason = RecycleTimeOnTaskNewArea
			return out
		}
	}

	return out
}

// computeDrivers fills in the numeric snapshot. Pulled out so the
// rule body reads as the spec text — predicates only.
func computeDrivers(in RecycleInputs) RecycleDrivers {
	d := RecycleDrivers{
		IncomingAffinity: in.IncomingAffinityScores.Combined,
	}
	if in.Health != nil {
		d.ContextPressure = in.Health.ContextPressure
		d.ConceptDrift = in.Health.ConceptDrift
		d.TimeOnTaskHours = in.Health.TimeOnTask.Hours()
	}
	if median, ok := medianAffinity(in.ReadySetAffinities); ok {
		d.MedianAffinity = median
	}
	d.ConceptOverlap = evaluateConceptOverlap(in.IncomingBead.Concepts, in.SessionConcepts)
	return d
}

// medianAffinity returns the median of values + ok=false when the
// slice is empty. Sort copies the input so the caller's slice is
// never mutated.
func medianAffinity(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2], true
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2, true
}

// evaluateConceptOverlap returns the bead's coverage of the
// session's lifetime concepts. Range [0, 1]:
//
//	0 — none of the bead's concepts appear in the profile
//	1 — every bead concept appears in the profile
//
// Empty bead concepts return 0 (the recycle rules treat that as
// "no overlap signal" → the more conservative answer for rule 2,
// which fires when overlap < threshold).
//
// Empty profile concepts also return 0 (a fresh session has no
// lifetime concepts to overlap; rule 2's drift threshold is what
// gates this case anyway — a session with concept_drift > 0.7
// must by definition have some history to drift from).
func evaluateConceptOverlap(beadConcepts []ConceptTag, sessionConcepts map[ConceptTag]float64) float64 {
	if len(beadConcepts) == 0 || len(sessionConcepts) == 0 {
		return 0
	}
	hits := 0
	for _, t := range beadConcepts {
		if _, ok := sessionConcepts[t]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(beadConcepts))
}

// isNewConceptArea returns true when at least one bead concept is
// not in the session profile — i.e. the bead opens a new area for
// this session. Empty bead concepts return false: nothing to
// compare, so don't fire rule 3.
//
// Empty session profile + non-empty bead concepts returns true:
// every bead concept is "new" to a session that has never recorded
// a concept. The 4h time-on-task threshold is what gates this case
// — a fresh session almost certainly hasn't been on task that long.
func isNewConceptArea(beadConcepts []ConceptTag, sessionConcepts map[ConceptTag]float64) bool {
	if len(beadConcepts) == 0 {
		return false
	}
	for _, t := range beadConcepts {
		if _, ok := sessionConcepts[t]; !ok {
			return true
		}
	}
	return false
}
