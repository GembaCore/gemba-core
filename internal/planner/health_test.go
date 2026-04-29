// Tests for the session-health telemetry surface (gm-s47n.5.1).
// Pure functions — every test fixes inputs and asserts on outputs;
// no I/O, no goroutines.

package planner

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// ── ConceptDrift cosine math ───────────────────────────────────────

func TestConceptDrift_IdenticalVectorsZero(t *testing.T) {
	v := map[ConceptTag]float64{"glob": 0.7, "planner": 0.4}
	got := ConceptDrift(v, v)
	// ConceptDrift normalizes then computes 1 - dot(v̂, v̂); the
	// normalization step introduces float64 roundoff (one ULP at
	// scale 1.0 = ~2.22e-16), so a strict `!= 0` is mis-calibrated.
	// Allow up to 1e-9 — well within "essentially zero" while still
	// catching any real regression to non-trivial drift.
	if got > 1e-9 {
		t.Errorf("ConceptDrift(v, v) = %v; want ≈ 0 (within 1e-9)", got)
	}
}

func TestConceptDrift_OrthogonalVectorsOne(t *testing.T) {
	a := map[ConceptTag]float64{"glob": 1.0}
	b := map[ConceptTag]float64{"planner": 1.0}
	got := ConceptDrift(a, b)
	if got != 1 {
		t.Errorf("ConceptDrift(disjoint) = %v; want 1", got)
	}
}

func TestConceptDrift_PartialOverlap(t *testing.T) {
	// Both vectors weight "glob" but differ on the second concept.
	// Result should be in (0, 1) reflecting partial similarity.
	a := map[ConceptTag]float64{"glob": 1.0, "targets": 1.0}
	b := map[ConceptTag]float64{"glob": 1.0, "planner": 1.0}
	got := ConceptDrift(a, b)
	// Hand-compute: dot=1, |a|=sqrt(2), |b|=sqrt(2), cos=0.5,
	// distance=0.5.
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("ConceptDrift = %v; want ~0.5", got)
	}
}

func TestConceptDrift_ScaleInvariant(t *testing.T) {
	// Cosine similarity is scale-invariant: doubling every weight
	// in one vector must not change the distance. A regression
	// here would mean we're measuring magnitude instead of
	// direction.
	a := map[ConceptTag]float64{"x": 1.0, "y": 2.0}
	b := map[ConceptTag]float64{"x": 2.0, "y": 4.0}
	got := ConceptDrift(a, b)
	if math.Abs(got) > 1e-9 {
		t.Errorf("ConceptDrift on scaled-equal vectors = %v; want 0", got)
	}
}

func TestConceptDrift_EmptyVectorIsZero(t *testing.T) {
	v := map[ConceptTag]float64{"glob": 1.0}
	if got := ConceptDrift(nil, v); got != 0 {
		t.Errorf("ConceptDrift(nil, v) = %v; want 0", got)
	}
	if got := ConceptDrift(v, nil); got != 0 {
		t.Errorf("ConceptDrift(v, nil) = %v; want 0", got)
	}
	if got := ConceptDrift(nil, nil); got != 0 {
		t.Errorf("ConceptDrift(nil, nil) = %v; want 0", got)
	}
}

func TestConceptDrift_ZeroMagnitudeIsZero(t *testing.T) {
	// A non-empty map with all zero weights still has zero norm.
	// Don't NaN; return 0 (no drift detectable).
	a := map[ConceptTag]float64{"x": 0, "y": 0}
	b := map[ConceptTag]float64{"x": 1.0}
	if got := ConceptDrift(a, b); got != 0 {
		t.Errorf("ConceptDrift(zero-magnitude, v) = %v; want 0", got)
	}
}

// ── LastNConceptVector ─────────────────────────────────────────────

type fakeConceptLookup struct {
	byID map[core.WorkItemID]map[ConceptTag]float64
	err  error
}

