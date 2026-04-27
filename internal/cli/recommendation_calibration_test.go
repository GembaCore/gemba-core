// Tests for `gemba recommendation-calibration` (gm-v5z2.8a). Pure
// aggregation is exercised in internal/planner/reccalibration; here
// we cover the CLI's stdin → JSON → text/JSON pipeline + flag
// handling.

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/planner/reccalibration"
)

func runRecommendationCalibrationCmd(t *testing.T, in RecommendationCalibrationInput, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{})
	root.SetArgs(append([]string{"recommendation-calibration"}, args...))
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	root.SetIn(bytes.NewReader(body))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err = root.Execute()
	return out.String(), err
}

func overrideSamples(n int, rank int, recScore, pickedScore float64) []RecommendationCalibrationSample {
	out := make([]RecommendationCalibrationSample, n)
	for i := range out {
		out[i] = RecommendationCalibrationSample{
			Mode:                "coach",
			RecommendedTopBead:  "rec",
			RecommendedTopScore: recScore,
			PickedBead:          "picked",
			PickedScore:         pickedScore,
			PickedRank:          rank,
		}
	}
	return out
}

// ── happy path / no flags ──────────────────────────────────────

func TestRecommendationCalibration_NoFlagsPrintsClearFooter(t *testing.T) {
	in := RecommendationCalibrationInput{
		Samples: func() []RecommendationCalibrationSample {
			out := make([]RecommendationCalibrationSample, 12)
			for i := range out {
				out[i] = RecommendationCalibrationSample{
					Mode:                "coach",
					RecommendedTopBead:  "rec",
					RecommendedTopScore: 0.8,
					PickedBead:          "rec",
					PickedScore:         0.8,
					PickedRank:          1,
				}
			}
			return out
		}(),
	}
	got, err := runRecommendationCalibrationCmd(t, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "no systematic recommendation drift") {
		t.Errorf("expected 'no systematic recommendation drift' footer; got %q", got)
	}
}

// ── canonical override_top_3 flag ──────────────────────────────

func TestRecommendationCalibration_OverrideTop3Flagged(t *testing.T) {
	in := RecommendationCalibrationInput{
		Samples: overrideSamples(12, 7, 0.90, 0.50),
	}
	got, err := runRecommendationCalibrationCmd(t, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "override_top_3") {
		t.Errorf("expected override_top_3 row; got %s", got)
	}
	if !strings.Contains(got, "FLAG") {
		t.Errorf("expected at least one FLAG marker; got %s", got)
	}
}

// ── auto-mode rows filtered ────────────────────────────────────

func TestRecommendationCalibration_AutoModeFiltered(t *testing.T) {
	samples := overrideSamples(12, 7, 0.90, 0.50)
	for i := range samples {
		samples[i].Mode = "auto"
	}
	in := RecommendationCalibrationInput{Samples: samples}
	got, err := runRecommendationCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s reccalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Findings.SampleCount != 0 {
		t.Errorf("auto-mode rows must be filtered; got %d coach samples", s.Findings.SampleCount)
	}
	if s.Findings.AnyFlag() {
		t.Errorf("auto-only input must not flag; got %+v", s.Findings)
	}
}

// ── intent miss flagged ────────────────────────────────────────

func TestRecommendationCalibration_IntentMissFlagged(t *testing.T) {
	samples := overrideSamples(12, 7, 0.90, 0.50)
	for i := range samples {
		samples[i].IntentSet = true
	}
	in := RecommendationCalibrationInput{Samples: samples}
	got, err := runRecommendationCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s reccalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s.Findings.SystematicIntentMiss {
		t.Errorf("expected SystematicIntentMiss; got %+v", s.Findings)
	}
}

// ── leverage miss flagged via component snapshots ──────────────

func TestRecommendationCalibration_LeverageMissThreaded(t *testing.T) {
	samples := overrideSamples(12, 2, 0.90, 0.50)
	for i := range samples {
		samples[i].RecommendedComponents = &reccalibration.Components{LeverageWeight: 1}
		samples[i].PickedComponents = &reccalibration.Components{LeverageWeight: 5}
	}
	in := RecommendationCalibrationInput{Samples: samples}
	got, err := runRecommendationCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s reccalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s.Findings.SystematicLeverageMiss {
		t.Errorf("expected SystematicLeverageMiss; got %+v", s.Findings)
	}
}

// ── thresholds round-trip ──────────────────────────────────────

func TestRecommendationCalibration_ThresholdsRoundTrip(t *testing.T) {
	in := RecommendationCalibrationInput{
		Samples: overrideSamples(12, 7, 0.51, 0.50),
		// Relax MeanScoreDelta — without this the small delta
		// suppresses the override_top_3 flag.
		Thresholds: &reccalibration.Thresholds{MeanScoreDelta: 0.001},
	}
	got, err := runRecommendationCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s reccalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Thresholds.MeanScoreDelta != 0.001 {
		t.Errorf("MeanScoreDelta override dropped; got %+v", s.Thresholds)
	}
	if !s.Findings.OverrideTop3 {
		t.Errorf("expected OverrideTop3 with relaxed threshold; got %+v", s.Findings)
	}
}

// ── insufficient samples surfaced ──────────────────────────────

func TestRecommendationCalibration_InsufficientSamplesNoted(t *testing.T) {
	in := RecommendationCalibrationInput{
		Samples: overrideSamples(5, 7, 0.90, 0.50),
	}
	got, err := runRecommendationCalibrationCmd(t, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "insufficient samples") {
		t.Errorf("expected insufficient-samples note; got %s", got)
	}
}

// ── bad JSON ───────────────────────────────────────────────────

func TestRecommendationCalibration_RejectsBadJSON(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"recommendation-calibration"})
	root.SetIn(strings.NewReader("not json"))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected error on bad JSON input")
	}
}

// ── invalid time ───────────────────────────────────────────────

func TestRecommendationCalibration_RejectsBadTime(t *testing.T) {
	in := RecommendationCalibrationInput{
		Samples: []RecommendationCalibrationSample{{
			Mode:               "coach",
			At:                 "yesterday",
			RecommendedTopBead: "a",
			PickedBead:         "b",
		}},
	}
	_, err := runRecommendationCalibrationCmd(t, in)
	if err == nil {
		t.Fatal("expected RFC3339 parse error")
	}
}

// ── help text ──────────────────────────────────────────────────

func TestRecommendationCalibration_HelpTextNamesMinSamples(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"recommendation-calibration", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	want := fmt.Sprintf("%d", reccalibration.DefaultThresholds.MinSamples)
	if !strings.Contains(out.String(), want) {
		t.Errorf("help should mention MinSamples=%s: %s", want, out.String())
	}
}
