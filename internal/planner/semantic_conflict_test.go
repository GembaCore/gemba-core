package planner

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/internal/sourceanalysis"
)

// fakeSA is a tiny in-test SourceAnalysis that returns a fixed
// dependents map. Keeps the semantic-conflict tests independent of
// any real backend.
type fakeSA struct {
	deps      map[string][]sourceanalysis.Target // key = "repo|path"
	available bool
	calls     int
}

func (f *fakeSA) Dependents(_ context.Context, t sourceanalysis.Target) ([]sourceanalysis.Target, error) {
	if !f.available {
		return nil, sourceanalysis.ErrUnavailable
	}
	f.calls++
	return f.deps[t.Repository+"|"+t.Path], nil
}

func (f *fakeSA) Dependencies(context.Context, sourceanalysis.Target) ([]sourceanalysis.Target, error) {
	return nil, nil
}

func (f *fakeSA) PublicContractChanges(context.Context, sourceanalysis.Diff) ([]sourceanalysis.Symbol, error) {
	return nil, nil
}

func (f *fakeSA) Describe(context.Context) (sourceanalysis.Capabilities, error) {
	return sourceanalysis.Capabilities{Backend: "fake", Available: f.available}, nil
}

func TestSemanticConflicts_NoBeadsNoEdges(t *testing.T) {
	got, err := SemanticConflicts(context.Background(), nil, &fakeSA{available: true})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d edges, want 0", len(got))
	}
}

func TestSemanticConflicts_NoOverlapNoEdges(t *testing.T) {
	// Two beads, neither's targets reach the other's targets via
	// Dependents → no conflict.
	sa := &fakeSA{
		available: true,
		deps: map[string][]sourceanalysis.Target{
			"gemba|a.go": {{Repository: "gemba", Path: "x.go"}}, // not in any other bead
			"gemba|b.go": {{Repository: "gemba", Path: "y.go"}},
		},
	}
	got, err := SemanticConflicts(context.Background(), []SemanticBeadInputs{
		{BeadID: "gm-1", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "a.go"}}},
		{BeadID: "gm-2", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "b.go"}}},
	}, sa)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d edges, want 0: %+v", len(got), got)
	}
}

func TestSemanticConflicts_ForwardReachEmitsEdge(t *testing.T) {
	// gm-1 modifies a.go; a.go's dependents include b.go; b.go is
	// in gm-2's targets → semantic conflict.
	sa := &fakeSA{
		available: true,
		deps: map[string][]sourceanalysis.Target{
			"gemba|a.go": {{Repository: "gemba", Path: "b.go"}},
		},
	}
	got, err := SemanticConflicts(context.Background(), []SemanticBeadInputs{
		{BeadID: "gm-1", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "a.go"}}},
		{BeadID: "gm-2", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "b.go"}}},
	}, sa)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(got), got)
	}
	if got[0].A != "gm-1" || got[0].B != "gm-2" {
		t.Errorf("edge endpoints: A=%q B=%q", got[0].A, got[0].B)
	}
	if got[0].Reason == "" {
		t.Errorf("reason is empty")
	}
}

func TestSemanticConflicts_BothDirectionsNamedInReason(t *testing.T) {
	// Mutual dependency: a's changes touch b's targets AND b's
	// changes touch a's targets. Reason names both directions.
	sa := &fakeSA{
		available: true,
		deps: map[string][]sourceanalysis.Target{
			"gemba|a.go": {{Repository: "gemba", Path: "b.go"}},
			"gemba|b.go": {{Repository: "gemba", Path: "a.go"}},
		},
	}
	got, err := SemanticConflicts(context.Background(), []SemanticBeadInputs{
		{BeadID: "gm-1", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "a.go"}}},
		{BeadID: "gm-2", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "b.go"}}},
	}, sa)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1", len(got))
	}
	if !contains(got[0].Reason, "gm-1 changes touch gm-2") {
		t.Errorf("reason missing forward: %q", got[0].Reason)
	}
	if !contains(got[0].Reason, "gm-2 changes touch gm-1") {
		t.Errorf("reason missing reverse: %q", got[0].Reason)
	}
}

