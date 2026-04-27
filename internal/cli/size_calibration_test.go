// Tests for `gemba size-calibration` (gm-v5z2.8b). Pure aggregation
// is exercised in internal/planner/sizecalibration; here we cover
// the CLI's stdin → JSON → text/JSON pipeline + flag handling.

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/enrichment"
	"github.com/MikeBengtson/gemba/internal/planner/sizecalibration"
)

func runSizeCalibrationCmd(t *testing.T, in SizeCalibrationInput, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{})
	root.SetArgs(append([]string{"size-calibration"}, args...))
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

func mediumSamples(predicted enrichment.EstimatedSize, actualSeconds float64, n int) []SizeCalibrationSample {
	out := make([]SizeCalibrationSample, n)
	for i := range out {
		out[i] = SizeCalibrationSample{
			BeadID:        "gm-test",
			Predicted:     predicted,
			ActualSeconds: actualSeconds,
		}
	}
	return out
}

// ── happy path / no drift ───────────────────────────────────────

func TestSizeCalibration_NoDriftPrintsHelpfulFooter(t *testing.T) {
	in := SizeCalibrationInput{
		Samples: mediumSamples(enrichment.SizeSmall, 28*60, 6),
	}
	got, err := runSizeCalibrationCmd(t, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "no drift") {
		t.Errorf("expected 'no drift' footer; got %q", got)
	}
}

// ── high drift surfaces threshold shift ─────────────────────────

func TestSizeCalibration_HighDriftPrintsNewThresholds(t *testing.T) {
	// 90 min / 30 min target = 3× → drift, lower SmallMax.
	in := SizeCalibrationInput{
		Samples: mediumSamples(enrichment.SizeSmall, 90*60, 6),
	}
	got, err := runSizeCalibrationCmd(t, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "suggested threshold shift") {
		t.Errorf("expected threshold shift in text output: %s", got)
	}
	if !strings.Contains(got, "SmallMax=") {
		t.Errorf("expected SmallMax in output: %s", got)
	}
}

// ── JSON envelope ───────────────────────────────────────────────

func TestSizeCalibration_JSONEnvelope(t *testing.T) {
	in := SizeCalibrationInput{
		Samples: mediumSamples(enrichment.SizeMedium, 12*60*60, 6),
	}
	got, err := runSizeCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s sizecalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v\noutput=%s", err, got)
	}
	if len(s.Reports) != 3 {
		t.Errorf("expected 3 bucket reports; got %d", len(s.Reports))
	}
	// Medium bucket should be flagged "high".
	var medium *sizecalibration.BucketReport
	for i := range s.Reports {
		if s.Reports[i].Bucket == enrichment.SizeMedium {
			medium = &s.Reports[i]
			break
		}
	}
	if medium == nil || medium.Direction != "high" {
		t.Errorf("medium bucket should be high; got %+v", medium)
	}
}

// ── insufficient signal ─────────────────────────────────────────

func TestSizeCalibration_InsufficientSignalIsCalledOut(t *testing.T) {
	// 3 samples → below MinSamplesPerBucket (5).
	in := SizeCalibrationInput{
		Samples: mediumSamples(enrichment.SizeSmall, 30*60, 3),
	}
	got, err := runSizeCalibrationCmd(t, in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "insufficient") {
		t.Errorf("expected insufficient direction in output: %s", got)
	}
}

// ── targets override ────────────────────────────────────────────

func TestSizeCalibration_TargetsOverrideThreaded(t *testing.T) {
	// 2-hour median against a 2-hour target → ok.
	in := SizeCalibrationInput{
		Samples: mediumSamples(enrichment.SizeSmall, 2*60*60, 6),
		Targets: &SizeCalibrationTargets{
			SmallSeconds:  2 * 60 * 60,
			MediumSeconds: 4 * 60 * 60,
			LargeSeconds:  24 * 60 * 60,
		},
	}
	got, err := runSizeCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s sizecalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Reports[0].Direction != "ok" {
		t.Errorf("custom target should mark small as ok; got %q", s.Reports[0].Direction)
	}
}

// ── current-thresholds round-trip ───────────────────────────────

func TestSizeCalibration_RespectsCurrentThresholds(t *testing.T) {
	cur := enrichment.SizeHeuristicThresholds{SmallMax: 999, MediumMax: 9999}
	in := SizeCalibrationInput{
		Samples: mediumSamples(enrichment.SizeSmall, 90*60, 6),
		// pass through to assert we didn't reset to defaults.
		CurrentThresholds: &cur,
	}
	got, err := runSizeCalibrationCmd(t, in, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var s sizecalibration.Suggestion
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Current.SmallMax != 999 || s.Current.MediumMax != 9999 {
		t.Errorf("custom thresholds dropped; got %+v", s.Current)
	}
	if s.NewThresholds == nil {
		t.Fatal("expected drift suggestion")
	}
	if s.NewThresholds.SmallMax >= 999 {
		t.Errorf("SmallMax should lower from 999 on under-estimate; got %d", s.NewThresholds.SmallMax)
	}
}

// ── stdin missing JSON ──────────────────────────────────────────

func TestSizeCalibration_RejectsBadJSON(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"size-calibration"})
	root.SetIn(strings.NewReader("not json"))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected error on bad JSON input")
	}
}

// Sanity check that the help text mentions the min-sample threshold
// the aggregator enforces, so an operator reading --help knows why
// some buckets land as "insufficient".
func TestSizeCalibration_HelpTextNamesMinSamples(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"size-calibration", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	want := fmt.Sprintf("%d", sizecalibration.MinSamplesPerBucket)
	if !strings.Contains(out.String(), want) {
		t.Errorf("help should call out MinSamplesPerBucket=%s: %s", want, out.String())
	}
}
