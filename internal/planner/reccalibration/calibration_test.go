// Pure aggregation tests for the recommendation calibration loop
// (gm-v5z2.8a, work-planning.md §7.5).

package reccalibration

import (
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// pick builds a coach-mode override sample with the minimum fields
// the aggregator inspects. Helpers below layer on intent / component
// snapshots when a specific signal is under test.
func pick(rec, picked string, recScore, pickedScore float64, rank int) Sample {
	return Sample{
		Mode:                ModeCoach,
		RecommendedTopBead:  core.WorkItemID(rec),
		RecommendedTopScore: recScore,
		PickedBead:          core.WorkItemID(picked),
		PickedScore:         pickedScore,
		PickedRank:          rank,
	}
}

// agree builds a coach-mode "took the recommendation" sample.
// PickedRank=1 by definition.
func agree(bead string, score float64) Sample {
	return Sample{
		Mode:                ModeCoach,
		RecommendedTopBead:  core.WorkItemID(bead),
		RecommendedTopScore: score,
		PickedBead:          core.WorkItemID(bead),
		PickedScore:         score,
		PickedRank:          1,
	}
}

// ── filters & shape ─────────────────────────────────────────────

func TestAggregate_NoSamplesIsBenign(t *testing.T) {
	got := Aggregate(nil, nil)
	if got.Findings.AnyFlag() {
		t.Errorf("empty input must not flag anything: %+v", got.Findings)
	}
	if got.Findings.SampleCount != 0 {
		t.Errorf("empty input must report zero samples; got %d", got.Findings.SampleCount)
	}
}

func TestAggregate_AutoModeRowsFiltered(t *testing.T) {
	in := []Sample{
		{Mode: ModeAuto, RecommendedTopBead: "a", PickedBead: "b", PickedRank: 5},
		{Mode: ModeAuto, RecommendedTopBead: "a", PickedBead: "b", PickedRank: 5},
	}
	got := Aggregate(in, nil)
	if got.Findings.SampleCount != 0 {
		t.Errorf("auto-mode rows must be filtered; got %d coach samples", got.Findings.SampleCount)
	}
}

func TestAggregate_DropsRowsMissingBeadIDs(t *testing.T) {
	in := []Sample{
		{Mode: ModeCoach, RecommendedTopBead: "", PickedBead: "x"},
		{Mode: ModeCoach, RecommendedTopBead: "x", PickedBead: ""},
	}
	got := Aggregate(in, nil)
	if got.Findings.SampleCount != 0 {
		t.Errorf("rows missing bead IDs must drop; got %d", got.Findings.SampleCount)
	}
}

// ── below floor: never flag ─────────────────────────────────────

func TestAggregate_BelowMinSamplesNeverFlags(t *testing.T) {
	// 9 overrides, all outside top-3, large delta — would flag if
	// MinSamples (10) wasn't blocking it.
	in := make([]Sample, 0, 9)
	for i := 0; i < 9; i++ {
		in = append(in, pick("rec", "picked", 0.9, 0.5, 7))
	}
	got := Aggregate(in, nil)
	if got.Findings.AnyFlag() {
		t.Errorf("below MinSamples must not flag; got %+v", got.Findings)
	}
	if got.Findings.SampleCount != 9 {
		t.Errorf("expected 9 samples; got %d", got.Findings.SampleCount)
	}
}

// ── high override rate but small delta → no flag ────────────────

func TestAggregate_LowDeltaDoesNotFlagOverrideTop3(t *testing.T) {
	// 12 coach samples, 100% overrides outside top-3, BUT delta is
	// tiny (0.01) — operator preference at the margin, not a
	// systematic miss.
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		in = append(in, pick("rec", "picked", 0.51, 0.50, 7))
	}
	got := Aggregate(in, nil)
	if got.Findings.OverrideTop3 {
		t.Errorf("low mean delta must suppress top-3 flag; got %+v", got.Findings)
	}
	if got.Findings.OverrideRate < 0.99 {
		t.Errorf("override rate should still be ~1.0; got %f", got.Findings.OverrideRate)
	}
}

// ── canonical top-3 miss ────────────────────────────────────────

