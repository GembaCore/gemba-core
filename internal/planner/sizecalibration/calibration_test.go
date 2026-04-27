// Pure aggregation tests for the bead-size calibration loop
// (gm-v5z2.8b, work-planning.md §7.6).

package sizecalibration

import (
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/enrichment"
)

func sample(b enrichment.EstimatedSize, dur time.Duration) Sample {
	return Sample{Predicted: b, Actual: dur}
}

// ── median helper ───────────────────────────────────────────────

func TestMedian_Empty(t *testing.T) {
	if got := median(nil); got != 0 {
		t.Errorf("empty median = 0; got %v", got)
	}
}

func TestMedian_Odd(t *testing.T) {
	in := []time.Duration{3 * time.Hour, time.Hour, 2 * time.Hour}
	if got := median(in); got != 2*time.Hour {
		t.Errorf("median: got %v want 2h", got)
	}
}

func TestMedian_Even(t *testing.T) {
	in := []time.Duration{4 * time.Hour, 2 * time.Hour}
	if got := median(in); got != 3*time.Hour {
		t.Errorf("median: got %v want 3h", got)
	}
}

// ── Aggregate: insufficient signal ──────────────────────────────

func TestAggregate_InsufficientSamples(t *testing.T) {
	// Three samples in small (need ≥5) — direction must be
	// "insufficient", no threshold shift.
	in := []Sample{
		sample(enrichment.SizeSmall, 25*time.Minute),
		sample(enrichment.SizeSmall, 35*time.Minute),
		sample(enrichment.SizeSmall, 30*time.Minute),
	}
	got := Aggregate(in, nil, nil)
	if len(got.Reports) != 3 {
		t.Fatalf("expected 3 bucket reports; got %d", len(got.Reports))
	}
	if got.Reports[0].Direction != "insufficient" {
		t.Errorf("small bucket should be insufficient; got %q", got.Reports[0].Direction)
	}
	if got.AnyDrift() {
		t.Error("insufficient samples must not flag drift")
	}
	if got.NewThresholds != nil {
		t.Error("insufficient samples must not produce new thresholds")
	}
}

// ── Aggregate: ok / no drift ────────────────────────────────────

func TestAggregate_NoDrift(t *testing.T) {
	in := []Sample{}
	for i := 0; i < 6; i++ {
		// All small samples land near the 30-minute target.
		in = append(in, sample(enrichment.SizeSmall, 28*time.Minute))
	}
	got := Aggregate(in, nil, nil)
	small := got.Reports[0]
	if small.Direction != "ok" {
		t.Errorf("samples near target → ok; got %q (median=%fs)", small.Direction, small.MedianActualSeconds)
	}
	if got.AnyDrift() {
		t.Error("no drift expected")
	}
	if got.NewThresholds != nil {
		t.Error("no thresholds change expected")
	}
}

// ── Aggregate: high drift (under-estimate) ──────────────────────

func TestAggregate_HighDriftLowersThreshold(t *testing.T) {
	// Six small samples that take 90 minutes — 3× the 30-min
	// target. Heuristic under-estimates → suggest lowering
	// SmallMax so future similar-shaped beads get promoted to
	// medium.
	in := []Sample{}
	for i := 0; i < 6; i++ {
		in = append(in, sample(enrichment.SizeSmall, 90*time.Minute))
	}
	got := Aggregate(in, nil, nil)
	small := got.Reports[0]
	if small.Direction != "high" {
		t.Errorf("3× target → high; got %q", small.Direction)
	}
	if !got.AnyDrift() {
		t.Error("expected drift flag")
	}
	if got.NewThresholds == nil {
		t.Fatal("expected NewThresholds on drift")
	}
	if got.NewThresholds.SmallMax >= got.Current.SmallMax {
		t.Errorf("SmallMax should LOWER on under-estimate; got %d (was %d)",
			got.NewThresholds.SmallMax, got.Current.SmallMax)
	}
}

// ── Aggregate: low drift (over-estimate) ────────────────────────

func TestAggregate_LowDriftRaisesThreshold(t *testing.T) {
	// Six small samples that take 5 min each — 1/6 of the target.
	// Heuristic over-estimates the bucket → suggest raising
	// SmallMax so beads stay in small longer.
	in := []Sample{}
	for i := 0; i < 6; i++ {
		in = append(in, sample(enrichment.SizeSmall, 5*time.Minute))
	}
	got := Aggregate(in, nil, nil)
	small := got.Reports[0]
	if small.Direction != "low" {
		t.Errorf("1/6 target → low; got %q", small.Direction)
	}
	if got.NewThresholds == nil {
		t.Fatal("expected NewThresholds on drift")
	}
	if got.NewThresholds.SmallMax <= got.Current.SmallMax {
		t.Errorf("SmallMax should RAISE on over-estimate; got %d (was %d)",
			got.NewThresholds.SmallMax, got.Current.SmallMax)
	}
}

