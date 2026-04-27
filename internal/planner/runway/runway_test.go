package runway

import (
	"math"
	"testing"

	"github.com/MikeBengtson/gemba/internal/enrichment"
	"github.com/MikeBengtson/gemba/internal/planner"
)

func almostEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// ── Estimate ───────────────────────────────────────────────────

func TestEstimate_NilHealthYieldsSafestSmall(t *testing.T) {
	r := Estimate(Inputs{})
	if r.Bucket != enrichment.SizeSmall {
		t.Errorf("nil health bucket = %q, want small", r.Bucket)
	}
	if r.Score != 0 {
		t.Errorf("nil health score = %v, want 0", r.Score)
	}
}

func TestEstimate_HealthySessionLandsLarge(t *testing.T) {
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 0.2,
			ConceptDrift:    0.1,
		},
	})
	if r.Bucket != enrichment.SizeLarge {
		t.Errorf("healthy bucket = %q, want large (score=%v)", r.Bucket, r.Score)
	}
	// headroom 0.8 - drift_penalty 0.05 = 0.75
	almostEqual(t, "score", r.Score, 0.75)
}

func TestEstimate_StressedSessionLandsSmall(t *testing.T) {
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 0.85,
			ConceptDrift:    0.6,
		},
	})
	if r.Bucket != enrichment.SizeSmall {
		t.Errorf("stressed bucket = %q (score=%v), want small", r.Bucket, r.Score)
	}
}

func TestEstimate_MidSessionLandsMedium(t *testing.T) {
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 0.45,
			ConceptDrift:    0.2,
		},
	})
	if r.Bucket != enrichment.SizeMedium {
		t.Errorf("mid bucket = %q (score=%v), want medium", r.Bucket, r.Score)
	}
}

func TestEstimate_CalibrationScalarShrinksRunway(t *testing.T) {
	healthy := &planner.SessionHealth{ContextPressure: 0.2, ConceptDrift: 0.1}
	uncalibrated := Estimate(Inputs{Health: healthy})
	overrunner := Estimate(Inputs{Health: healthy, Calibration: 0.5})
	if overrunner.Score >= uncalibrated.Score {
		t.Errorf("0.5 calibration must shrink score: got %v vs %v", overrunner.Score, uncalibrated.Score)
	}
	almostEqual(t, "overrunner score", overrunner.Score, 0.75*0.5)
}

func TestEstimate_NegativeOrZeroCalibrationFallsBackToDefault(t *testing.T) {
	healthy := &planner.SessionHealth{ContextPressure: 0.2, ConceptDrift: 0.1}
	for _, cal := range []float64{0, -1.5} {
		r := Estimate(Inputs{Health: healthy, Calibration: cal})
		almostEqual(t, "fallback score", r.Score, 0.75)
	}
}

func TestEstimate_DriftPenaltyClampsAtZero(t *testing.T) {
	// Pressure low, drift huge → penalty exceeds headroom → score 0.
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 0.1,
			ConceptDrift:    1.0,
		},
	})
	if r.Score < 0 {
		t.Errorf("score should clamp at 0; got %v", r.Score)
	}
	almostEqual(t, "score", r.Score, 0.4) // (0.9 - 0.5*1.0) * 1.0 = 0.4
}

func TestEstimate_OverpressureClampsHeadroomAtZero(t *testing.T) {
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 1.5, // out-of-range; should clamp
			ConceptDrift:    0.0,
		},
	})
	if r.Drivers.Headroom != 0 {
		t.Errorf("over-pressure must clamp headroom to 0; got %v", r.Drivers.Headroom)
	}
	if r.Score != 0 {
		t.Errorf("over-pressure score = %v, want 0", r.Score)
	}
	if r.Bucket != enrichment.SizeSmall {
		t.Errorf("over-pressure bucket = %q, want small", r.Bucket)
	}
}

func TestEstimate_NegativeHealthClampsAtZero(t *testing.T) {
	// Garbage in (negative numbers) should not produce garbage out.
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: -0.3,
			ConceptDrift:    -0.5,
		},
	})
	if r.Drivers.ContextPressure != 0 || r.Drivers.ConceptDrift != 0 {
		t.Errorf("negatives must clamp to 0; got %+v", r.Drivers)
	}
}

func TestEstimate_DriversAreSnapshotted(t *testing.T) {
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 0.4,
			ConceptDrift:    0.3,
		},
		Calibration: 0.8,
	})
	almostEqual(t, "pressure", r.Drivers.ContextPressure, 0.4)
	almostEqual(t, "drift", r.Drivers.ConceptDrift, 0.3)
	almostEqual(t, "headroom", r.Drivers.Headroom, 0.6)
	almostEqual(t, "drift_penalty", r.Drivers.DriftPenalty, 0.15)
	almostEqual(t, "calibration", r.Drivers.Calibration, 0.8)
}

func TestEstimate_CustomDriftWeightOverridesDefault(t *testing.T) {
	r := Estimate(Inputs{
		Health: &planner.SessionHealth{
			ContextPressure: 0.0,
			ConceptDrift:    0.4,
		},
		DriftPenaltyWeight: 1.0, // double the default
	})
	// (1.0 - 1.0*0.4) * 1.0 = 0.6
	almostEqual(t, "score", r.Score, 0.6)
}

// ── Bucket boundaries ──────────────────────────────────────────

func TestEstimateWithThresholds_RespectsCustomBoundaries(t *testing.T) {
	healthy := &planner.SessionHealth{ContextPressure: 0.2, ConceptDrift: 0.1}
	tight := BucketThresholds{SmallMax: 0.9, MediumMax: 0.95}
	r := EstimateWithThresholds(Inputs{Health: healthy}, tight)
	if r.Bucket != enrichment.SizeSmall {
		t.Errorf("with tight thresholds even healthy session should fall to small; got %q", r.Bucket)
	}
}

// ── FitsBead ───────────────────────────────────────────────────

func TestFitsBead_LargerSizeDemotesAgainstSmallRunway(t *testing.T) {
	r := Runway{Bucket: enrichment.SizeSmall}
	if r.FitsBead(enrichment.SizeLarge) {
		t.Error("large bead should not fit small runway")
	}
	if r.FitsBead(enrichment.SizeMedium) {
		t.Error("medium bead should not fit small runway")
	}
	if !r.FitsBead(enrichment.SizeSmall) {
		t.Error("small bead must fit small runway")
	}
}

func TestFitsBead_LargeRunwayAcceptsAll(t *testing.T) {
	r := Runway{Bucket: enrichment.SizeLarge}
	for _, b := range []enrichment.EstimatedSize{enrichment.SizeSmall, enrichment.SizeMedium, enrichment.SizeLarge} {
		if !r.FitsBead(b) {
			t.Errorf("large runway should fit %q", b)
		}
	}
}

func TestFitsBead_UnknownSizeFitsAnyRunway(t *testing.T) {
	r := Runway{Bucket: enrichment.SizeSmall}
	if !r.FitsBead("") {
		t.Error("unestimated bead must fit any runway (don't demote unknowns)")
	}
}