func TestSemanticConflicts_CrossRepoDoesNotCollide(t *testing.T) {
	// auth.go in repo A vs auth.go in repo B — different files.
	// SourceAnalysis returns dependents scoped to the right repo,
	// so no cross-repo confusion should fire.
	sa := &fakeSA{
		available: true,
		deps: map[string][]sourceanalysis.Target{
			"repoA|auth.go": {{Repository: "repoA", Path: "session.go"}},
		},
	}
	got, err := SemanticConflicts(context.Background(), []SemanticBeadInputs{
		{BeadID: "gm-A", Targets: []sourceanalysis.Target{{Repository: "repoA", Path: "auth.go"}}},
		{BeadID: "gm-B", Targets: []sourceanalysis.Target{{Repository: "repoB", Path: "auth.go"}}},
	}, sa)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("cross-repo collision: %+v", got)
	}
}

func TestSemanticConflicts_DependentsCacheDeDups(t *testing.T) {
	// Two beads share a target file. SourceAnalysis.Dependents
	// MUST be called once for that file, not twice.
	sa := &fakeSA{
		available: true,
		deps: map[string][]sourceanalysis.Target{
			"gemba|shared.go": {{Repository: "gemba", Path: "x.go"}},
		},
	}
	_, err := SemanticConflicts(context.Background(), []SemanticBeadInputs{
		{BeadID: "gm-1", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "shared.go"}}},
		{BeadID: "gm-2", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "shared.go"}}},
	}, sa)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sa.calls != 1 {
		t.Errorf("Dependents called %d times, want 1 (cache miss)", sa.calls)
	}
}

func TestSemanticConflicts_UnavailableSAReturnsErrUnavailable(t *testing.T) {
	got, err := SemanticConflicts(context.Background(), []SemanticBeadInputs{
		{BeadID: "gm-1", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "a.go"}}},
		{BeadID: "gm-2", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "b.go"}}},
	}, &fakeSA{available: false})
	if !errors.Is(err, sourceanalysis.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if got != nil {
		t.Errorf("got non-nil result with ErrUnavailable: %+v", got)
	}
}

func TestSemanticConflicts_NilSAReturnsErrUnavailable(t *testing.T) {
	got, err := SemanticConflicts(context.Background(), nil, nil)
	if !errors.Is(err, sourceanalysis.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if got != nil {
		t.Errorf("got non-nil result: %+v", got)
	}
}

func TestSemanticConflicts_DeterministicOrder(t *testing.T) {
	sa := &fakeSA{
		available: true,
		deps: map[string][]sourceanalysis.Target{
			"gemba|a.go": {{Repository: "gemba", Path: "b.go"}, {Repository: "gemba", Path: "c.go"}},
			"gemba|b.go": {{Repository: "gemba", Path: "c.go"}},
		},
	}
	beads := []SemanticBeadInputs{
		{BeadID: "gm-c", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "c.go"}}},
		{BeadID: "gm-a", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "a.go"}}},
		{BeadID: "gm-b", Targets: []sourceanalysis.Target{{Repository: "gemba", Path: "b.go"}}},
	}
	first, err := SemanticConflicts(context.Background(), beads, sa)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	second, _ := SemanticConflicts(context.Background(), beads, sa)
	if len(first) != len(second) {
		t.Fatalf("non-deterministic length: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("non-deterministic edge %d: %+v vs %+v", i, first[i], second[i])
		}
	}
	// Sorted by (A, B): gm-a→gm-b, gm-a→gm-c, gm-b→gm-c.
	wantPairs := [][2]string{{"gm-a", "gm-b"}, {"gm-a", "gm-c"}, {"gm-b", "gm-c"}}
	if len(first) != 3 {
		t.Fatalf("got %d edges, want 3", len(first))
	}
	for i, want := range wantPairs {
		if first[i].A != want[0] || first[i].B != want[1] {
			t.Errorf("edge %d: (%s,%s), want (%s,%s)", i, first[i].A, first[i].B, want[0], want[1])
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
