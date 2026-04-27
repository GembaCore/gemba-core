// Session runway estimator (gm-v5z2.4). Pure function over
// SessionHealth + a per-session calibration scalar.
//
// Spec recipe (work-planning.md §4 Layer 5.1):
//
//   1. Start with (1 - context_pressure) as the upper-bound runway
//      in "session lifetimes."
//   2. Subtract a concept_drift penalty (drift wastes runway: a
//      session that has wandered topics will slow down on a
//      complex bead).
//   3. Multiply by a per-session calibration scalar from recent
//      promised-vs-actual cycles. A session that overruns by 2x
//      gets a 0.5 scalar.
//
// The output bucket is comparable with EstimatedSize on a bead so
// Selection's runway gate is a one-line check: bead.size > runway →
// demote.

package runway

import (
	"math"

	"github.com/MikeBengtson/gemba/internal/enrichment"
	"github.com/MikeBengtson/gemba/internal/planner"
)

// DefaultDriftPenaltyWeight is the multiplier applied to
// concept_drift before subtracting it from the headroom term. A
// drift of 0.5 with the default weight removes 0.25 from runway.
const DefaultDriftPenaltyWeight = 0.5

// DefaultCalibration is the per-session calibration scalar a fresh
// session starts with — no recent promised-vs-actual data, treat
// as on-track. Operators / the calibration loop (gm-v5z2.8) tune
// this per session as evidence accumulates.
const DefaultCalibration = 1.0

// Runway is the snapshot the estimator emits. Bucket maps onto
// enrichment.EstimatedSize so Selection compares apples-to-apples
// against bead.estimated_size. Score is the raw [0, 1] number the
// bucket was derived from; surfaced so the operator-facing CLI can
// show "small (0.18)" instead of just "small."
type Runway struct {
	Bucket  enrichment.EstimatedSize `json:"bucket"`
	Score   float64                  `json:"score"`
	Drivers Drivers                  `json:"drivers"`
}

// Drivers is the operator-readable breakdown — the three numbers
// that fed the score. Renders the recipe in the CLI output so an
// operator can see WHY runway is what it is, not just the bucket.
type Drivers struct {
	ContextPressure float64 `json:"context_pressure"`
	ConceptDrift    float64 `json:"concept_drift"`
	Calibration     float64 `json:"calibration"`
	Headroom        float64 `json:"headroom"`         // 1 - pressure
	DriftPenalty    float64 `json:"drift_penalty"`    // weight * drift
}

// Inputs bundles everything Estimate needs. Health is required
// (the runway recipe is a function of the three health numbers);
// Calibration is optional — leave zero for DefaultCalibration.
//
// PressureCeiling defaults to 1.0 and is the saturation point for
// the headroom term — left configurable so the calibration loop
// (gm-v5z2.8) can tune the upper bound per rig if a model with
// huge context becomes the norm. Most operators leave it alone.
//
// DriftPenaltyWeight defaults to DefaultDriftPenaltyWeight; same
// rationale.
type Inputs struct {
	Health             *planner.SessionHealth
	Calibration        float64
	PressureCeiling    float64
	DriftPenaltyWeight float64
}

// Bucket boundaries on the [0, 1] runway score. A score below
// SmallMax is small, below MediumMax is medium, otherwise large.
// Mirrors the structure of enrichment.SizeHeuristicThresholds so
// future per-rig tuning can swap both knobs from one config block.
type BucketThresholds struct {
	SmallMax  float64
	MediumMax float64
}

// DefaultBucketThresholds — chosen so a healthy session
// (pressure 0.2, drift 0.1, calibration 1.0) lands in "large"
// (~0.75) and a stressed session (pressure 0.8, drift 0.6,
// calibration 0.5) lands in "small" (~0.05).
var DefaultBucketThresholds = BucketThresholds{
	SmallMax:  0.30,
	MediumMax: 0.65,
}

// Estimate runs the spec recipe and returns the snapshot.
//
// nil Health (or a Health with no telemetry) yields the safest
// answer: small bucket with score=0. The selection gate then
// treats the session as too constrained for anything but small
// beads — biased toward not over-stuffing a session we can't
// observe.
func Estimate(in Inputs) Runway {
	return EstimateWithThresholds(in, DefaultBucketThresholds)
}

// EstimateWithThresholds is Estimate with caller-controlled
// bucket boundaries. Used by tests and by future per-rig tuning.
func EstimateWithThresholds(in Inputs, t BucketThresholds) Runway {
	cal := in.Calibration
	if cal <= 0 {
		cal = DefaultCalibration
	}
	ceil := in.PressureCeiling
	if ceil <= 0 {
		ceil = 1.0
	}
	dw := in.DriftPenaltyWeight
	if dw <= 0 {
		dw = DefaultDriftPenaltyWeight
	}

	out := Runway{
		Drivers: Drivers{Calibration: cal},
		Bucket:  enrichment.SizeSmall,
	}
	if in.Health == nil {
		return out
	}

	pressure := clamp01(in.Health.ContextPressure)
	drift := clamp01(in.Health.ConceptDrift)
	headroom := math.Max(0, ceil-pressure)
	if headroom > 1 {
		headroom = 1
	}
	driftPenalty := dw * drift

	score := (headroom - driftPenalty) * cal
	score = clamp01(score)

	out.Score = score
	out.Drivers.ContextPressure = pressure
	out.Drivers.ConceptDrift = drift
	out.Drivers.Headroom = headroom
	out.Drivers.DriftPenalty = driftPenalty
	out.Bucket = bucketFor(score, t)
	return out
}

// FitsBead reports whether a bead of the given size fits this
// runway — selection's runway gate. Uses Rank() so the comparison
// is total: small bead always fits, large bead only fits a large
// runway. An unknown bead size (Rank()==0, e.g. legacy beads with
// no estimated_size) is treated as fitting — selection should not
// demote beads that haven't been estimated yet.
func (r Runway) FitsBead(beadSize enrichment.EstimatedSize) bool {
	br := beadSize.Rank()
	if br == 0 {
		return true
	}
	return br <= r.Bucket.Rank()
}

func bucketFor(score float64, t BucketThresholds) enrichment.EstimatedSize {
	switch {
	case score < t.SmallMax:
		return enrichment.SizeSmall
	case score < t.MediumMax:
		return enrichment.SizeMedium
	default:
		return enrichment.SizeLarge
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
