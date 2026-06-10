// Tests for the bead-set conflict scorer (gm-s47n.4.3). Pure
// function; every test mirrors a planner-input scenario without
// touching the rest of the gemba stack.

package conflicts

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
)

func bead(id core.WorkItemID, ts ...targets.Pattern) Bead {
	return Bead{ID: id, Targets: ts}
}

func TestConflicts_NoTargetOverlap(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/a.go"),
		bead("gm-2", "src/b.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 0 {
		t.Errorf("expected no edges; got %+v", g.Edges)
	}
	if len(g.Beads) != 2 {
		t.Errorf("expected 2 beads in graph; got %d", len(g.Beads))
	}
}

func TestConflicts_TargetOverlapEdge(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/a.go", "lib/x.go"),
		bead("gm-2", "src/a.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge; got %d (%+v)", len(g.Edges), g.Edges)
	}
	e := g.Edges[0]
	if e.From != "gm-1" || e.To != "gm-2" {
		t.Errorf("edge endpoints = (%s, %s); want (gm-1, gm-2)", e.From, e.To)
	}
	if len(e.Reasons) != 1 || e.Reasons[0].Kind != ReasonTargetOverlap {
		t.Errorf("expected single ReasonTargetOverlap; got %+v", e.Reasons)
	}
}

