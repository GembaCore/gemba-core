package retro

import (
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/dispatch"
)

// pickRow builds a calibration row inline. Tests vary the
// ScoreDelta + WasInTop3 + WasTopPick + Mode flags directly to
// control which signal each fixture exercises.
func pickRow(beadID string, picked, top float64, inTop3, wasTop bool) PickCalibrationRow {
	return PickCalibrationRow{
		BeadID:              core.WorkItemID(beadID),
		RecommendedTopBead:  "top-of-set",
		PickedAffinity:      picked,
		RecommendedAffinity: top,
		ScoreDelta:          top - picked,
		WasTopPick:          wasTop,
		WasInTop3:           inTop3,
		DecidedAt:           time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
		Mode:                dispatch.ModeCoach,
	}
}

func TestPickCalibrationFromDecision_DerivesTopAndDelta(t *testing.T) {
	d := dispatch.Decision{
		BeadID: "gm-2",
		ReadySet: []dispatch.ReadySetEntry{
			{BeadID: "gm-1", AffinityCombined: 0.9},
			{BeadID: "gm-2", AffinityCombined: 0.6},
			{BeadID: "gm-3", AffinityCombined: 0.4},
		},
		Affinity: planner.AffinityScores{Combined: 0.6},
		Mode:     dispatch.ModeCoach,
	}
	row, ok := PickCalibrationFromDecision(d)
	if !ok {
		t.Fatal("derivation skipped a non-empty ready set")
	}
	if row.RecommendedTopBead != "gm-1" {
		t.Errorf("top = %q, want gm-1", row.RecommendedTopBead)
	}
	if delta := row.ScoreDelta; delta < 0.29 || delta > 0.31 {
		t.Errorf("delta = %v, want ~0.3 (0.9 - 0.6)", delta)
	}
	if row.WasTopPick {
		t.Error("WasTopPick=true on a non-top pick")
	}
	if !row.WasInTop3 {
		t.Error("WasInTop3=false despite picked being rank 2")
	}
}

func TestPickCalibrationFromDecision_EmptyReadySetSkipped(t *testing.T) {
	d := dispatch.Decision{BeadID: "gm-1"}
	if _, ok := PickCalibrationFromDecision(d); ok {
		t.Error("derivation accepted empty ready set")
	}
}

func TestPickCalibrationFromDecision_PickedNotInReadySetUsesStoredAffinity(t *testing.T) {
	// Operator dispatched a bead that wasn't on the grid (e.g.
	// `gemba dispatch <id>` of a non-ready bead). Score_delta
	// falls back to the stored Affinity.Combined.
	d := dispatch.Decision{
		BeadID: "gm-99",
		ReadySet: []dispatch.ReadySetEntry{
			{BeadID: "gm-1", AffinityCombined: 0.9},
		},
		Affinity: planner.AffinityScores{Combined: 0.4},
		Mode:     dispatch.ModeCoach,
	}
	row, ok := PickCalibrationFromDecision(d)
	if !ok {
		t.Fatal("derivation skipped")
	}
	if row.PickedAffinity != 0.4 {
		t.Errorf("PickedAffinity = %v, want 0.4 (stored)", row.PickedAffinity)
	}
	if row.ScoreDelta != 0.5 {
		t.Errorf("delta = %v, want 0.5", row.ScoreDelta)
	}
	if row.WasInTop3 {
		t.Error("WasInTop3=true despite picked not in ready set")
	}
}

func TestAggregateRecommendationCalibration_BelowSampleSizeReturnsNil(t *testing.T) {
	rows := []PickCalibrationRow{
		pickRow("a", 0.5, 0.9, false, false),
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{MinSampleSize: 10})
	if got != nil {
		t.Errorf("got %v, want nil at sample < 10", got)
	}
}