// ── Aggregate: per-bucket independence ──────────────────────────

func TestAggregate_OnlyDriftedBucketShifts(t *testing.T) {
	in := []Sample{}
	// Small: 6 samples at the target → no drift.
	for i := 0; i < 6; i++ {
		in = append(in, sample(enrichment.SizeSmall, 30*time.Minute))
	}
	// Medium: 6 samples at 12h — 3× the 4h target. Shift
	// MediumMax down.
	for i := 0; i < 6; i++ {
		in = append(in, sample(enrichment.SizeMedium, 12*time.Hour))
	}
	got := Aggregate(in, nil, nil)
	if got.Reports[0].Direction != "ok" {
		t.Errorf("small bucket should be ok; got %q", got.Reports[0].Direction)
	}
	if got.Reports[1].Direction != "high" {
		t.Errorf("medium bucket should be high; got %q", got.Reports[1].Direction)
	}
	if got.NewThresholds == nil {
		t.Fatal("expected NewThresholds")
	}
	if got.NewThresholds.SmallMax != got.Current.SmallMax {
		t.Errorf("small unchanged; got %d", got.NewThresholds.SmallMax)
	}
	if got.NewThresholds.MediumMax >= got.Current.MediumMax {
		t.Errorf("medium should lower; got %d (was %d)",
			got.NewThresholds.MediumMax, got.Current.MediumMax)
	}
}

// ── Aggregate: invariants ───────────────────────────────────────

func TestAggregate_KeepsSmallMaxLeqMediumMax(t *testing.T) {
	// Construct a pathological run where small drifts down and
	// medium doesn't move; the suggester must NOT let SmallMax
	// rise above MediumMax.
	current := enrichment.SizeHeuristicThresholds{SmallMax: 100, MediumMax: 100}
	in := []Sample{}
	for i := 0; i < 6; i++ {
		// 5 min in small bucket → over-estimate → suggest raising
		// SmallMax (which is already at MediumMax).
		in = append(in, sample(enrichment.SizeSmall, 5*time.Minute))
	}
	got := Aggregate(in, nil, &current)
	if got.NewThresholds == nil {
		t.Fatal("expected suggestion")
	}
	if got.NewThresholds.SmallMax > got.NewThresholds.MediumMax {
		t.Errorf("SmallMax must not exceed MediumMax; got %+v", got.NewThresholds)
	}
}

func TestAggregate_DroppedSamplesDontInflate(t *testing.T) {
	// Negative duration + invalid bucket: aggregator must drop
	// rather than count them.
	in := []Sample{
		{Predicted: enrichment.SizeSmall, Actual: -time.Hour},
		{Predicted: "xxl", Actual: time.Hour},
		// Five legit small samples near target.
		sample(enrichment.SizeSmall, 30*time.Minute),
		sample(enrichment.SizeSmall, 30*time.Minute),
		sample(enrichment.SizeSmall, 30*time.Minute),
		sample(enrichment.SizeSmall, 30*time.Minute),
		sample(enrichment.SizeSmall, 30*time.Minute),
	}
	got := Aggregate(in, nil, nil)
	if got.Reports[0].SampleCount != 5 {
		t.Errorf("dropped samples leaked into count: %+v", got.Reports[0])
	}
}

// ── Aggregate: target override ──────────────────────────────────

func TestAggregate_RespectsTargetOverride(t *testing.T) {
	// Override target so a 2-hour median lands as "ok" instead of
	// "high" (which it would be against the default 30-min small
	// target).
	custom := BucketTargets{
		Small:  2 * time.Hour,
		Medium: DefaultBucketTargets.Medium,
		Large:  DefaultBucketTargets.Large,
	}
	in := []Sample{}
	for i := 0; i < 6; i++ {
		in = append(in, sample(enrichment.SizeSmall, 2*time.Hour))
	}
	got := Aggregate(in, &custom, nil)
	if got.Reports[0].Direction != "ok" {
		t.Errorf("custom target ignored; got %q", got.Reports[0].Direction)
	}
}
