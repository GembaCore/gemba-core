package enrichment

import (
	"strings"
	"testing"
)

// ── DispatchStatus ─────────────────────────────────────────────

func TestDispatchStatus_IsValidAcceptsClosedSet(t *testing.T) {
	for _, v := range allDispatchStatuses {
		if !v.IsValid() {
			t.Errorf("%q should be valid", v)
		}
	}
}

func TestDispatchStatus_IsValidRejectsEmptyAndUnknown(t *testing.T) {
	for _, v := range []DispatchStatus{"", "ready ", "READY", "blocked", "in-progress"} {
		if v.IsValid() {
			t.Errorf("%q should be invalid", v)
		}
	}
}

func TestDispatchStatus_IsReadyOnlyForReady(t *testing.T) {
	if !DispatchReady.IsReady() {
		t.Error("DispatchReady should be ready")
	}
	for _, v := range []DispatchStatus{
		DispatchAwaitingDesign, DispatchAwaitingVendor,
		DispatchAwaitingReview, DispatchNotNow, "",
	} {
		if v.IsReady() {
			t.Errorf("%q should not be ready", v)
		}
	}
}

func TestParseDispatchStatus_AcceptsValidWithNormalisation(t *testing.T) {
	cases := []struct {
		in   string
		want DispatchStatus
	}{
		{"ready", DispatchReady},
		{"READY", DispatchReady},
		{"  awaiting-design  ", DispatchAwaitingDesign},
		{"not-now", DispatchNotNow},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDispatchStatus(tc.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseDispatchStatus_RejectsEmpty(t *testing.T) {
	_, err := ParseDispatchStatus("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "expected one of") {
		t.Errorf("error must list valid set: %v", err)
	}
}

func TestParseDispatchStatus_RejectsUnknownAndListsValidSet(t *testing.T) {
	_, err := ParseDispatchStatus("blocked")
	if err == nil {
		t.Fatal("expected error for unknown value")
	}
	if !strings.Contains(err.Error(), "ready") || !strings.Contains(err.Error(), "not-now") {
		t.Errorf("error must list closed set: %v", err)
	}
}

func TestDispatchStatusOrDefault_FillsEmpty(t *testing.T) {
	if got := DispatchStatusOrDefault(""); got != DispatchReady {
		t.Errorf("empty → %q, want %q", got, DispatchReady)
	}
	if got := DispatchStatusOrDefault(DispatchNotNow); got != DispatchNotNow {
		t.Errorf("explicit value should round-trip; got %q", got)
	}
}

// ── EstimatedSize ──────────────────────────────────────────────

func TestEstimatedSize_RankIsTotal(t *testing.T) {
	if SizeSmall.Rank() >= SizeMedium.Rank() {
		t.Errorf("small must rank below medium")
	}
	if SizeMedium.Rank() >= SizeLarge.Rank() {
		t.Errorf("medium must rank below large")
	}
	if EstimatedSize("xl").Rank() != 0 {
		t.Errorf("unknown size must rank 0 (sentinel)")
	}
}

func TestEstimatedSize_IsValid(t *testing.T) {
	for _, v := range allEstimatedSizes {
		if !v.IsValid() {
			t.Errorf("%q should be valid", v)
		}
	}
	for _, v := range []EstimatedSize{"", "tiny", "huge", "Small"} {
		if v.IsValid() {
			t.Errorf("%q should be invalid", v)
		}
	}
}

func TestParseEstimatedSize_AcceptsValid(t *testing.T) {
	cases := []struct {
		in   string
		want EstimatedSize
	}{
		{"small", SizeSmall},
		{"  Medium  ", SizeMedium},
		{"LARGE", SizeLarge},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseEstimatedSize(tc.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseEstimatedSize_RejectsEmptyAndUnknown(t *testing.T) {
	if _, err := ParseEstimatedSize(""); err == nil {
		t.Fatal("expected error for empty")
	}
	if _, err := ParseEstimatedSize("xl"); err == nil {
		t.Fatal("expected error for unknown")
	}
}

// ── EstimateSize heuristic ─────────────────────────────────────

func TestEstimateSize_EmptyBodyReturnsSmall(t *testing.T) {
	if got := EstimateSize("", nil); got != SizeSmall {
		t.Errorf("empty body → %q, want small", got)
	}
	if got := EstimateSize("   \n  \n", nil); got != SizeSmall {
		t.Errorf("whitespace-only body → %q, want small", got)
	}
}

func TestEstimateSize_ShortBodyNoDoDIsSmall(t *testing.T) {
	body := "Quick fix to the auth handler — typo in error message."
	if got := EstimateSize(body, nil); got != SizeSmall {
		t.Errorf("got %q, want small", got)
	}
}

func TestEstimateSize_LongBodyMultipleDoDLinesIsLarge(t *testing.T) {
	body := strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 30) + `

## Definition of Done

- [ ] First criterion
- [ ] Second criterion
- [ ] Third criterion
- [ ] Fourth criterion
`
	if got := EstimateSize(body, nil); got != SizeLarge {
		t.Errorf("got %q, want large", got)
	}
}

func TestEstimateSize_MediumBucket(t *testing.T) {
	// Mid-size body with one explicit DoD line should land in the
	// middle bucket under default thresholds.
	body := strings.Repeat("Mid-length description text. ", 20) + "\n\n- [ ] one criterion\n"
	if got := EstimateSize(body, nil); got != SizeMedium {
		t.Errorf("got %q, want medium", got)
	}
}

func TestEstimateSize_RespectsCustomThresholds(t *testing.T) {
	body := "Short body."
	tight := &SizeHeuristicThresholds{SmallMax: 5, MediumMax: 10}
	if got := EstimateSize(body, tight); got != SizeLarge {
		t.Errorf("with tight thresholds short body should be large; got %q", got)
	}
}

// ── Enrichment integration ─────────────────────────────────────

func TestEnrichment_IsZeroNotZeroWithDispatchStatus(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", DispatchStatus: DispatchAwaitingDesign}
	if e.IsZero() {
		t.Error("dispatch_status should count as signal")
	}
}

func TestEnrichment_IsZeroNotZeroWithEstimatedSize(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", EstimatedSize: SizeLarge}
	if e.IsZero() {
		t.Error("estimated_size should count as signal")
	}
}

func TestEnrichment_IsZeroTrueOnlyWhenAllFieldsEmpty(t *testing.T) {
	e := Enrichment{BeadID: "gm-1"}
	if !e.IsZero() {
		t.Error("naked enrichment should be zero")
	}
}

func TestEnrichment_SetDispatchStatusReturnsCopy(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", Targets: []string{"a.go"}}
	e2 := e.SetDispatchStatus(DispatchNotNow)
	if e.DispatchStatus != "" {
		t.Errorf("original mutated: %+v", e)
	}
	if e2.DispatchStatus != DispatchNotNow {
		t.Errorf("copy missing value: %+v", e2)
	}
	if len(e2.Targets) != 1 || e2.Targets[0] != "a.go" {
		t.Errorf("targets not preserved: %+v", e2.Targets)
	}
}

func TestEnrichment_SetEstimatedSizeReturnsCopy(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", Concepts: []string{"auth"}}
	e2 := e.SetEstimatedSize(SizeLarge)
	if e.EstimatedSize != "" {
		t.Errorf("original mutated: %+v", e)
	}
	if e2.EstimatedSize != SizeLarge {
		t.Errorf("copy missing value: %+v", e2)
	}
	if len(e2.Concepts) != 1 || e2.Concepts[0] != "auth" {
		t.Errorf("concepts not preserved: %+v", e2.Concepts)
	}
}

func TestEnrichment_SetDispatchStatusEmptyClears(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", DispatchStatus: DispatchNotNow}
	e2 := e.SetDispatchStatus("")
	if e2.DispatchStatus != "" {
		t.Errorf("clear failed: %q", e2.DispatchStatus)
	}
}