func TestAggregateRecommendationCalibration_OverrideTop3Fires(t *testing.T) {
	// 8 of 10 rows are picks outside the top-3 → 80% > 30% threshold.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 8; i++ {
		rows = append(rows, pickRow("out-"+string(rune('a'+i)), 0.3, 0.9, false, false))
	}
	for i := 0; i < 2; i++ {
		rows = append(rows, pickRow("in-"+string(rune('a'+i)), 0.85, 0.9, true, true))
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if !hasKind(got, SuggestionOverrideTop3) {
		t.Fatalf("expected SuggestionOverrideTop3; got %+v", got)
	}
	s := findKind(got, SuggestionOverrideTop3)
	if s.Metric < 0.7 {
		t.Errorf("metric = %v, want >= 0.7 (8/10)", s.Metric)
	}
	if s.SampleSize != 10 {
		t.Errorf("sample = %d, want 10", s.SampleSize)
	}
	if len(s.SampleBeads) == 0 {
		t.Error("SampleBeads is empty")
	}
}

func TestAggregateRecommendationCalibration_OverrideMeanDeltaFires(t *testing.T) {
	// 10 rows where the operator overrode the top pick by an
	// average score_delta of 0.4 — well above the 0.20 default.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, pickRow("o-"+string(rune('a'+i)), 0.5, 0.9, true, false))
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if !hasKind(got, SuggestionOverrideMeanDelta) {
		t.Fatalf("expected SuggestionOverrideMeanDelta; got %+v", got)
	}
}

func TestAggregateRecommendationCalibration_AllTopPicksNoSuggestion(t *testing.T) {
	rows := make([]PickCalibrationRow, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, pickRow("top-"+string(rune('a'+i)), 0.9, 0.9, true, true))
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if len(got) != 0 {
		t.Errorf("expected no suggestions for all-top picks; got %+v", got)
	}
}

func TestAggregateRecommendationCalibration_AutoModeRowsExcluded(t *testing.T) {
	// Construct 10 auto-mode rows with maximum override signal —
	// none should reach the aggregator.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		r := pickRow("a-"+string(rune('a'+i)), 0.1, 0.9, false, false)
		r.Mode = dispatch.ModeAuto
		rows = append(rows, r)
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if got != nil {
		t.Errorf("auto-mode rows leaked into aggregator: %+v", got)
	}
}

func TestAggregateBeadSizeCalibration_BelowSampleSizeSkipped(t *testing.T) {
	rows := []SizeCalibrationRow{
		{BeadID: "gm-1", PredictedBucket: core.SizeSmall, ActualDuration: 5 * time.Hour},
	}
	got := AggregateBeadSizeCalibration(rows, SizeCalibrationOptions{MinSampleSize: 5})
	if len(got) != 0 {
		t.Errorf("got %v, want empty at sample < 5", got)
	}
}

func TestAggregateBeadSizeCalibration_ExpandSignal(t *testing.T) {
	// 5 small-bucket beads that actually took 4h each — well past
	// the 30m midpoint and over the 2x drift threshold.
	rows := make([]SizeCalibrationRow, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, SizeCalibrationRow{
			BeadID:          core.WorkItemID("gm-" + string(rune('a'+i))),
			PredictedBucket: core.SizeSmall,
			ActualDuration:  4 * time.Hour,
		})
	}
	got := AggregateBeadSizeCalibration(rows, SizeCalibrationOptions{})
	if len(got) != 1 {
		t.Fatalf("got %d deltas, want 1; full=%+v", len(got), got)
	}
	if got[0].Bucket != core.SizeSmall {
		t.Errorf("bucket = %q, want small", got[0].Bucket)
	}
	if got[0].Direction != "expand" {
		t.Errorf("direction = %q, want expand", got[0].Direction)
	}
	if got[0].SampleSize != 5 {
		t.Errorf("sample = %d, want 5", got[0].SampleSize)
	}
	if got[0].MedianActual != 4*time.Hour {
		t.Errorf("median = %v, want 4h", got[0].MedianActual)
	}
}

