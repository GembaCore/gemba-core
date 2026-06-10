package planner

import (
	"testing"
	"time"
)

func TestShouldRecycle_NoSignalKeepsSession(t *testing.T) {
	got := ShouldRecycle(RecycleInputs{})
	if got.Recycle {
		t.Errorf("empty inputs should keep; got recycle=%v", got)
	}
	if got.Reason != RecycleOK {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestShouldRecycle_HighPressureBelowMedianFiresRule1(t *testing.T) {
	got := ShouldRecycle(RecycleInputs{
		Health:                 &SessionHealth{ContextPressure: 0.9},
		IncomingAffinityScores: AffinityScores{Combined: 0.20},
		ReadySetAffinities:     []float64{0.20, 0.50, 0.80},
	})
	if !got.Recycle {
		t.Fatalf("expected recycle; got %+v", got)
	}
	if got.Reason != RecyclePressureBelowMedian {
		t.Errorf("reason = %q", got.Reason)
	}
	if got.DriverScores.MedianAffinity != 0.50 {
		t.Errorf("median = %v", got.DriverScores.MedianAffinity)
	}
}

func TestShouldRecycle_HighPressureAboveMedianKeeps(t *testing.T) {
	// Pressure high but the bead is one of the warmer matches —
	// don't waste the priming.
	got := ShouldRecycle(RecycleInputs{
		Health:                 &SessionHealth{ContextPressure: 0.9},
		IncomingAffinityScores: AffinityScores{Combined: 0.85},
		ReadySetAffinities:     []float64{0.20, 0.50, 0.80, 0.85},
	})
	if got.Recycle {
		t.Errorf("rule 1 fired despite above-median affinity: %+v", got)
	}
}

func TestShouldRecycle_PressureExactlyAtThresholdKeeps(t *testing.T) {
	// > 0.85, not >= — exactly at the threshold should NOT fire.
	got := ShouldRecycle(RecycleInputs{
		Health:                 &SessionHealth{ContextPressure: 0.85},
		IncomingAffinityScores: AffinityScores{Combined: 0.10},
		ReadySetAffinities:     []float64{0.50},
	})
	if got.Recycle {
		t.Errorf("threshold edge fired: %+v", got)
	}
}

func TestShouldRecycle_HighDriftWithLowOverlapFiresRule2(t *testing.T) {
	got := ShouldRecycle(RecycleInputs{
		Health: &SessionHealth{ConceptDrift: 0.8},
		IncomingBead: AffinityBeadInputs{
			Concepts: []ConceptTag{"new-area-1", "new-area-2"},
		},
		SessionConcepts: map[ConceptTag]float64{
			"old-area-1": 0.4,
		},
	})
	if !got.Recycle {
		t.Fatalf("expected recycle; got %+v", got)
	}
	if got.Reason != RecycleDriftAndMismatch {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestShouldRecycle_HighDriftWithHighOverlapKeeps(t *testing.T) {
	// Drift high but the bead overlaps the session's history —
	// the next bead is bringing the session back to its concept
	// home; not the time to recycle.
	got := ShouldRecycle(RecycleInputs{
		Health: &SessionHealth{ConceptDrift: 0.8},
		IncomingBead: AffinityBeadInputs{
			Concepts: []ConceptTag{"auth", "auth-tokens", "auth-refresh"},
		},
		SessionConcepts: map[ConceptTag]float64{
			"auth": 0.5, "auth-tokens": 0.3, "auth-refresh": 0.2,
		},
	})
	if got.Recycle {
		t.Errorf("rule 2 fired despite full overlap: %+v", got)
	}
}

func TestShouldRecycle_LongTimeOnTaskNewAreaFiresRule3(t *testing.T) {
	got := ShouldRecycle(RecycleInputs{
		Health: &SessionHealth{TimeOnTask: 5 * time.Hour},
		IncomingBead: AffinityBeadInputs{
			Concepts: []ConceptTag{"never-seen-this-area"},
		},
		SessionConcepts: map[ConceptTag]float64{"old-area": 1.0},
	})
	if !got.Recycle {
		t.Fatalf("expected recycle; got %+v", got)
	}
	if got.Reason != RecycleTimeOnTaskNewArea {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestShouldRecycle_LongTimeOnTaskFamiliarAreaKeeps(t *testing.T) {
	// 5 hours but the next bead is in a familiar area — keep the
	// warmth.
	got := ShouldRecycle(RecycleInputs{
		Health: &SessionHealth{TimeOnTask: 5 * time.Hour},
		IncomingBead: AffinityBeadInputs{
			Concepts: []ConceptTag{"auth"},
		},
		SessionConcepts: map[ConceptTag]float64{"auth": 1.0},
	})
	if got.Recycle {
		t.Errorf("rule 3 fired despite known concept: %+v", got)
	}
}

func TestShouldRecycle_TimeOnTaskExactlyFourHoursKeeps(t *testing.T) {
	// > 4h, not >= — exactly 4h should not fire.
	got := ShouldRecycle(RecycleInputs{
		Health:          &SessionHealth{TimeOnTask: 4 * time.Hour},
		IncomingBead:    AffinityBeadInputs{Concepts: []ConceptTag{"new"}},
		SessionConcepts: map[ConceptTag]float64{"old": 1.0},
	})
	if got.Recycle {
		t.Errorf("threshold edge fired: %+v", got)
	}
}

func TestShouldRecycle_FirstFiringRuleWinsInPriorityOrder(t *testing.T) {
	// Construct inputs that would fire rules 1 + 2 + 3
	// simultaneously. Rule 1 fires first per the function's
	// evaluation order — this defends the audit-log determinism.
	got := ShouldRecycle(RecycleInputs{
		Health: &SessionHealth{
			ContextPressure: 0.95,
			ConceptDrift:    0.9,
			TimeOnTask:      10 * time.Hour,
		},
		IncomingBead: AffinityBeadInputs{
			Concepts: []ConceptTag{"new-area"},
		},
		IncomingAffinityScores: AffinityScores{Combined: 0.1},
		ReadySetAffinities:     []float64{0.5, 0.6, 0.7},
		SessionConcepts:        map[ConceptTag]float64{"old": 1.0},
	})
	if !got.Recycle || got.Reason != RecyclePressureBelowMedian {
		t.Errorf("expected rule 1 to win; got %+v", got)
	}
}

func TestShouldRecycle_DriversAlwaysPopulated(t *testing.T) {
	// Even when no rule fires, the audit envelope must carry the
	// numbers — the coach UI surfaces "what was the planner
	// looking at?" alongside every keep / recycle decision.
	got := ShouldRecycle(RecycleInputs{
		Health: &SessionHealth{
			ContextPressure: 0.42,
			ConceptDrift:    0.18,
			TimeOnTask:      90 * time.Minute,
		},
		IncomingAffinityScores: AffinityScores{Combined: 0.66},
		ReadySetAffinities:     []float64{0.5, 0.66, 0.8},
	})
	if got.Recycle {
		t.Fatalf("expected keep; got %+v", got)
	}
	if got.DriverScores.ContextPressure != 0.42 {
		t.Errorf("pressure not snapshotted: %+v", got.DriverScores)
	}
	if got.DriverScores.TimeOnTaskHours != 1.5 {
		t.Errorf("time-on-task not snapshotted: %+v", got.DriverScores)
	}
	if got.DriverScores.MedianAffinity != 0.66 {
		t.Errorf("median = %v", got.DriverScores.MedianAffinity)
	}
}

func TestMedianAffinity_OddAndEvenLengths(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
		ok   bool
	}{
		{"empty", nil, 0, false},
		{"single", []float64{0.5}, 0.5, true},
		{"odd unsorted", []float64{0.9, 0.1, 0.5}, 0.5, true},
		{"even unsorted", []float64{0.4, 0.2, 0.8, 0.6}, 0.5, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := medianAffinity(tc.in)
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if ok {
				almostEqual(t, "median", got, tc.want)
			}
		})
	}
}

func TestMedianAffinity_DoesNotMutateInput(t *testing.T) {
	in := []float64{0.9, 0.1, 0.5}
	_, _ = medianAffinity(in)
	if in[0] != 0.9 {
		t.Errorf("input mutated: %+v", in)
	}
}

func TestEvaluateConceptOverlap_FullPartialNone(t *testing.T) {
	session := map[ConceptTag]float64{"auth": 1.0, "spa": 0.5}
	cases := []struct {
		name string
		bead []ConceptTag
		want float64
	}{
		{"empty bead", nil, 0},
		{"full overlap", []ConceptTag{"auth", "spa"}, 1.0},
		{"half overlap", []ConceptTag{"auth", "missing"}, 0.5},
		{"no overlap", []ConceptTag{"new1", "new2"}, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateConceptOverlap(tc.bead, session)
			almostEqual(t, "overlap", got, tc.want)
		})
	}
}

func TestIsNewConceptArea_EmptyBeadFalse(t *testing.T) {
	if isNewConceptArea(nil, map[ConceptTag]float64{"x": 1}) {
		t.Error("empty bead concepts should not flag new area")
	}
}

func TestIsNewConceptArea_EmptyProfileTrueWhenBeadHasConcepts(t *testing.T) {
	if !isNewConceptArea([]ConceptTag{"auth"}, nil) {
		t.Error("empty profile + non-empty bead should be new area")
	}
}

func TestIsNewConceptArea_AnyMissingConceptIsEnough(t *testing.T) {
	session := map[ConceptTag]float64{"auth": 1}
	if !isNewConceptArea([]ConceptTag{"auth", "spa-routing"}, session) {
		t.Error("one missing concept should flag new area")
	}
}

// Recycle constants must not drift without a spec update.
func TestRecycleThresholds_PinnedToSpec(t *testing.T) {
	if RecyclePressureThreshold != 0.85 {
		t.Errorf("pressure threshold = %v, want 0.85 (spec §5.5)", RecyclePressureThreshold)
	}
	if RecycleDriftThreshold != 0.70 {
		t.Errorf("drift threshold = %v, want 0.70 (spec §5.5)", RecycleDriftThreshold)
	}
	if RecycleConceptOverlapMaxForDrift != 0.30 {
		t.Errorf("overlap-for-drift = %v, want 0.30 (spec §5.5)", RecycleConceptOverlapMaxForDrift)
	}
	if RecycleTimeOnTaskHours != 4 {
		t.Errorf("time-on-task threshold = %v, want 4 (spec §5.5)", RecycleTimeOnTaskHours)
	}
}
