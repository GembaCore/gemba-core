// Bead-size calibration aggregation (gm-v5z2.8b, §7.6).

package sizecalibration

import (
	"sort"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/enrichment"
)

// Sample is one closed bead's predicted-vs-actual pair. Predicted
// is the bucket the heuristic assigned at creation time; Actual is
// the wall-clock duration from start to close. Author is optional
// — the per-author calibration in §7.6 reads it when enough
// signal exists; per-rig aggregation ignores it.
type Sample struct {
	BeadID    core.WorkItemID
	Predicted enrichment.EstimatedSize
	Actual    time.Duration
	Author    string
}

// BucketTargets is the expected wall-clock time-to-close per
// bucket. Drift > 2x in either direction triggers a suggestion to
// shift the heuristic's score thresholds. Override via per-rig
// settings once the calibration loop's review path lands.
type BucketTargets struct {
	Small  time.Duration
	Medium time.Duration
	Large  time.Duration
}

// DefaultBucketTargets is the v1 calibration anchor. Numbers chosen
// from the rough rule-of-thumb every coach maintains in their head:
//   - small ≈ a 30-minute fix
//   - medium ≈ a 4-hour stint
//   - large ≈ a full day's work
//
// These are wall-clock — clock starts at session claim, stops at
// close — so distractions and parallel tasks inflate the actual.
// The retro filters out interrupted runs upstream; the aggregator
// just receives "good" samples.
var DefaultBucketTargets = BucketTargets{
	Small:  30 * time.Minute,
	Medium: 4 * time.Hour,
	Large:  24 * time.Hour,
}

// MinSamplesPerBucket is the floor for trusting a median. Below
// this we report "insufficient signal" rather than nudging
// thresholds on a single bead's noise.
const MinSamplesPerBucket = 5

// DriftFactor is the trigger threshold. A bucket whose median
// actual duration is outside [target / DriftFactor, target *
// DriftFactor] is flagged. Spec calls this "> 2x in either
// direction"; we expose it as a constant so tests can use a
// tighter factor for deterministic small-sample fixtures.
const DriftFactor = 2.0

// BucketReport summarises one size bucket's calibration result.
// Direction is one of {"low", "high", "ok", "insufficient"}; the
// SuggestedThresholdShift is in the same units as
// SizeHeuristicThresholds (description-length × DoD-line-count
// score).
type BucketReport struct {
	Bucket                  enrichment.EstimatedSize `json:"bucket"`
	SampleCount             int                      `json:"sample_count"`
	MedianActualSeconds     float64                  `json:"median_actual_seconds"`
	TargetSeconds           float64                  `json:"target_seconds"`
	Direction               string                   `json:"direction"`
	SuggestedThresholdShift int                      `json:"suggested_threshold_shift,omitempty"`
	Reason                  string                   `json:"reason,omitempty"`
}

// Suggestion is the aggregator output. Per-bucket reports plus the
// resulting recommended thresholds. NewThresholds == nil when no
// bucket drifted enough to trigger a shift; callers compare with
// the current thresholds to decide whether to surface a review
// suggestion.
type Suggestion struct {
	Reports       []BucketReport                      `json:"reports"`
	NewThresholds *enrichment.SizeHeuristicThresholds `json:"new_thresholds,omitempty"`
	Current       enrichment.SizeHeuristicThresholds  `json:"current_thresholds"`
	Targets       BucketTargets                       `json:"targets"`
}

// AnyDrift reports whether any bucket has DriftDirection != "ok"
// or "insufficient". Used by the CLI to decide between "looks
// good" and "review suggested" in human output.
func (s Suggestion) AnyDrift() bool {
	for _, r := range s.Reports {
		if r.Direction == "low" || r.Direction == "high" {
			return true
		}
	}
	return false
}

// Aggregate is the pure entry point. Groups samples by bucket,
// computes median actual duration, compares against targets, and
// — when drift exceeds DriftFactor in either direction — emits a
// suggested threshold shift. Returns a Suggestion regardless;
// callers branch on AnyDrift to decide whether to surface.
//
// targets == nil → DefaultBucketTargets.
// current == zero → enrichment.DefaultSizeThresholds.
//
// Samples with empty Predicted or non-positive Actual are dropped.
// The caller's responsibility to feed clean samples; we silently
// filter rather than error so a single bad row in the retrospective
// stream doesn't tank the whole calibration run.
func Aggregate(samples []Sample, targets *BucketTargets, current *enrichment.SizeHeuristicThresholds) Suggestion {
	t := DefaultBucketTargets
	if targets != nil {
		t = *targets
	}
	c := enrichment.DefaultSizeThresholds
	if current != nil {
		c = *current
	}

	byBucket := map[enrichment.EstimatedSize][]time.Duration{
		enrichment.SizeSmall:  nil,
		enrichment.SizeMedium: nil,
		enrichment.SizeLarge:  nil,
	}
	for _, s := range samples {
		if s.Actual <= 0 {
			continue
		}
		if !s.Predicted.IsValid() {
			continue
		}
		byBucket[s.Predicted] = append(byBucket[s.Predicted], s.Actual)
	}

	reports := make([]BucketReport, 0, 3)
	// Stable order: small → medium → large, matching Rank().
	order := []enrichment.EstimatedSize{enrichment.SizeSmall, enrichment.SizeMedium, enrichment.SizeLarge}
	totalShift := 0
	for _, b := range order {
		report := buildBucketReport(b, byBucket[b], targetFor(b, t))
		// A drift in a smaller bucket bumps the threshold for the
		// boundary just above it (SmallMax / MediumMax). We
		// accumulate per-bucket shifts and apply them as the
		// suggested NewThresholds at the end.
		totalShift += report.SuggestedThresholdShift
		reports = append(reports, report)
	}

	out := Suggestion{
		Reports: reports,
		Current: c,
		Targets: t,
	}
	if anyDriftIn(reports) {
		newT := suggestThresholds(c, reports)
		out.NewThresholds = &newT
	}
	_ = totalShift // reserved for richer per-author shifts in a future slice
	return out
}

