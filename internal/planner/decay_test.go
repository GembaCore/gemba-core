package planner

import (
	"math"
	"testing"
)

func almostEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	const tol = 1e-9
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func TestDecayWeight_MostRecentIsOne(t *testing.T) {
	almostEqual(t, "i=n-0", DecayWeight(0, 5), 1.0)
}

func TestDecayWeight_OneHalfLifeBack(t *testing.T) {
	// One half-life back → exactly 0.5.
	almostEqual(t, "i=n-h", DecayWeight(5, 5), 0.5)
}

func TestDecayWeight_TwoHalfLivesBack(t *testing.T) {
	// Two half-lives back → 0.25.
	almostEqual(t, "i=n-2h", DecayWeight(10, 5), 0.25)
}

func TestDecayWeight_ZeroHalfLifeFallsBackToDefault(t *testing.T) {
	// halfLife = 0 should fall through to DefaultDecayHalfLife (5).
	almostEqual(t, "h=0 → default 5", DecayWeight(5, 0), 0.5)
}

func TestDecayWeight_NegativeDistanceClampedToZero(t *testing.T) {
	// "Future" events treated as right-now. Defends against
	// off-by-one bugs in the caller's distance computation.
	almostEqual(t, "negative dist", DecayWeight(-3, 5), 1.0)
}

func TestDecayConcepts_EmptyEventsReturnsEmptyNonNilMap(t *testing.T) {
	out := DecayConcepts(nil, 5)
	if out == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(out) != 0 {
		t.Errorf("expected empty map, got %+v", out)
	}
}

func TestDecayConcepts_SingleEventScoresOne(t *testing.T) {
	out := DecayConcepts([]EventContribution{
		{Concepts: []ConceptTag{"auth"}},
	}, 5)
	almostEqual(t, "single most-recent event", out["auth"], 1.0)
}

func TestDecayConcepts_TwoEventsSameTagSumsWithDecay(t *testing.T) {
	// events[0]=oldest (5 events back at h=5 → 0.5);
	// events[1]=most-recent (factor 1.0). Sum = 1.5.
	events := []EventContribution{
		{Concepts: []ConceptTag{"auth"}}, // oldest
	}
	// Pad with 5 newer events that don't carry "auth" so the
	// oldest is exactly h=5 events back from the most-recent.
	for i := 0; i < 5; i++ {
		events = append(events, EventContribution{Concepts: []ConceptTag{"other"}})
	}
	// Now make the most-recent also "auth".
	events[len(events)-1] = EventContribution{Concepts: []ConceptTag{"auth"}}
	out := DecayConcepts(events, 5)
	almostEqual(t, "auth = 0.5 (oldest) + 1.0 (newest)", out["auth"], 1.5)
}

func TestDecayConcepts_EventWeightAppliesAsMultiplier(t *testing.T) {
	out := DecayConcepts([]EventContribution{
		{Concepts: []ConceptTag{"auth"}, Weight: 0.5},
	}, 5)
	almostEqual(t, "weight 0.5 most-recent", out["auth"], 0.5)
}

func TestDecayConcepts_ZeroWeightTreatedAsOne(t *testing.T) {
	// Zero is the Go zero-value, so EventContribution{...} with
	// no explicit Weight should still count as 1.0 — otherwise
	// every default-constructed event would silently drop.
	out := DecayConcepts([]EventContribution{
		{Concepts: []ConceptTag{"auth"}},
	}, 5)
	almostEqual(t, "default weight", out["auth"], 1.0)
}

func TestDecayFiles_TracksFileWeightsTheSameWay(t *testing.T) {
	out := DecayFiles([]EventContribution{
		{Files: []string{"src/auth.go"}}, // oldest
		{Files: []string{"src/auth.go"}}, // most-recent
	}, 5)
	// One event back at h=5 → 0.5^(1/5) ≈ 0.8706.
	want := 1.0 + math.Pow(0.5, 1.0/5.0)
	almostEqual(t, "files decay", out["src/auth.go"], want)
}

func TestAgeProfile_HalvesAfterHalfLifeWorthOfAging(t *testing.T) {
	in := map[ConceptTag]float64{"auth": 1.0}
	cur := in
	// Age the profile 5 times — a full half-life. Result should
	// be ~0.5.
	for i := 0; i < 5; i++ {
		cur = AgeProfile(cur, 5)
	}
	almostEqual(t, "5 ages at h=5 → 0.5", cur["auth"], 0.5)
}

func TestAgeProfile_NilInputReturnsNil(t *testing.T) {
	if got := AgeProfile(nil, 5); got != nil {
		t.Errorf("nil input → nil output; got %+v", got)
	}
}

func TestAgeProfile_DoesNotMutateInput(t *testing.T) {
	in := map[ConceptTag]float64{"auth": 1.0}
	_ = AgeProfile(in, 5)
	if in["auth"] != 1.0 {
		t.Errorf("AgeProfile mutated input; auth=%v", in["auth"])
	}
}

func TestAgeProfile_ZeroHalfLifeFallsBackToDefault(t *testing.T) {
	in := map[ConceptTag]float64{"auth": 1.0}
	out := AgeProfile(in, 0)
	// Default is 5 → factor = 0.5^(1/5) ≈ 0.8706.
	almostEqual(t, "h=0 → default", out["auth"], math.Pow(0.5, 1.0/5.0))
}

func TestMergeContribution_AppendsAtFullWeight(t *testing.T) {
	in := map[ConceptTag]float64{"auth": 0.5}
	out := MergeContribution(in, []ConceptTag{"auth", "spa-routing"}, 0)
	almostEqual(t, "auth merged", out["auth"], 1.5)
	almostEqual(t, "spa-routing new", out["spa-routing"], 1.0)
	// Input must not be mutated.
	if in["auth"] != 0.5 {
		t.Errorf("input mutated")
	}
}

func TestMergeContribution_RespectsExplicitWeight(t *testing.T) {
	out := MergeContribution(nil, []ConceptTag{"auth"}, 0.25)
	almostEqual(t, "weight 0.25", out["auth"], 0.25)
}

func TestMergeFileContribution_AppendsAtFullWeight(t *testing.T) {
	out := MergeFileContribution(map[string]float64{"src/auth.go": 0.5}, []string{"src/auth.go", "src/login.go"}, 0)
	almostEqual(t, "merged", out["src/auth.go"], 1.5)
	almostEqual(t, "new", out["src/login.go"], 1.0)
}

// Round-trip property: AgeProfile(profile, h) + MergeContribution
// (..., 1.0) over k iterations equals DecayConcepts on the
// equivalent k-event stream. This is the spec invariant the write
// hooks rely on — they don't keep the event history, just the
// rolling weights.
func TestDecay_RollingMatchesFullRecompute(t *testing.T) {
	const h = 5
	stream := []EventContribution{
		{Concepts: []ConceptTag{"auth"}},
		{Concepts: []ConceptTag{"spa-routing"}},
		{Concepts: []ConceptTag{"auth", "spa-routing"}},
	}
	full := DecayConcepts(stream, h)

	rolling := map[ConceptTag]float64{}
	for _, e := range stream {
		rolling = AgeProfile(rolling, h)
		rolling = MergeContribution(rolling, e.Concepts, 0)
	}
	for k, want := range full {
		almostEqual(t, "rolling vs full: "+string(k), rolling[k], want)
	}
}
