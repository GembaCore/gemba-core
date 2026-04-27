// Recommendation + bead-size calibration aggregators (gm-v5z2.8,
// work-planning.md §7.5 + §7.6). Both are PURE — they take
// fixed-shape inputs and emit suggestions a downstream review queue
// renders. Wiring them to the actual periodic loop / operator queue
// happens elsewhere; this file owns just the math.
//
// §7.5 Recommendation calibration: every coach-mode dispatch
// records the top recommendation alongside the operator's pick. The
// aggregator scans recent dispatches and emits suggestions when the
// override pattern is systematic enough to act on (the planner is
// mis-calibrated, not just disagreed-with on individual rows).
//
// §7.6 Bead-size calibration: the estimated_size heuristic
// (description-length × DoD-line-count) is graded against actual
// time-to-close. Drift > 2x in either direction contributes a
// delta to the bucket boundary so the heuristic auto-tunes per-rig
// without operator config edits.

package retro

import (
	"sort"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner/dispatch"
)

// PickCalibrationRow is one (recommended_top, picked, score_delta,
// operator_reason) tuple derived from a dispatch.Decision. The
// "recommended_top" is the highest-affinity entry in the decision's
// ready set; "picked" is the bead the operator actually selected.
//
// Constructed via [PickCalibrationFromDecision] so the derivation
// rule (top-of-ready-set by AffinityCombined) lives in one place.
type PickCalibrationRow struct {
	BeadID            core.WorkItemID `json:"bead_id"`            // picked
	RecommendedTopBead core.WorkItemID `json:"recommended_top_bead"`
	PickedAffinity    float64         `json:"picked_affinity"`
	RecommendedAffinity float64       `json:"recommended_affinity"`
	// ScoreDelta = recommended.AffinityCombined - picked.AffinityCombined.
	// > 0 means the operator picked something the planner thought
	// was lower-scoring (an override); 0 means the operator agreed
	// with the planner; < 0 is degenerate (the picked bead beat
	// the ready-set top — usually a stale ready set).
	ScoreDelta     float64   `json:"score_delta"`
	WasTopPick     bool      `json:"was_top_pick"`
	WasInTop3      bool      `json:"was_in_top_3"`
	OperatorReason string    `json:"operator_reason,omitempty"`
	DecidedAt      time.Time `json:"decided_at"`
	// Mode mirrors dispatch.Decision.Mode. Auto-mode dispatches are
	// excluded from override detection (the operator wasn't there
	// to override) — kept on the row so a downstream consumer can
	// filter rather than the aggregator silently dropping data.
	Mode dispatch.Mode `json:"mode"`
}

// PickCalibrationFromDecision derives a calibration row from a
// stored dispatch decision. Returns ok=false when the ready set is
// empty (no calibration signal — every pick is degenerately the
// top because there were no alternatives).
func PickCalibrationFromDecision(d dispatch.Decision) (PickCalibrationRow, bool) {
	if len(d.ReadySet) == 0 {
		return PickCalibrationRow{}, false
	}
	// Find the top-affinity entry in the ready set and the picked
	// bead's row (so we have both affinities for the delta).
	var (
		topBead     core.WorkItemID
		topScore    float64
		pickedScore float64
		pickedSeen  bool
	)
	for i, r := range d.ReadySet {
		if i == 0 || r.AffinityCombined > topScore {
			topBead = r.BeadID
			topScore = r.AffinityCombined
		}
		if r.BeadID == d.BeadID {
			pickedScore = r.AffinityCombined
			pickedSeen = true
		}
	}
	// If the picked bead isn't in the ready set, fall back to the
	// stored dispatch affinity (the planner scored it elsewhere
	// and the row reflects that).
	if !pickedSeen {
		pickedScore = d.Affinity.Combined
	}

	rank := rankOfBead(d.ReadySet, d.BeadID)
	return PickCalibrationRow{
		BeadID:              d.BeadID,
		RecommendedTopBead:  topBead,
		PickedAffinity:      pickedScore,
		RecommendedAffinity: topScore,
		ScoreDelta:          topScore - pickedScore,
		WasTopPick:          rank == 1,
		WasInTop3:           rank > 0 && rank <= 3,
		OperatorReason:      d.OperatorReason,
		DecidedAt:           d.DecidedAt,
		Mode:                d.Mode,
	}, true
}

