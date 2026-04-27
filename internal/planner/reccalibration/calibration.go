// Recommendation calibration aggregation (gm-v5z2.8a, §7.5).

package reccalibration

import (
	"fmt"
	"sort"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// ModeCoach marks a sample as a coach-mode pick (operator chose
// the bead). Only coach-mode samples participate in the aggregate
// — auto-dispatched rows have no operator override signal and are
// silently filtered.
const (
	ModeCoach = "coach"
	ModeAuto  = "auto"
)

// Components is the per-component score snapshot the calibration
// loop reads. Mirrors selection.Components but kept in a local
// shape so the package doesn't pull in the selection layer (and so
// the wire format is stable independent of selection refactors).
type Components struct {
	Affinity       float64 `json:"affinity"`
	Leverage       float64 `json:"leverage"`
	LeverageWeight int     `json:"leverage_weight"`
	EpicAffinity   float64 `json:"epic_affinity"`
	RunwayDemoted  bool    `json:"runway_demoted,omitempty"`
	IntentDemoted  bool    `json:"intent_demoted,omitempty"`
}

// Sample is one coach-mode pick. RecommendedTopBead is the bead
// Selection ranked first; PickedBead is the bead the operator
// actually took. PickedRank is 1-based within the dispatchable
// candidate set as Selection ordered it (1 = took the
// recommendation; 2-3 = top-3; 4+ = outside top-3; 0 = picked bead
// wasn't in the dispatchable set, e.g. a manual claim outside the
// recommended slice — treated as outside top-3 by the aggregator).
//
// IntentSet records whether session intent was active at pick
// time; the intent-miss aggregator splits samples on this. Mode
// must be "coach" for the row to count; "auto" rows are filtered.
type Sample struct {
	BeadID                core.WorkItemID `json:"bead_id,omitempty"`
	At                    time.Time       `json:"at,omitempty"`
	Mode                  string          `json:"mode"`
	SessionID             string          `json:"session_id,omitempty"`
	RecommendedTopBead    core.WorkItemID `json:"recommended_top_bead"`
	RecommendedTopScore   float64         `json:"recommended_top_score"`
	PickedBead            core.WorkItemID `json:"picked_bead"`
	PickedScore           float64         `json:"picked_score"`
	PickedRank            int             `json:"picked_rank"`
	IntentSet             bool            `json:"intent_set,omitempty"`
	OperatorReason        string          `json:"operator_reason,omitempty"`
	RecommendedComponents *Components     `json:"recommended_components,omitempty"`
	PickedComponents      *Components     `json:"picked_components,omitempty"`
}

// Override reports whether the operator picked something other
// than the planner's top recommendation. The aggregate's primary
// gate.
func (s Sample) Override() bool {
	return s.PickedBead != "" && s.PickedBead != s.RecommendedTopBead
}

// Thresholds drives when the aggregator promotes a metric from
// "tracked" to "systematic" (a finding worth surfacing). Defaults
// match the bead notes: 10-sample floor, 30% override rate, 0.20
// mean score-delta. Per-rig overrides land later via the operator-
// review queue; today this is per-call.
type Thresholds struct {
	// MinSamples is the floor on coach-mode sample count. Below
	// this the aggregator returns Findings with all flags false
	// and SampleCount populated — never flag systematic patterns
	// on noise.
	MinSamples int `json:"min_samples"`
	// OverrideRate is the fraction of coach-mode picks that
	// disagreed with the planner above which we consider the
	// override pattern "systematic". Defaults to 0.30.
	OverrideRate float64 `json:"override_rate"`
	// MeanScoreDelta is the mean (recommended_score - picked_score)
	// across overrides; rises only when the planner is "very
	// confident wrong" rather than "marginally wrong." Above this
	// the override-pattern flags promote.
	MeanScoreDelta float64 `json:"mean_score_delta"`
	// Top3Rate is the fraction of overrides where the operator
	// picked outside the planner's top-3. The OverrideTop3 finding
	// flips when this clears the threshold. Defaults to 0.30.
	Top3Rate float64 `json:"top3_rate"`
	// IntentMissRate / LeverageMissRate / RunwayOvertrustRate use
	// the same shape — fraction of qualifying overrides above which
	// the corresponding finding flips. Default 0.30 each.
	IntentMissRate      float64 `json:"intent_miss_rate"`
	LeverageMissRate    float64 `json:"leverage_miss_rate"`
	RunwayOvertrustRate float64 `json:"runway_overtrust_rate"`
}

// DefaultThresholds is the v1 calibration anchor. Per-rig tuning
// arrives once the operator-review queue (gm-s47n.7.3) consumes
// these findings.
var DefaultThresholds = Thresholds{
	MinSamples:          10,
	OverrideRate:        0.30,
	MeanScoreDelta:      0.20,
	Top3Rate:            0.30,
	IntentMissRate:      0.30,
	LeverageMissRate:    0.30,
	RunwayOvertrustRate: 0.30,
}

// Findings is the aggregation result. Every rate is populated even
// when the corresponding boolean is false, so callers can render a
// "tracking, not flagged" view alongside the systematic flags.
type Findings struct {
	SampleCount   int     `json:"sample_count"`
	OverrideCount int     `json:"override_count"`
	OverrideRate  float64 `json:"override_rate"`
	// MeanScoreDelta is the average (recommended_score -
	// picked_score) across overrides. Positive means the planner
	// was confident-wrong; near zero means the planner was
	// marginally-wrong (which is much weaker signal).
	MeanScoreDelta float64 `json:"mean_score_delta"`

	// Top-level "the planner is systematically off" flags. These
	// promote together when OverrideRate AND MeanScoreDelta both
	// clear their thresholds and OverrideTop3Rate is high enough.
	OverrideTop3     bool    `json:"override_top_3"`
	OverrideTop3Rate float64 `json:"override_top_3_rate"`

	IntentSampleCount    int     `json:"intent_sample_count"`
	SystematicIntentMiss bool    `json:"systematic_intent_miss"`
	IntentMissRate       float64 `json:"intent_miss_rate"`

	SystematicLeverageMiss bool    `json:"systematic_leverage_miss"`
	LeverageMissRate       float64 `json:"leverage_miss_rate"`

	SystematicRunwayOvertrust bool    `json:"systematic_runway_overtrust"`
	RunwayOvertrustRate       float64 `json:"runway_overtrust_rate"`
}

// AnyFlag reports whether any systematic pattern flipped.
func (f Findings) AnyFlag() bool {
	return f.OverrideTop3 ||
		f.SystematicIntentMiss ||
		f.SystematicLeverageMiss ||
		f.SystematicRunwayOvertrust
}

// Suggestion bundles the findings, the resolved thresholds, and
// human-readable notes the CLI prints. Notes carry the *why* —
// "intent miss rate 0.42 > 0.30 over 14 intent-set picks" — so the
// operator-review queue can reproduce the math.
type Suggestion struct {
	Findings   Findings   `json:"findings"`
	Thresholds Thresholds `json:"thresholds"`
	Notes      []string   `json:"notes,omitempty"`
	// Mode names which sample mode was aggregated (always "coach"
	// today; reserved for "auto" backfills if a future loop wants
	// them).
	Mode string `json:"mode"`
}

// Aggregate is the pure entry point. Filters auto-mode samples,
// computes per-signal rates, and flips Findings flags when rates
// clear thresholds AND the floor (MinSamples + MeanScoreDelta)
// holds.
//
// thresholds == nil → DefaultThresholds.
//
// Samples with empty RecommendedTopBead or PickedBead are dropped
// (caller's responsibility; we silently filter rather than error).
func Aggregate(samples []Sample, thresholds *Thresholds) Suggestion {
	t := DefaultThresholds
	if thresholds != nil {
		t = mergeThresholds(t, *thresholds)
	}

	coach := filterCoach(samples)
	overrides := filterOverrides(coach)
	intentSet := filterIntent(coach)
	intentOverrides := filterOverrides(intentSet)

	// Sort by At for stable iteration in tests / human output.
	sort.SliceStable(coach, func(i, j int) bool { return coach[i].At.Before(coach[j].At) })

	f := Findings{
		SampleCount:       len(coach),
		OverrideCount:     len(overrides),
		IntentSampleCount: len(intentSet),
	}
	if len(coach) == 0 {
		return Suggestion{
			Findings:   f,
			Thresholds: t,
			Mode:       ModeCoach,
			Notes:      []string{"no coach-mode samples — calibration is a no-op"},
		}
	}

	f.OverrideRate = ratio(len(overrides), len(coach))
	f.MeanScoreDelta = meanScoreDelta(overrides)
	f.OverrideTop3Rate = ratio(countOutsideTop3(overrides), len(overrides))
	f.IntentMissRate = ratio(countOutsideTop3(intentOverrides), len(intentSet))
	f.LeverageMissRate = ratio(countLeverageMiss(overrides), len(overrides))
	f.RunwayOvertrustRate = ratio(countRunwayOvertrust(overrides), len(overrides))

	notes := make([]string, 0, 4)
	if len(coach) < t.MinSamples {
		notes = append(notes, "insufficient samples — below MinSamples floor; flags suppressed")
		return Suggestion{Findings: f, Thresholds: t, Mode: ModeCoach, Notes: notes}
	}

	// Override-rate must clear AND mean delta must clear before
	// any "the planner is systematically off" flag flips. Either
	// alone is too noisy — a 30% override at 0.05 mean delta is
	// just operator preference at the margin.
	overrideRateOK := f.OverrideRate >= t.OverrideRate && f.MeanScoreDelta >= t.MeanScoreDelta

	if overrideRateOK && f.OverrideTop3Rate >= t.Top3Rate {
		f.OverrideTop3 = true
		notes = append(notes,
			fmtNote("override_top_3: %.0f%% of overrides picked outside the planner's top-3 (rate %.2f, mean Δ %.2f)",
				f.OverrideTop3Rate*100, f.OverrideRate, f.MeanScoreDelta))
	}

	// Intent miss is its own gate — the IntentSet sub-population
	// has its own MinSamples-style floor (use the same MinSamples
	// for v1).
	if f.IntentSampleCount >= t.MinSamples && f.IntentMissRate >= t.IntentMissRate {
		f.SystematicIntentMiss = true
		notes = append(notes,
			fmtNote("systematic_intent_miss: %.0f%% of intent-set picks landed outside the planner's top-3 (over %d intent samples)",
				f.IntentMissRate*100, f.IntentSampleCount))
	}

	if overrideRateOK && f.LeverageMissRate >= t.LeverageMissRate {
		f.SystematicLeverageMiss = true
		notes = append(notes,
			fmtNote("systematic_leverage_miss: %.0f%% of overrides picked beads with higher blocks-weight than the recommendation",
				f.LeverageMissRate*100))
	}

	if overrideRateOK && f.RunwayOvertrustRate >= t.RunwayOvertrustRate {
		f.SystematicRunwayOvertrust = true
		notes = append(notes,
			fmtNote("systematic_runway_overtrust: %.0f%% of overrides took beads the runway gate had demoted",
				f.RunwayOvertrustRate*100))
	}

	if !f.AnyFlag() && len(notes) == 0 {
		notes = append(notes, "no systematic recommendation drift detected")
	}
	return Suggestion{Findings: f, Thresholds: t, Mode: ModeCoach, Notes: notes}
}

func filterCoach(in []Sample) []Sample {
	out := make([]Sample, 0, len(in))
	for _, s := range in {
		if s.Mode != ModeCoach {
			continue
		}
		if s.RecommendedTopBead == "" || s.PickedBead == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func filterOverrides(in []Sample) []Sample {
	out := make([]Sample, 0, len(in))
	for _, s := range in {
		if s.Override() {
			out = append(out, s)
		}
	}
	return out
}

func filterIntent(in []Sample) []Sample {
	out := make([]Sample, 0, len(in))
	for _, s := range in {
		if s.IntentSet {
			out = append(out, s)
		}
	}
	return out
}

// countOutsideTop3 includes PickedRank == 0 as "outside top-3" —
// the operator picked something Selection didn't even surface.
// PickedRank == 1 means took-the-recommendation and is filtered
// out by the override step before reaching here.
func countOutsideTop3(overrides []Sample) int {
	n := 0
	for _, s := range overrides {
		if s.PickedRank == 0 || s.PickedRank > 3 {
			n++
		}
	}
	return n
}

func countLeverageMiss(overrides []Sample) int {
	n := 0
	for _, s := range overrides {
		if s.PickedComponents == nil || s.RecommendedComponents == nil {
			continue
		}
		if s.PickedComponents.LeverageWeight > s.RecommendedComponents.LeverageWeight {
			n++
		}
	}
	return n
}

// countRunwayOvertrust spots overrides where the recommendation
// was runway-demoted but the operator picked a bead that wasn't.
// That's "operator overrode the runway gate's pessimism" — and
// when it lands fine often enough the runway estimator is too
// conservative.
//
// We only count overrides where component snapshots are present;
// missing snapshots are silently skipped (insufficient signal
// rather than counting them as zero).
func countRunwayOvertrust(overrides []Sample) int {
	n := 0
	for _, s := range overrides {
		if s.PickedComponents == nil || s.RecommendedComponents == nil {
			continue
		}
		if s.RecommendedComponents.RunwayDemoted && !s.PickedComponents.RunwayDemoted {
			n++
		}
	}
	return n
}

func meanScoreDelta(overrides []Sample) float64 {
	if len(overrides) == 0 {
		return 0
	}
	var sum float64
	for _, s := range overrides {
		sum += s.RecommendedTopScore - s.PickedScore
	}
	return sum / float64(len(overrides))
}

func ratio(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom)
}

func mergeThresholds(base, in Thresholds) Thresholds {
	if in.MinSamples > 0 {
		base.MinSamples = in.MinSamples
	}
	if in.OverrideRate > 0 {
		base.OverrideRate = in.OverrideRate
	}
	if in.MeanScoreDelta > 0 {
		base.MeanScoreDelta = in.MeanScoreDelta
	}
	if in.Top3Rate > 0 {
		base.Top3Rate = in.Top3Rate
	}
	if in.IntentMissRate > 0 {
		base.IntentMissRate = in.IntentMissRate
	}
	if in.LeverageMissRate > 0 {
		base.LeverageMissRate = in.LeverageMissRate
	}
	if in.RunwayOvertrustRate > 0 {
		base.RunwayOvertrustRate = in.RunwayOvertrustRate
	}
	return base
}

// fmtNote is a thin wrapper so the call sites read uniformly. Kept
// here rather than in fmt-laden code so a future caller can replace
// it with a structured-event emitter without rewriting the gates.
func fmtNote(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
