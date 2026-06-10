package phase

import (
	"errors"
	"testing"
)

func TestPhase_IsValid(t *testing.T) {
	cases := []struct {
		p     Phase
		valid bool
	}{
		{PhaseIdeation, true},
		{PhaseDesign, true},
		{PhaseBuilding, true},
		{PhaseValidating, true},
		{PhaseShipping, true},
		{PhaseCommercialization, true},
		{Phase(""), false},
		{Phase("not-a-phase"), false},
		{Phase("Ideation"), false}, // case-sensitive
	}
	for _, tc := range cases {
		if got := tc.p.IsValid(); got != tc.valid {
			t.Errorf("Phase(%q).IsValid() = %v, want %v", tc.p, got, tc.valid)
		}
	}
}

func TestPhase_Validate(t *testing.T) {
	if err := PhaseDesign.Validate(); err != nil {
		t.Errorf("Validate(design) = %v, want nil", err)
	}
	err := Phase("bogus").Validate()
	if err == nil {
		t.Fatal("Validate(bogus) = nil, want error")
	}
	if !errors.Is(err, ErrInvalidPhase) {
		t.Errorf("Validate(bogus) error not wrapped with ErrInvalidPhase: %v", err)
	}
}

func TestAll_ReturnsCanonicalSet(t *testing.T) {
	got := All()
	want := []Phase{
		PhaseIdeation, PhaseDesign, PhaseBuilding,
		PhaseValidating, PhaseShipping, PhaseCommercialization,
	}
	if len(got) != len(want) {
		t.Fatalf("All() len = %d, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("All()[%d] = %q, want %q", i, got[i], p)
		}
	}
	// Returned slice MUST be a copy — mutating it must not affect the
	// next call.
	got[0] = PhaseShipping
	if All()[0] != PhaseIdeation {
		t.Error("All() leaked internal slice; mutation observed on second call")
	}
}

func TestCanTransition_LegalEdges(t *testing.T) {
	legal := []struct{ from, to Phase }{
		// Forward chain.
		{PhaseIdeation, PhaseDesign},
		{PhaseDesign, PhaseBuilding},
		{PhaseBuilding, PhaseValidating},
		{PhaseValidating, PhaseShipping},
		{PhaseShipping, PhaseCommercialization},
		// Lateral / re-open.
		{PhaseDesign, PhaseIdeation},
		{PhaseBuilding, PhaseDesign},
		{PhaseValidating, PhaseDesign},
		{PhaseValidating, PhaseBuilding},
		{PhaseShipping, PhaseValidating},
		{PhaseCommercialization, PhaseShipping},
	}
	for _, tc := range legal {
		if !CanTransition(tc.from, tc.to) {
			t.Errorf("CanTransition(%s, %s) = false, want true", tc.from, tc.to)
		}
	}
}

func TestCanTransition_IllegalEdges(t *testing.T) {
	illegal := []struct{ from, to Phase }{
		// Reflexive — Advance returns ErrSamePhase, CanTransition is
		// false (handlers test for the no-op path explicitly).
		{PhaseIdeation, PhaseIdeation},
		{PhaseShipping, PhaseShipping},

		// Skip-forward jumps that must go through intermediate phases.
		{PhaseIdeation, PhaseBuilding},
		{PhaseIdeation, PhaseShipping},
		{PhaseDesign, PhaseValidating},
		{PhaseDesign, PhaseShipping},
		{PhaseBuilding, PhaseShipping},
		{PhaseBuilding, PhaseCommercialization},

		// Backwards from terminal phase to early phases.
		{PhaseCommercialization, PhaseIdeation},
		{PhaseCommercialization, PhaseDesign},
		{PhaseCommercialization, PhaseBuilding},
		{PhaseCommercialization, PhaseValidating},

		// Backwards skip — shipping cannot drop to design directly.
		{PhaseShipping, PhaseDesign},
		{PhaseShipping, PhaseBuilding},
		{PhaseShipping, PhaseIdeation},

		// Invalid phase tokens.
		{Phase("bogus"), PhaseDesign},
		{PhaseDesign, Phase("bogus")},
	}
	for _, tc := range illegal {
		if CanTransition(tc.from, tc.to) {
			t.Errorf("CanTransition(%s, %s) = true, want false", tc.from, tc.to)
		}
	}
}

func TestLegalTransitionsFrom(t *testing.T) {
	got := LegalTransitionsFrom(PhaseValidating)
	want := []Phase{PhaseDesign, PhaseBuilding, PhaseShipping}
	if len(got) != len(want) {
		t.Fatalf("LegalTransitionsFrom(validating) = %v, want %v", got, want)
	}
	// Mutating returned slice must not corrupt internal table.
	got[0] = PhaseCommercialization
	again := LegalTransitionsFrom(PhaseValidating)
	if again[0] != PhaseDesign {
		t.Error("LegalTransitionsFrom returned an aliased internal slice")
	}

	// Unknown phase: empty slice, not nil-panic.
	if got := LegalTransitionsFrom(Phase("nope")); len(got) != 0 {
		t.Errorf("LegalTransitionsFrom(nope) = %v, want empty", got)
	}
}