// rankOfBead returns the 1-based rank of bead in readySet sorted by
// AffinityCombined descending. Returns 0 when the bead isn't in the
// set (the operator picked outside the ready window, e.g. via
// `gemba dispatch <id>` of a bead that wasn't on the grid).
func rankOfBead(readySet []dispatch.ReadySetEntry, bead core.WorkItemID) int {
	type pair struct {
		bead  core.WorkItemID
		score float64
	}
	sorted := make([]pair, 0, len(readySet))
	for _, r := range readySet {
		sorted = append(sorted, pair{r.BeadID, r.AffinityCombined})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})
	for i, s := range sorted {
		if s.bead == bead {
			return i + 1
		}
	}
	return 0
}

// CalibrationOptions tunes the override-rate thresholds the
// aggregator triggers on. Zero values resolve to the design
// defaults; tests pin deterministic values.
type CalibrationOptions struct {
	// MinSampleSize is the smallest number of coach-mode rows the
	// aggregator considers before emitting any suggestion. Below
	// this the signal is noise. Default 10.
	MinSampleSize int
	// OverrideRateThreshold is the fraction of coach picks where
	// the operator chose outside the top-3 above which the
	// aggregator emits OverrideTop3Suggestion. Range [0, 1];
	// default 0.30 (operator overrode the top-3 on >=30% of
	// recent dispatches).
	OverrideRateThreshold float64
	// MeanScoreDeltaThreshold is the average score_delta across
	// non-degenerate picks above which the aggregator emits
	// OverrideMeanDeltaSuggestion. Default 0.20 (operator
	// systematically picks beads scoring 0.2 below the planner's
	// top — the planner is over-confident).
	MeanScoreDeltaThreshold float64
}

// SuggestionKind names the calibration signals the aggregator can
// emit. Plain string so a future signal lands without an enum
// churn; the operator-review queue treats unknown kinds opaquely.
type SuggestionKind string

const (
	// SuggestionOverrideTop3: operators pick outside the planner's
	// top-3 with a frequency that exceeds OverrideRateThreshold.
	// Likely indicates the affinity weighting is tuned wrong for
	// this rig.
	SuggestionOverrideTop3 SuggestionKind = "override_top_3"
	// SuggestionOverrideMeanDelta: the average score_delta on
	// override picks exceeds MeanScoreDeltaThreshold. The planner
	// thinks its top picks are decisively better; the operator
	// disagrees — probably a missing signal, not a weight tweak.
	SuggestionOverrideMeanDelta SuggestionKind = "override_mean_delta"
)

// Suggestion is one calibration finding. The operator-review queue
// renders Reason verbatim; the metadata fields surface in the
// detail view so the operator can drill in.
type Suggestion struct {
	Kind       SuggestionKind  `json:"kind"`
	Reason     string          `json:"reason"`
	SampleSize int             `json:"sample_size"`
	Metric     float64         `json:"metric"`
	Threshold  float64         `json:"threshold"`
	// SampleBeads is up to ten bead ids that contributed to the
	// signal — gives the operator a starting point for the drill-in
	// without scanning the full window.
	SampleBeads []core.WorkItemID `json:"sample_beads,omitempty"`
}