func TestConflicts_PrefixGlobOverlap(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/**"),
		bead("gm-2", "src/foo.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 || g.Edges[0].Reasons[0].Kind != ReasonTargetOverlap {
		t.Errorf("expected target-overlap edge from prefix-glob; got %+v", g.Edges)
	}
}

func TestConflicts_MaybeWithoutFsBecomesMaybeReason(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/*.go"),
		bead("gm-2", "src/*.ts"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge; got %d", len(g.Edges))
	}
	if g.Edges[0].Reasons[0].Kind != ReasonTargetOverlapMaybe {
		t.Errorf("expected Maybe reason; got %v", g.Edges[0].Reasons[0].Kind)
	}
}

func TestConflicts_MaybeIsConflictEscalates(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/*.go"),
		bead("gm-2", "src/*.ts"),
	}, Options{MaybeIsConflict: true})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 || g.Edges[0].Reasons[0].Kind != ReasonTargetOverlap {
		t.Errorf("expected Maybe → TargetOverlap escalation; got %+v", g.Edges)
	}
}

func TestConflicts_FsResolvesMaybe(t *testing.T) {
	fs := fakeFS{
		"src/*.go":   {"src/a.go", "src/share.go"},
		"src/sh*.go": {"src/share.go"},
	}
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/*.go"),
		bead("gm-2", "src/sh*.go"),
	}, Options{FS: fs})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 || g.Edges[0].Reasons[0].Kind != ReasonTargetOverlap {
		t.Errorf("expected FS-resolved TargetOverlap; got %+v", g.Edges)
	}
}

func TestConflicts_SemanticDetector(t *testing.T) {
	sem := fakeSemantic{
		overlapPairs: map[[2]core.WorkItemID]string{
			{"gm-1", "gm-2"}: "Foo's exported symbol referenced by gm-2",
		},
	}
	g, err := Conflicts(context.Background(), []Bead{
		// Disjoint targets so target-overlap doesn't fire.
		bead("gm-1", "src/foo.go"),
		bead("gm-2", "src/bar.go"),
	}, Options{Semantic: sem})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 || g.Edges[0].Reasons[0].Kind != ReasonSemantic {
		t.Errorf("expected semantic-only edge; got %+v", g.Edges)
	}
}

func TestConflicts_WorkspaceCollisionDetector(t *testing.T) {
	ws := fakeWorkspace{
		colliding: map[[2]core.WorkItemID]string{
			{"gm-1", "gm-2"}: "both routed to worktree /tmp/wt-1",
		},
	}
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/foo.go"),
		bead("gm-2", "lib/bar.go"),
	}, Options{WorkspaceCollision: ws})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 || g.Edges[0].Reasons[0].Kind != ReasonWorkspaceCollision {
		t.Errorf("expected workspace-collision edge; got %+v", g.Edges)
	}
}

func TestConflicts_MultipleReasonsOnOneEdge(t *testing.T) {
	sem := fakeSemantic{overlapPairs: map[[2]core.WorkItemID]string{{"gm-1", "gm-2"}: "shared symbol"}}
	ws := fakeWorkspace{colliding: map[[2]core.WorkItemID]string{{"gm-1", "gm-2"}: "shared worktree"}}
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/foo.go"),
		bead("gm-2", "src/foo.go"),
	}, Options{Semantic: sem, WorkspaceCollision: ws})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge; got %d", len(g.Edges))
	}
	if len(g.Edges[0].Reasons) != 3 {
		t.Errorf("expected 3 reasons (target+semantic+workspace); got %d: %+v",
			len(g.Edges[0].Reasons), g.Edges[0].Reasons)
	}
}

func TestConflicts_EdgeOrderingCanonical(t *testing.T) {
	// Input order is reversed; the resulting edge should still have
	// From="gm-1", To="gm-2" (lexicographic).
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-2", "src/x.go"),
		bead("gm-1", "src/x.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge; got %d", len(g.Edges))
	}
	if g.Edges[0].From != "gm-1" || g.Edges[0].To != "gm-2" {
		t.Errorf("edge = (%s, %s); want canonical (gm-1, gm-2)", g.Edges[0].From, g.Edges[0].To)
	}
}

func TestConflicts_EmptyTargetsNoEdge(t *testing.T) {
	// A bead with no targets can't conflict at the file level. (It
	// might still conflict via semantic / workspace; those detectors
	// are nil here.)
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1"),
		bead("gm-2", "src/x.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(g.Edges) != 0 {
		t.Errorf("empty-target bead must not produce a target-overlap edge; got %+v", g.Edges)
	}
}

func TestConflicts_DetectorErrorPropagates(t *testing.T) {
	sem := errSemantic{err: errors.New("source-analysis cache cold")}
	_, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/x.go"),
		bead("gm-2", "lib/y.go"),
	}, Options{Semantic: sem})
	if err == nil {
		t.Fatal("expected error from semantic detector; got nil")
	}
}

// ── Graph methods ──────────────────────────────────────────────────

func TestGraph_NeighborsAndHasEdge(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/a.go"),
		bead("gm-2", "src/a.go"),
		bead("gm-3", "src/b.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got := g.Neighbors("gm-1"); !reflect.DeepEqual(got, []core.WorkItemID{"gm-2"}) {
		t.Errorf("Neighbors(gm-1) = %v; want [gm-2]", got)
	}
	if got := g.Neighbors("gm-3"); len(got) != 0 {
		t.Errorf("Neighbors(gm-3) = %v; want empty", got)
	}
	if has, _ := g.HasEdge("gm-1", "gm-2"); !has {
		t.Error("HasEdge(gm-1, gm-2) = false; want true")
	}
	if has, _ := g.HasEdge("gm-2", "gm-1"); !has {
		t.Error("HasEdge order-insensitive; got false")
	}
	if has, _ := g.HasEdge("gm-1", "gm-3"); has {
		t.Error("HasEdge(gm-1, gm-3) = true; want false")
	}
}

// ── Batches ────────────────────────────────────────────────────────

func TestBatches_AllConflictFreeFitsOneBatch(t *testing.T) {
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/a.go"),
		bead("gm-2", "src/b.go"),
		bead("gm-3", "src/c.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	bs := g.Batches()
	if len(bs) != 1 {
		t.Fatalf("expected 1 batch; got %d (%+v)", len(bs), bs)
	}
	if !reflect.DeepEqual(bs[0].Beads, []core.WorkItemID{"gm-1", "gm-2", "gm-3"}) {
		t.Errorf("batch = %v; want [gm-1, gm-2, gm-3]", bs[0].Beads)
	}
}

func TestBatches_FirstFitGreedyWithConflict(t *testing.T) {
	// gm-1 and gm-2 conflict; gm-3 is independent.
	// Expected first-fit:
	//   batch 0: gm-1, gm-3 (gm-3 fits because it doesn't conflict with gm-1)
	//   batch 1: gm-2 (conflicts with gm-1, opens new batch)
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/a.go"),
		bead("gm-2", "src/a.go"),
		bead("gm-3", "src/b.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	bs := g.Batches()
	if len(bs) != 2 {
		t.Fatalf("expected 2 batches; got %d (%+v)", len(bs), bs)
	}
	if !reflect.DeepEqual(bs[0].Beads, []core.WorkItemID{"gm-1", "gm-3"}) {
		t.Errorf("batch 0 = %v; want [gm-1, gm-3]", bs[0].Beads)
	}
	if !reflect.DeepEqual(bs[1].Beads, []core.WorkItemID{"gm-2"}) {
		t.Errorf("batch 1 = %v; want [gm-2]", bs[1].Beads)
	}
}

func TestBatches_ChainOfConflicts(t *testing.T) {
	// gm-1 ↔ gm-2 ↔ gm-3 (linear chain). gm-1 and gm-3 are NOT in
	// conflict with each other (no edge), so they can share a batch.
	g, err := Conflicts(context.Background(), []Bead{
		bead("gm-1", "src/a.go", "src/shared12.go"),
		bead("gm-2", "src/shared12.go", "src/shared23.go"),
		bead("gm-3", "src/c.go", "src/shared23.go"),
	}, Options{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	bs := g.Batches()
	if len(bs) != 2 {
		t.Fatalf("chain expected 2 batches; got %d (%+v)", len(bs), bs)
	}
	// First-fit greedy with sorted (gm-1, gm-2, gm-3):
	//   batch 0: gm-1
	//   batch 1: gm-2 (conflicts with gm-1)
	//   batch 0 again: gm-3 fits (only conflicts with gm-2 in batch 1)
	if !reflect.DeepEqual(bs[0].Beads, []core.WorkItemID{"gm-1", "gm-3"}) {
		t.Errorf("batch 0 = %v; want [gm-1, gm-3]", bs[0].Beads)
	}
	if !reflect.DeepEqual(bs[1].Beads, []core.WorkItemID{"gm-2"}) {
		t.Errorf("batch 1 = %v; want [gm-2]", bs[1].Beads)
	}
}

func TestBatches_DeterministicAcrossInputOrder(t *testing.T) {
	// Same set of beads; different input order → identical batches.
	mk := func(order []core.WorkItemID) []Batch {
		bs := []Bead{
			bead("gm-1", "src/a.go", "src/shared.go"),
			bead("gm-2", "src/shared.go"),
			bead("gm-3", "src/c.go"),
		}
		// Reorder by `order`.
		idx := map[core.WorkItemID]int{"gm-1": 0, "gm-2": 1, "gm-3": 2}
		permuted := make([]Bead, len(order))
		for i, id := range order {
			permuted[i] = bs[idx[id]]
		}
		g, err := Conflicts(context.Background(), permuted, Options{})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		return g.Batches()
	}
	a := mk([]core.WorkItemID{"gm-1", "gm-2", "gm-3"})
	b := mk([]core.WorkItemID{"gm-3", "gm-1", "gm-2"})
	if !reflect.DeepEqual(a, b) {
		t.Errorf("batches differ across input order:\n  a=%+v\n  b=%+v", a, b)
	}
}

// ── fakes ──────────────────────────────────────────────────────────

type fakeFS map[targets.Pattern][]string

func (f fakeFS) Glob(pattern targets.Pattern) ([]string, error) {
	if v, ok := f[pattern]; ok {
		return v, nil
	}
	return nil, nil
}

type fakeSemantic struct {
	overlapPairs map[[2]core.WorkItemID]string
}

func (f fakeSemantic) Detect(_ context.Context, a, b Bead) (bool, string, error) {
	key := pairKey(a.ID, b.ID)
	if msg, ok := f.overlapPairs[key]; ok {
		return true, msg, nil
	}
	return false, "", nil
}

type fakeWorkspace struct {
	colliding map[[2]core.WorkItemID]string
}

func (f fakeWorkspace) Detect(_ context.Context, a, b core.WorkItemID) (bool, string, error) {
	key := pairKey(a, b)
	if msg, ok := f.colliding[key]; ok {
		return true, msg, nil
	}
	return false, "", nil
}

type errSemantic struct{ err error }

func (e errSemantic) Detect(_ context.Context, _, _ Bead) (bool, string, error) {
	return false, "", e.err
}

func pairKey(a, b core.WorkItemID) [2]core.WorkItemID {
	if a < b {
		return [2]core.WorkItemID{a, b}
	}
	return [2]core.WorkItemID{b, a}
}