func anyDriftIn(reports []BucketReport) bool {
	for _, r := range reports {
		if r.Direction == "low" || r.Direction == "high" {
			return true
		}
	}
	return false
}

func targetFor(b enrichment.EstimatedSize, t BucketTargets) time.Duration {
	switch b {
	case enrichment.SizeSmall:
		return t.Small
	case enrichment.SizeMedium:
		return t.Medium
	case enrichment.SizeLarge:
		return t.Large
	}
	return 0
}

func buildBucketReport(b enrichment.EstimatedSize, samples []time.Duration, target time.Duration) BucketReport {
	r := BucketReport{
		Bucket:        b,
		SampleCount:   len(samples),
		TargetSeconds: target.Seconds(),
	}
	if len(samples) < MinSamplesPerBucket {
		r.Direction = "insufficient"
		r.Reason = "not enough samples for this bucket"
		return r
	}
	med := median(samples)
	r.MedianActualSeconds = med.Seconds()
	if target <= 0 {
		r.Direction = "ok"
		return r
	}
	ratio := float64(med) / float64(target)
	switch {
	case ratio > DriftFactor:
		// Median is more than DriftFactor × target → bucket
		// under-estimates (real beads in this bucket take much
		// longer than the heuristic predicts). Lower the threshold
		// for the bucket boundary above so similar beads get
		// promoted to the next size up.
		r.Direction = "high"
		r.SuggestedThresholdShift = -shiftMagnitude(ratio)
		r.Reason = "median exceeds target by > 2× — heuristic under-estimates this bucket"
	case ratio < 1.0/DriftFactor:
		// Median is less than half the target → bucket over-
		// estimates. Raise the threshold above so beads stay in
		// the smaller bucket longer.
		r.Direction = "low"
		r.SuggestedThresholdShift = shiftMagnitude(1.0 / ratio)
		r.Reason = "median falls below half target — heuristic over-estimates this bucket"
	default:
		r.Direction = "ok"
	}
	return r
}

// shiftMagnitude scales the threshold delta by how far past the
// drift factor the ratio sits. A 2.5× ratio nudges by 200; a 4×
// ratio nudges by 800. Conservative growth so a single calibration
// run can't blow the thresholds way out.
func shiftMagnitude(ratio float64) int {
	excess := ratio - DriftFactor
	if excess < 0 {
		excess = -excess
	}
	// Linear-ish: 200 units per excess factor.
	return int(200 * (1 + excess))
}

func suggestThresholds(current enrichment.SizeHeuristicThresholds, reports []BucketReport) enrichment.SizeHeuristicThresholds {
	out := current
	for _, r := range reports {
		switch r.Bucket {
		case enrichment.SizeSmall:
			// Small bucket drift adjusts SmallMax (the boundary
			// between small and medium).
			out.SmallMax = applyShift(out.SmallMax, r.SuggestedThresholdShift)
		case enrichment.SizeMedium:
			// Medium bucket drift adjusts MediumMax.
			out.MediumMax = applyShift(out.MediumMax, r.SuggestedThresholdShift)
		case enrichment.SizeLarge:
			// Large is open-ended — drift here flags a missing
			// xlarge bucket but doesn't shift the existing two
			// boundaries. Reserved for a future bucket-expansion
			// slice; today we report the drift but no threshold
			// change.
		}
	}
	// Invariants: SmallMax ≤ MediumMax. Don't let the calibrator
	// swap them.
	if out.SmallMax > out.MediumMax {
		out.SmallMax = out.MediumMax
	}
	if out.SmallMax < 1 {
		out.SmallMax = 1
	}
	return out
}

func applyShift(current, delta int) int {
	v := current + delta
	if v < 1 {
		return 1
	}
	return v
}

func median(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	dup := make([]time.Duration, len(samples))
	copy(dup, samples)
	sort.Slice(dup, func(i, j int) bool { return dup[i] < dup[j] })
	mid := len(dup) / 2
	if len(dup)%2 == 1 {
		return dup[mid]
	}
	return (dup[mid-1] + dup[mid]) / 2
}