func TestAggregate_OverrideTop3FlagsWhenAllConditionsMet(t *testing.T) {
	in := make([]Sample, 0, 15)
	// 12 of 15 are overrides outside top-3 with a 0.40 delta.
	for i := 0; i < 12; i++ {
		in = append(in, pick("rec", "picked", 0.90, 0.50, 7))
	}
	for i := 0; i < 3; i++ {
		in = append(in, agree("rec", 0.90))
	}
	got := Aggregate(in, nil)
	if !got.Findings.OverrideTop3 {
		t.Errorf("expected OverrideTop3 to flag; got %+v", got.Findings)
	}
	if got.Findings.OverrideCount != 12 {
		t.Errorf("override count: want 12 got %d", got.Findings.OverrideCount)
	}
	if got.Findings.OverrideTop3Rate != 1.0 {
		t.Errorf("all overrides outside top-3 → rate 1.0; got %f", got.Findings.OverrideTop3Rate)
	}
}

// PickedRank=0 (operator picked a bead Selection didn't surface)
// counts as outside top-3.
func TestAggregate_PickedRankZeroCountsAsOutsideTop3(t *testing.T) {
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		s := pick("rec", "picked", 0.90, 0.50, 0)
		in = append(in, s)
	}
	got := Aggregate(in, nil)
	if !got.Findings.OverrideTop3 {
		t.Errorf("rank=0 picks should count as outside top-3; got %+v", got.Findings)
	}
}

// PickedRank in top-3 → override rate clears, top-3 rate doesn't.
func TestAggregate_OverrideInsideTop3DoesNotFlag(t *testing.T) {
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		// Operator agreed-ish: picked the #2 bead, not the #1.
		in = append(in, pick("rec", "picked", 0.90, 0.50, 2))
	}
	got := Aggregate(in, nil)
	if got.Findings.OverrideTop3 {
		t.Errorf("top-3 picks must not flag; got %+v", got.Findings)
	}
	if got.Findings.OverrideRate < 0.99 {
		t.Errorf("override rate should still be ~1.0; got %f", got.Findings.OverrideRate)
	}
}

// ── intent miss ─────────────────────────────────────────────────

func TestAggregate_SystematicIntentMissFlags(t *testing.T) {
	in := make([]Sample, 0, 12)
	// 12 intent-set overrides, all outside top-3.
	for i := 0; i < 12; i++ {
		s := pick("rec", "picked", 0.90, 0.50, 7)
		s.IntentSet = true
		in = append(in, s)
	}
	got := Aggregate(in, nil)
	if !got.Findings.SystematicIntentMiss {
		t.Errorf("expected SystematicIntentMiss; got %+v", got.Findings)
	}
	if got.Findings.IntentSampleCount != 12 {
		t.Errorf("intent sample count: want 12 got %d", got.Findings.IntentSampleCount)
	}
}

func TestAggregate_IntentMissRequiresFloor(t *testing.T) {
	in := make([]Sample, 0, 15)
	// Only 5 intent-set; 10 non-intent. Aggregate-wide floor met,
	// intent sub-floor not met.
	for i := 0; i < 5; i++ {
		s := pick("rec", "picked", 0.90, 0.50, 7)
		s.IntentSet = true
		in = append(in, s)
	}
	for i := 0; i < 10; i++ {
		// Plain agree to keep override-rate from cleared overall.
		in = append(in, agree("rec", 0.90))
	}
	got := Aggregate(in, nil)
	if got.Findings.SystematicIntentMiss {
		t.Errorf("intent sub-floor not met; must not flag: %+v", got.Findings)
	}
}

// ── leverage miss ───────────────────────────────────────────────

func TestAggregate_SystematicLeverageMissFlags(t *testing.T) {
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		s := pick("rec", "picked", 0.90, 0.50, 7)
		s.RecommendedComponents = &Components{LeverageWeight: 1}
		s.PickedComponents = &Components{LeverageWeight: 5}
		in = append(in, s)
	}
	got := Aggregate(in, nil)
	if !got.Findings.SystematicLeverageMiss {
		t.Errorf("expected SystematicLeverageMiss when picked > recommended LeverageWeight; got %+v", got.Findings)
	}
}

func TestAggregate_LeverageMissIgnoresMissingComponents(t *testing.T) {
	// Without component snapshots we have no leverage signal —
	// must not flag from the absence of data.
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		in = append(in, pick("rec", "picked", 0.90, 0.50, 7))
	}
	got := Aggregate(in, nil)
	if got.Findings.SystematicLeverageMiss {
		t.Errorf("missing components must not flag leverage-miss; got %+v", got.Findings)
	}
	if got.Findings.LeverageMissRate != 0 {
		t.Errorf("missing components → 0 leverage miss rate; got %f", got.Findings.LeverageMissRate)
	}
}