// AggregateRecommendationCalibration scans coach-mode rows and
// emits suggestions when the override pattern crosses the
// configured thresholds. Pure: no I/O, no clock reads. Auto-mode
// rows are dropped (the operator wasn't there to override).
func AggregateRecommendationCalibration(rows []PickCalibrationRow, opts CalibrationOptions) []Suggestion {
	o := opts.resolved()
	coach := make([]PickCalibrationRow, 0, len(rows))
	for _, r := range rows {
		if r.Mode != dispatch.ModeCoach && r.Mode != "" {
			continue
		}
		coach = append(coach, r)
	}
	if len(coach) < o.MinSampleSize {
		return nil
	}

	out := make([]Suggestion, 0, 2)

	// SuggestionOverrideTop3.
	outsideTop3 := 0
	outsideTop3Beads := make([]core.WorkItemID, 0, 10)
	for _, r := range coach {
		if !r.WasInTop3 {
			outsideTop3++
			if len(outsideTop3Beads) < 10 {
				outsideTop3Beads = append(outsideTop3Beads, r.BeadID)
			}
		}
	}
	rate := float64(outsideTop3) / float64(len(coach))
	if rate >= o.OverrideRateThreshold {
		out = append(out, Suggestion{
			Kind:        SuggestionOverrideTop3,
			Reason:      "operators pick outside the planner's top-3 frequently — affinity weighting may be off",
			SampleSize:  len(coach),
			Metric:      rate,
			Threshold:   o.OverrideRateThreshold,
			SampleBeads: outsideTop3Beads,
		})
	}

	// SuggestionOverrideMeanDelta — measured only over rows where
	// the operator overrode the top pick (otherwise score_delta
	// is 0 by construction and dilutes the signal).
	overrides := make([]PickCalibrationRow, 0, len(coach))
	for _, r := range coach {
		if !r.WasTopPick {
			overrides = append(overrides, r)
		}
	}
	if len(overrides) > 0 {
		sumDelta := 0.0
		sampleBeads := make([]core.WorkItemID, 0, 10)
		for _, r := range overrides {
			sumDelta += r.ScoreDelta
			if len(sampleBeads) < 10 {
				sampleBeads = append(sampleBeads, r.BeadID)
			}
		}
		mean := sumDelta / float64(len(overrides))
		if mean >= o.MeanScoreDeltaThreshold {
			out = append(out, Suggestion{
				Kind:        SuggestionOverrideMeanDelta,
				Reason:      "operator overrides systematically pick lower-scored beads — a signal is missing from the score",
				SampleSize:  len(overrides),
				Metric:      mean,
				Threshold:   o.MeanScoreDeltaThreshold,
				SampleBeads: sampleBeads,
			})
		}
	}

	return out
}

func (o CalibrationOptions) resolved() CalibrationOptions {
	d := CalibrationOptions{
		MinSampleSize:           10,
		OverrideRateThreshold:   0.30,
		MeanScoreDeltaThreshold: 0.20,
	}
	if o.MinSampleSize > 0 {
		d.MinSampleSize = o.MinSampleSize
	}
	if o.OverrideRateThreshold > 0 {
		d.OverrideRateThreshold = o.OverrideRateThreshold
	}
	if o.MeanScoreDeltaThreshold > 0 {
		d.MeanScoreDeltaThreshold = o.MeanScoreDeltaThreshold
	}
	return d
}

// SizeCalibrationRow is one (predicted bucket vs actual time-to-
// close) tuple. The retro pipeline derives one row per closed
// bead; the aggregator folds them into per-bucket boundary deltas.
type SizeCalibrationRow struct {
	BeadID         core.WorkItemID    `json:"bead_id"`
	PredictedBucket core.EstimatedSize `json:"predicted_bucket"`
	ActualDuration time.Duration      `json:"actual_duration"`
	// Repository / AuthorID are the per-rig + per-author breakdown
	// the spec calls for. Empty when the bead doesn't carry the
	// signal — those rows roll up to the workspace bucket.
	Repository core.RepositoryID `json:"repository,omitempty"`
	AuthorID   string            `json:"author_id,omitempty"`
}

// SizeBucketDelta is one suggested boundary tweak the aggregator
// emits when a bucket's actual durations drift > 2x from the
// implied boundary. Operator-review-queue consumers apply (or
// reject) it manually; the value never auto-tunes.
type SizeBucketDelta struct {
	Bucket       core.EstimatedSize `json:"bucket"`
	Repository   core.RepositoryID  `json:"repository,omitempty"`
	SampleSize   int                `json:"sample_size"`
	MedianActual time.Duration      `json:"median_actual"`
	BoundaryHint time.Duration      `json:"boundary_hint"`
	Direction    string             `json:"direction"` // "expand" (bucket too small) | "shrink" (too large)
}

