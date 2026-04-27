package phase

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mustState(t *testing.T, p Phase, now time.Time) WorkspaceState {
	t.Helper()
	s, err := InitialState(p, now)
	if err != nil {
		t.Fatalf("InitialState(%s): %v", p, err)
	}
	return s
}

func TestAdvance_HappyPath(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	s := mustState(t, PhaseDesign, now)

	later := now.Add(1 * time.Hour)
	out, err := Advance(s, PhaseBuilding, "operator", "ratify ADR-12", later)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}

	if out.Phase != PhaseBuilding {
		t.Errorf("out.Phase = %s, want building", out.Phase)
	}
	if !out.Since.Equal(later) {
		t.Errorf("out.Since = %v, want %v", out.Since, later)
	}
	if len(out.History) != 1 {
		t.Fatalf("out.History len = %d, want 1", len(out.History))
	}
	tr := out.History[0]
	if tr.From != PhaseDesign || tr.To != PhaseBuilding {
		t.Errorf("Transition From/To = %s→%s, want design→building", tr.From, tr.To)
	}
	if tr.By != "operator" {
		t.Errorf("Transition.By = %q, want operator", tr.By)
	}
	if tr.Reason != "ratify ADR-12" {
		t.Errorf("Transition.Reason = %q", tr.Reason)
	}
	if !tr.At.Equal(later) {
		t.Errorf("Transition.At = %v, want %v", tr.At, later)
	}

	// Input state was not mutated (purity invariant).
	if s.Phase != PhaseDesign {
		t.Errorf("Advance mutated input state: s.Phase = %s", s.Phase)
	}
	if len(s.History) != 0 {
		t.Errorf("Advance mutated input state: len(s.History) = %d", len(s.History))
	}
}

func TestAdvance_AppendsToHistory(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	s := mustState(t, PhaseIdeation, now)

	s, err := Advance(s, PhaseDesign, "a1", "kickoff", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("design: %v", err)
	}
	s, err = Advance(s, PhaseBuilding, "a2", "ADR-1 ratified", now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	s, err = Advance(s, PhaseValidating, "a3", "feature complete", now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("validating: %v", err)
	}

	if len(s.History) != 3 {
		t.Fatalf("History len = %d, want 3", len(s.History))
	}
	if s.History[0].From != PhaseIdeation || s.History[2].To != PhaseValidating {
		t.Errorf("history not in order: %+v", s.History)
	}
	if s.Phase != PhaseValidating {
		t.Errorf("final phase = %s, want validating", s.Phase)
	}
}

func TestAdvance_RejectsInvalidTarget(t *testing.T) {
	now := time.Now().UTC()
	s := mustState(t, PhaseDesign, now)
	_, err := Advance(s, Phase("bogus"), "op", "x", now)
	if err == nil {
		t.Fatal("Advance(bogus) = nil err, want error")
	}
	if !errors.Is(err, ErrInvalidPhase) {
		t.Errorf("err not ErrInvalidPhase: %v", err)
	}
}

func TestAdvance_RejectsSamePhase(t *testing.T) {
	now := time.Now().UTC()
	s := mustState(t, PhaseBuilding, now)
	_, err := Advance(s, PhaseBuilding, "op", "x", now)
	if err == nil {
		t.Fatal("Advance(same) = nil err, want error")
	}
	if !errors.Is(err, ErrSamePhase) {
		t.Errorf("err not ErrSamePhase: %v", err)
	}
}

func TestAdvance_RejectsIllegalTransition(t *testing.T) {
	now := time.Now().UTC()
	// ideation → shipping is not in the graph.
	s := mustState(t, PhaseIdeation, now)
	_, err := Advance(s, PhaseShipping, "op", "skip ahead", now)
	if err == nil {
		t.Fatal("Advance(ideation→shipping) = nil err, want error")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("err not ErrInvalidTransition: %v", err)
	}
}

func TestAdvance_TrimsReason(t *testing.T) {
	now := time.Now().UTC()
	s := mustState(t, PhaseDesign, now)
	out, err := Advance(s, PhaseBuilding, "op", "  ready to ship  ", now)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if out.History[0].Reason != "ready to ship" {
		t.Errorf("Reason = %q, want trimmed", out.History[0].Reason)
	}
}

func TestInitialState_DefaultsAndValidates(t *testing.T) {
	now := time.Now().UTC()
	s, err := InitialState("", now)
	if err != nil {
		t.Fatalf("InitialState(empty): %v", err)
	}
	if s.Phase != PhaseIdeation {
		t.Errorf("default phase = %s, want ideation", s.Phase)
	}
	if !s.Since.Equal(now) {
		t.Errorf("Since = %v, want %v", s.Since, now)
	}

	if _, err := InitialState(Phase("bogus"), now); err == nil {
		t.Fatal("InitialState(bogus) = nil err, want error")
	}
}

func TestMemoryStore_GetSetRoundTrip(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	store := NewMemoryStore()
	if _, err := store.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("first Get on fresh store = %v, want ErrNotFound", err)
	}

	seed, _ := InitialState(PhaseBuilding, now)
	if err := store.Set(ctx, seed); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Phase != PhaseBuilding {
		t.Errorf("got.Phase = %s, want building", got.Phase)
	}

	// Mutating the retrieved copy MUST NOT affect the store.
	got.Phase = PhaseShipping
	got.History = append(got.History, Transition{From: PhaseBuilding, To: PhaseShipping})
	again, _ := store.Get(ctx)
	if again.Phase != PhaseBuilding {
		t.Error("MemoryStore.Get returned an aliased state; mutation observed on second Get")
	}
	if len(again.History) != 0 {
		t.Errorf("again.History len = %d, want 0", len(again.History))
	}
}

func TestNewSeededMemoryStore(t *testing.T) {
	now := time.Now().UTC()
	store, err := NewSeededMemoryStore(PhaseDesign, now)
	if err != nil {
		t.Fatalf("NewSeededMemoryStore: %v", err)
	}
	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Phase != PhaseDesign {
		t.Errorf("got.Phase = %s, want design", got.Phase)
	}
}