// ── runway over-trust ───────────────────────────────────────────

func TestAggregate_SystematicRunwayOvertrustFlags(t *testing.T) {
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		s := pick("rec", "picked", 0.90, 0.50, 2)
		s.RecommendedComponents = &Components{RunwayDemoted: true}
		s.PickedComponents = &Components{RunwayDemoted: false}
		in = append(in, s)
	}
	got := Aggregate(in, nil)
	if !got.Findings.SystematicRunwayOvertrust {
		t.Errorf("expected SystematicRunwayOvertrust; got %+v", got.Findings)
	}
}

// Both runway-demoted: the operator just picked a different big
// bead — not "over-trust", just bead preference. Don't flag.
func TestAggregate_RunwayBothDemotedNotFlagged(t *testing.T) {
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		s := pick("rec", "picked", 0.90, 0.50, 2)
		s.RecommendedComponents = &Components{RunwayDemoted: true}
		s.PickedComponents = &Components{RunwayDemoted: true}
		in = append(in, s)
	}
	got := Aggregate(in, nil)
	if got.Findings.SystematicRunwayOvertrust {
		t.Errorf("both-demoted must not flag; got %+v", got.Findings)
	}
}

// ── thresholds override ─────────────────────────────────────────

func TestAggregate_ThresholdOverrideTightensFlagging(t *testing.T) {
	// 12 samples, 10 overrides at 0.20 delta, 2 agreement (0.83
	// override rate, 0.20 mean delta — exactly at default
	// MeanScoreDelta=0.20). Tighten the override-rate threshold
	// to 0.99 and the OverrideTop3 flag must NOT flip.
	in := make([]Sample, 0, 12)
	for i := 0; i < 10; i++ {
		in = append(in, pick("rec", "picked", 0.70, 0.50, 7))
	}
	for i := 0; i < 2; i++ {
		in = append(in, agree("rec", 0.70))
	}
	got := Aggregate(in, &Thresholds{OverrideRate: 0.99})
	if got.Findings.OverrideTop3 {
		t.Errorf("tighter override-rate threshold should suppress flag; got %+v", got.Findings)
	}
}

func TestAggregate_ThresholdOverrideRelaxesFlagging(t *testing.T) {
	// 12 samples, all overrides outside top-3, but delta is small.
	// Relax MeanScoreDelta to 0.001 and the flag should flip.
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		in = append(in, pick("rec", "picked", 0.51, 0.50, 7))
	}
	got := Aggregate(in, &Thresholds{MeanScoreDelta: 0.001})
	if !got.Findings.OverrideTop3 {
		t.Errorf("relaxed MeanScoreDelta should flip the flag; got %+v", got.Findings)
	}
}

// ── thresholds in result ────────────────────────────────────────

func TestAggregate_SuggestionEchoesResolvedThresholds(t *testing.T) {
	got := Aggregate([]Sample{agree("rec", 0.5)}, nil)
	if got.Thresholds.MinSamples != DefaultThresholds.MinSamples {
		t.Errorf("default thresholds not echoed; got %+v", got.Thresholds)
	}
	got2 := Aggregate([]Sample{agree("rec", 0.5)}, &Thresholds{MinSamples: 5})
	if got2.Thresholds.MinSamples != 5 {
		t.Errorf("custom MinSamples not threaded; got %+v", got2.Thresholds)
	}
}

// ── notes ──────────────────────────────────────────────────────

func TestAggregate_NoFlagsProducesAClearNote(t *testing.T) {
	in := make([]Sample, 0, 12)
	for i := 0; i < 12; i++ {
		in = append(in, agree("rec", 0.5))
	}
	got := Aggregate(in, nil)
	if got.Findings.AnyFlag() {
		t.Errorf("100%% agreement must not flag; got %+v", got.Findings)
	}
	if len(got.Notes) == 0 {
		t.Error("expected an informational note")
	}
}

// AnyFlag() helper.
func TestFindings_AnyFlag(t *testing.T) {
	cases := []struct {
		name string
		f    Findings
		want bool
	}{
		{"empty", Findings{}, false},
		{"top3", Findings{OverrideTop3: true}, true},
		{"intent", Findings{SystematicIntentMiss: true}, true},
		{"leverage", Findings{SystematicLeverageMiss: true}, true},
		{"runway", Findings{SystematicRunwayOvertrust: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.AnyFlag(); got != tc.want {
				t.Errorf("%s: want %v got %v", tc.name, tc.want, got)
			}
		})
	}
}