// SizeCalibrationOptions tunes per-bucket boundaries the
// aggregator compares median actuals against. Zero values resolve
// to placeholders; the spec calls for per-rig calibration via the
// operator-review queue, so these defaults are starting points
// only.
type SizeCalibrationOptions struct {
	// MinSampleSize is the smallest sample-per-bucket the
	// aggregator emits a delta for. Default 5.
	MinSampleSize int
	// SmallBoundary, MediumBoundary are the implied boundaries
	// between buckets — small ≤ SmallBoundary < medium ≤
	// MediumBoundary < large. Defaults: 1h / 4h.
	SmallBoundary  time.Duration
	MediumBoundary time.Duration
	// DriftRatio is the multiplier above which the aggregator
	// emits an "expand" delta (or below 1/DriftRatio for "shrink").
	// Default 2.0 per the spec.
	DriftRatio float64
}

func (o SizeCalibrationOptions) resolved() SizeCalibrationOptions {
	d := SizeCalibrationOptions{
		MinSampleSize:  5,
		SmallBoundary:  1 * time.Hour,
		MediumBoundary: 4 * time.Hour,
		DriftRatio:     2.0,
	}
	if o.MinSampleSize > 0 {
		d.MinSampleSize = o.MinSampleSize
	}
	if o.SmallBoundary > 0 {
		d.SmallBoundary = o.SmallBoundary
	}
	if o.MediumBoundary > 0 {
		d.MediumBoundary = o.MediumBoundary
	}
	if o.DriftRatio > 0 {
		d.DriftRatio = o.DriftRatio
	}
	return d
}

// AggregateBeadSizeCalibration folds rows into per-(repository,
// bucket) deltas. Pure: no I/O, no clock reads. Empty input
// returns nil.
//
// The output is sorted by (repository, bucket) for deterministic
// rendering — operators reviewing a long list want the same order
// across reloads.
func AggregateBeadSizeCalibration(rows []SizeCalibrationRow, opts SizeCalibrationOptions) []SizeBucketDelta {
	o := opts.resolved()
	if len(rows) == 0 {
		return nil
	}

	type key struct {
		Repo   core.RepositoryID
		Bucket core.EstimatedSize
	}
	groups := make(map[key][]time.Duration)
	for _, r := range rows {
		bucket := r.PredictedBucket.Effective()
		k := key{r.Repository, bucket}
		groups[k] = append(groups[k], r.ActualDuration)
	}

	out := make([]SizeBucketDelta, 0, len(groups))
	for k, durations := range groups {
		if len(durations) < o.MinSampleSize {
			continue
		}
		median := medianDuration(durations)
		// Implied "expected" duration for the bucket midpoint —
		// small ≈ SmallBoundary/2, medium ≈ midpoint between
		// boundaries, large ≈ MediumBoundary*1.5 (open-ended).
		expected := bucketMidpoint(k.Bucket, o)
		if expected == 0 {
			continue
		}
		ratio := float64(median) / float64(expected)
		direction := ""
		if ratio >= o.DriftRatio {
			direction = "expand"
		} else if ratio*o.DriftRatio <= 1 {
			direction = "shrink"
		} else {
			continue
		}
		out = append(out, SizeBucketDelta{
			Bucket:       k.Bucket,
			Repository:   k.Repo,
			SampleSize:   len(durations),
			MedianActual: median,
			BoundaryHint: median,
			Direction:    direction,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Repository != out[j].Repository {
			return out[i].Repository < out[j].Repository
		}
		return out[i].Bucket < out[j].Bucket
	})
	return out
}

// bucketMidpoint returns a representative duration for a bucket so
// the aggregator has something to compare median actuals against.
// Open-ended "large" uses MediumBoundary * 1.5 as a placeholder —
// the spec calls for per-rig calibration; this is the boot value.
func bucketMidpoint(b core.EstimatedSize, o SizeCalibrationOptions) time.Duration {
	switch b.Effective() {
	case core.SizeSmall:
		return o.SmallBoundary / 2
	case core.SizeMedium:
		return (o.SmallBoundary + o.MediumBoundary) / 2
	case core.SizeLarge:
		return time.Duration(float64(o.MediumBoundary) * 1.5)
	}
	return 0
}

// medianDuration sorts in place; callers that don't want their
// slice mutated pass a copy.
func medianDuration(in []time.Duration) time.Duration {
	if len(in) == 0 {
		return 0
	}
	sort.Slice(in, func(i, j int) bool { return in[i] < in[j] })
	mid := len(in) / 2
	if len(in)%2 == 1 {
		return in[mid]
	}
	return (in[mid-1] + in[mid]) / 2
}