func TestAggregateBeadSizeCalibration_ShrinkSignal(t *testing.T) {
	// 5 large-bucket beads that actually took 30m — well under
	// the 6h midpoint by more than 2x.
	rows := make([]SizeCalibrationRow, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, SizeCalibrationRow{
			BeadID:          core.WorkItemID("gm-" + string(rune('a'+i))),
			PredictedBucket: core.SizeLarge,
			ActualDuration:  30 * time.Minute,
		})
	}
	got := AggregateBeadSizeCalibration(rows, SizeCalibrationOptions{})
	if len(got) != 1 || got[0].Direction != "shrink" {
		t.Fatalf("expected shrink delta; got %+v", got)
	}
}

func TestAggregateBeadSizeCalibration_PerRepositoryGrouping(t *testing.T) {
	// Same bucket, two repos — each emits its own delta when the
	// per-repo sample crosses MinSampleSize.
	rows := []SizeCalibrationRow{}
	for i := 0; i < 5; i++ {
		rows = append(rows, SizeCalibrationRow{
			BeadID:     core.WorkItemID("a-" + string(rune('a'+i))),
			Repository: "repo-a", PredictedBucket: core.SizeSmall,
			ActualDuration: 4 * time.Hour,
		})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, SizeCalibrationRow{
			BeadID:     core.WorkItemID("b-" + string(rune('a'+i))),
			Repository: "repo-b", PredictedBucket: core.SizeSmall,
			ActualDuration: 5 * time.Minute, // shrink signal
		})
	}
	got := AggregateBeadSizeCalibration(rows, SizeCalibrationOptions{})
	if len(got) != 2 {
		t.Fatalf("expected 2 deltas (one per repo), got %d: %+v", len(got), got)
	}
	// Sorted by repo so a is first.
	if got[0].Repository != "repo-a" || got[0].Direction != "expand" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Repository != "repo-b" || got[1].Direction != "shrink" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestAggregateBeadSizeCalibration_OnTargetNoDelta(t *testing.T) {
	// Median exactly at the bucket midpoint — no drift, no delta.
	rows := make([]SizeCalibrationRow, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, SizeCalibrationRow{
			BeadID:          core.WorkItemID("gm-" + string(rune('a'+i))),
			PredictedBucket: core.SizeSmall,
			ActualDuration:  30 * time.Minute, // SmallBoundary/2 = 30m
		})
	}
	got := AggregateBeadSizeCalibration(rows, SizeCalibrationOptions{})
	if len(got) != 0 {
		t.Errorf("on-target rows produced delta: %+v", got)
	}
}

func TestMedianDuration_OddAndEvenLengths(t *testing.T) {
	odd := []time.Duration{2 * time.Hour, 1 * time.Hour, 3 * time.Hour}
	if got := medianDuration(odd); got != 2*time.Hour {
		t.Errorf("odd median = %v, want 2h", got)
	}
	even := []time.Duration{1 * time.Hour, 3 * time.Hour, 2 * time.Hour, 4 * time.Hour}
	if got := medianDuration(even); got != 2*time.Hour+30*time.Minute {
		t.Errorf("even median = %v, want 2h30m", got)
	}
}

// ── intent_miss ────────────────────────────────────────────────

func TestAggregateRecommendationCalibration_IntentMissFires(t *testing.T) {
	// 10 intent-set picks, 9 outside top-3 — 90% > 30% threshold.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 9; i++ {
		r := pickRow("im-"+string(rune('a'+i)), 0.4, 0.9, false, false)
		r.IntentSet = true
		rows = append(rows, r)
	}
	r := pickRow("im-z", 0.85, 0.9, true, true)
	r.IntentSet = true
	rows = append(rows, r)

	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if !hasKind(got, SuggestionIntentMiss) {
		t.Fatalf("expected SuggestionIntentMiss; got %+v", got)
	}
	s := findKind(got, SuggestionIntentMiss)
	if s.SampleSize != 10 {
		t.Errorf("intent sample size = %d, want 10", s.SampleSize)
	}
	if s.Metric < 0.85 {
		t.Errorf("intent miss rate = %v, want >= 0.85", s.Metric)
	}
}