func (f *fakeConceptLookup) BeadConcepts(_ context.Context, id core.WorkItemID) (map[ConceptTag]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

func TestLastNConceptVector_AggregatesAcrossBeads(t *testing.T) {
	lookup := &fakeConceptLookup{
		byID: map[core.WorkItemID]map[ConceptTag]float64{
			"gm-1": {"glob": 1.0, "planner": 0.5},
			"gm-2": {"glob": 0.5, "targets": 1.0},
		},
	}
	got, err := LastNConceptVector(context.Background(),
		[]core.WorkItemID{"gm-1", "gm-2"}, lookup,
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	want := map[ConceptTag]float64{
		"glob":    1.5,
		"planner": 0.5,
		"targets": 1.0,
	}
	if len(got) != len(want) {
		t.Fatalf("vector len = %d; want %d (got=%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if math.Abs(got[k]-v) > 1e-9 {
			t.Errorf("vector[%s] = %v; want %v", k, got[k], v)
		}
	}
}

func TestLastNConceptVector_NilLookupReturnsEmpty(t *testing.T) {
	got, err := LastNConceptVector(context.Background(),
		[]core.WorkItemID{"gm-1"}, nil,
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("nil lookup should yield empty vector; got %v", got)
	}
}

func TestLastNConceptVector_PerBeadErrorIsSkipped(t *testing.T) {
	// One bead fails; the other still aggregates. Drift is advisory;
	// dropping a single bead doesn't poison the snapshot.
	lookup := &fakeConceptLookup{err: errors.New("transient")}
	got, err := LastNConceptVector(context.Background(),
		[]core.WorkItemID{"gm-1", "gm-2"}, lookup,
	)
	if err != nil {
		t.Fatalf("per-bead errors must not fail the call; got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("all-error lookup should yield empty vector; got %v", got)
	}
}

// ── ComputeHealth ──────────────────────────────────────────────────

func TestComputeHealth_NilSessionReturnsNil(t *testing.T) {
	h, err := ComputeHealth(context.Background(), nil, nil, nil, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h != nil {
		t.Errorf("nil session must yield nil health; got %+v", h)
	}
}

func TestComputeHealth_SessionOnly_TimeOnTask(t *testing.T) {
	sess := &core.Session{StartedAt: startedAt(15 * time.Minute)}
	h, err := ComputeHealth(context.Background(), sess, nil, nil, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h == nil {
		t.Fatal("h is nil")
	}
	if h.TimeOnTask != 15*time.Minute {
		t.Errorf("TimeOnTask = %v; want 15m", h.TimeOnTask)
	}
	if h.ContextPressure != 0 || h.ConceptDrift != 0 {
		t.Errorf("expected zero defaults for missing profile; got %+v", h)
	}
}

func TestComputeHealth_ProfileContextPressure(t *testing.T) {
	sess := &core.Session{StartedAt: startedAt(time.Minute)}
	prof := &SessionProfile{
		ContextPct: 0.3,
	}
	h, err := ComputeHealth(context.Background(), sess, prof, nil, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.ContextPressure != 0.3 {
		t.Errorf("ContextPressure = %v; want 0.3", h.ContextPressure)
	}
}

func TestComputeHealth_ConceptDriftFromLookup(t *testing.T) {
	sess := &core.Session{StartedAt: startedAt(time.Minute)}
	prof := &SessionProfile{
		Concepts:  map[ConceptTag]float64{"glob": 1.0, "targets": 1.0},
		LastBeads: []core.WorkItemID{"gm-1"},
	}
	lookup := &fakeConceptLookup{
		byID: map[core.WorkItemID]map[ConceptTag]float64{
			// Last bead's concepts are orthogonal to the lifetime
			// vector — drift should be 1.0.
			"gm-1": {"planner": 1.0},
		},
	}
	h, err := ComputeHealth(context.Background(), sess, prof, lookup, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if math.Abs(h.ConceptDrift-1.0) > 1e-9 {
		t.Errorf("ConceptDrift = %v; want 1 (orthogonal)", h.ConceptDrift)
	}
}

func TestComputeHealth_NoLookupNoDrift(t *testing.T) {
	sess := &core.Session{StartedAt: startedAt(time.Minute)}
	prof := &SessionProfile{
		Concepts:  map[ConceptTag]float64{"glob": 1.0},
		LastBeads: []core.WorkItemID{"gm-1"},
	}
	h, err := ComputeHealth(context.Background(), sess, prof, nil, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.ConceptDrift != 0 {
		t.Errorf("ConceptDrift = %v; want 0 (no lookup wired)", h.ConceptDrift)
	}
}

func TestComputeHealth_EmptyLastBeadsNoDrift(t *testing.T) {
	sess := &core.Session{StartedAt: startedAt(time.Minute)}
	prof := &SessionProfile{
		Concepts:  map[ConceptTag]float64{"glob": 1.0},
		LastBeads: nil,
	}
	lookup := &fakeConceptLookup{
		byID: map[core.WorkItemID]map[ConceptTag]float64{
			"gm-1": {"planner": 1.0},
		},
	}
	h, err := ComputeHealth(context.Background(), sess, prof, lookup, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.ConceptDrift != 0 {
		t.Errorf("ConceptDrift = %v; want 0 (no recent beads to compare)", h.ConceptDrift)
	}
}

func TestComputeHealth_EmptyLifetimeNoDrift(t *testing.T) {
	// Brand-new session with last beads but no lifetime concepts
	// yet — drift is zero (nothing to drift FROM).
	sess := &core.Session{StartedAt: startedAt(time.Minute)}
	prof := &SessionProfile{
		Concepts:  nil,
		LastBeads: []core.WorkItemID{"gm-1"},
	}
	lookup := &fakeConceptLookup{
		byID: map[core.WorkItemID]map[ConceptTag]float64{
			"gm-1": {"planner": 1.0},
		},
	}
	h, err := ComputeHealth(context.Background(), sess, prof, lookup, fixedNow)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.ConceptDrift != 0 {
		t.Errorf("ConceptDrift = %v; want 0 (no lifetime to compare)", h.ConceptDrift)
	}
}

func TestComputeHealth_NowFallsBackToTimeNow(t *testing.T) {
	sess := &core.Session{StartedAt: time.Now().Add(-time.Second)}
	h, err := ComputeHealth(context.Background(), sess, nil, nil, nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if h.TimeOnTask <= 0 {
		t.Errorf("TimeOnTask = %v; want positive", h.TimeOnTask)
	}
}

// ── OperationalContext integration: BeadConcepts wired through ─────

func TestReadOperationalContext_PropagatesConceptDrift(t *testing.T) {
	// End-to-end: when OperationalContextReaders.BeadConcepts is
	// wired and the profile has a non-trivial concept history, the
	// returned SessionHealth.ConceptDrift is non-zero.
	id, r := fixture()
	// Mutate the fixture's profile to carry a concept history.
	prof := r.Profiles.(fakeProfileLookup).byID[id]
	prof.Concepts = map[ConceptTag]float64{"glob": 1.0, "targets": 1.0}
	prof.LastBeads = []core.WorkItemID{"gm-99"}
	r.BeadConcepts = &fakeConceptLookup{
		byID: map[core.WorkItemID]map[ConceptTag]float64{
			"gm-99": {"planner": 1.0}, // orthogonal to lifetime
		},
	}
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Health == nil {
		t.Fatal("Health is nil")
	}
	if math.Abs(got.Health.ConceptDrift-1.0) > 1e-9 {
		t.Errorf("ConceptDrift = %v; want 1 (orthogonal lifetime vs last-N)",
			got.Health.ConceptDrift)
	}
}

func TestReadOperationalContext_NilBeadConceptsLeavesZeroDrift(t *testing.T) {
	// Default fixture has no BeadConcepts wired; ConceptDrift stays
	// at zero (matches the operational_context.go pre-gm-s47n.5.1
	// behaviour).
	id, r := fixture()
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Health.ConceptDrift != 0 {
		t.Errorf("ConceptDrift = %v; want 0 (no BeadConcepts wired)",
			got.Health.ConceptDrift)
	}
}