func TestAggregateRecommendationCalibration_IntentMissNeedsSubFloor(t *testing.T) {
	// 5 intent-set rows, 5 non-intent rows. Aggregate-wide floor
	// met (10 total); intent sub-population below the 10-row sub-
	// floor — must NOT flag SuggestionIntentMiss even though every
	// intent row missed top-3.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 5; i++ {
		r := pickRow("i-"+string(rune('a'+i)), 0.4, 0.9, false, false)
		r.IntentSet = true
		rows = append(rows, r)
	}
	for i := 0; i < 5; i++ {
		// Plain top picks — keep override-top-3 from also firing.
		rows = append(rows, pickRow("p-"+string(rune('a'+i)), 0.85, 0.9, true, true))
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if hasKind(got, SuggestionIntentMiss) {
		t.Errorf("intent sub-floor not met but flag fired: %+v", got)
	}
}

// ── leverage_miss ──────────────────────────────────────────────

func TestAggregateRecommendationCalibration_LeverageMissFires(t *testing.T) {
	// 10 override rows where picked carries higher LeverageWeight
	// than the recommendation — 100% > 30% threshold.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		r := pickRow("lm-"+string(rune('a'+i)), 0.4, 0.9, false, false)
		r.PickedLeverageWeight = 5
		r.RecommendedLeverageWeight = 1
		rows = append(rows, r)
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if !hasKind(got, SuggestionLeverageMiss) {
		t.Fatalf("expected SuggestionLeverageMiss; got %+v", got)
	}
}

func TestAggregateRecommendationCalibration_LeverageMissSkipsUninstrumentedRows(t *testing.T) {
	// 10 override rows with no LeverageWeight populated — leverage
	// signal denominator stays at 0; flag must not fire.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, pickRow("lm-"+string(rune('a'+i)), 0.4, 0.9, false, false))
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if hasKind(got, SuggestionLeverageMiss) {
		t.Errorf("expected no LeverageMiss without instrumentation; got %+v", got)
	}
}

// ── runway_overtrust ───────────────────────────────────────────

func TestAggregateRecommendationCalibration_RunwayOvertrustFires(t *testing.T) {
	// 10 override rows where the recommendation was runway-demoted
	// but the picked bead was not. 100% over the 30% threshold.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		r := pickRow("ro-"+string(rune('a'+i)), 0.4, 0.9, false, false)
		r.RecommendedRunwayDemoted = true
		r.PickedRunwayDemoted = false
		rows = append(rows, r)
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if !hasKind(got, SuggestionRunwayOvertrust) {
		t.Fatalf("expected SuggestionRunwayOvertrust; got %+v", got)
	}
}

func TestAggregateRecommendationCalibration_RunwayBothDemotedNoFlag(t *testing.T) {
	// Both beads runway-demoted on every row → no over-trust signal
	// (operator just preferred a different big bead).
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		r := pickRow("ro-"+string(rune('a'+i)), 0.4, 0.9, false, false)
		r.RecommendedRunwayDemoted = true
		r.PickedRunwayDemoted = true
		rows = append(rows, r)
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if hasKind(got, SuggestionRunwayOvertrust) {
		t.Errorf("both-demoted must not flag; got %+v", got)
	}
}

func TestAggregateRecommendationCalibration_RunwayUninstrumentedRowsSkipped(t *testing.T) {
	// 10 overrides with no runway flags populated → no signal,
	// no flag.
	rows := make([]PickCalibrationRow, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, pickRow("ro-"+string(rune('a'+i)), 0.4, 0.9, false, false))
	}
	got := AggregateRecommendationCalibration(rows, CalibrationOptions{})
	if hasKind(got, SuggestionRunwayOvertrust) {
		t.Errorf("no runway instrumentation must not flag; got %+v", got)
	}
}

func hasKind(ss []Suggestion, k SuggestionKind) bool {
	for _, s := range ss {
		if s.Kind == k {
			return true
		}
	}
	return false
}

func findKind(ss []Suggestion, k SuggestionKind) Suggestion {
	for _, s := range ss {
		if s.Kind == k {
			return s
		}
	}
	return Suggestion{}
}
