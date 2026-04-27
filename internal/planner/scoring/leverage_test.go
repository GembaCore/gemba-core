package scoring

import (
	"math"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// fakeView is a literal in-memory DependencyView. blocks[X] = list of
// beads X blocks; closed marks beads excluded from the leverage
// count.
type fakeView struct {
	blocks map[core.WorkItemID][]core.WorkItemID
	closed map[core.WorkItemID]bool
}

func (f fakeView) Blocks(id core.WorkItemID) []core.WorkItemID { return f.blocks[id] }
func (f fakeView) IsOpen(id core.WorkItemID) bool              { return !f.closed[id] }

func TestLeverage_LeafBeadScoresZero(t *testing.T) {
	view := fakeView{}
	got := Leverage("gm-leaf", view, 0)
	if got.Score != 0 {
		t.Errorf("score = %v, want 0", got.Score)
	}
	if got.Weight != 0 {
		t.Errorf("weight = %d, want 0", got.Weight)
	}
	if len(got.Blocked) != 0 {
		t.Errorf("blocked = %v, want empty", got.Blocked)
	}
}

func TestLeverage_SingleDirectBlockBumpsScore(t *testing.T) {
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-2"},
	}}
	got := Leverage("gm-1", view, 0)
	if got.Weight != 1 {
		t.Fatalf("weight = %d, want 1", got.Weight)
	}
	wantScore := 1 - math.Exp(-1.0)
	if math.Abs(got.Score-wantScore) > 1e-9 {
		t.Errorf("score = %v, want %v", got.Score, wantScore)
	}
	if len(got.Blocked) != 1 || got.Blocked[0] != "gm-2" {
		t.Errorf("blocked = %v", got.Blocked)
	}
}

func TestLeverage_TransitiveChainCountsAllDownstream(t *testing.T) {
	// gm-1 → gm-2 → gm-3 → gm-4
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-2"},
		"gm-2": {"gm-3"},
		"gm-3": {"gm-4"},
	}}
	got := Leverage("gm-1", view, 0)
	if got.Weight != 3 {
		t.Errorf("weight = %d, want 3 (transitive)", got.Weight)
	}
	if len(got.Blocked) != 3 {
		t.Errorf("blocked = %v, want 3 entries", got.Blocked)
	}
}

func TestLeverage_FiveBlockerBeadAsymptotesNearOne(t *testing.T) {
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-2", "gm-3", "gm-4", "gm-5", "gm-6"},
	}}
	got := Leverage("gm-1", view, 0)
	if got.Score < 0.99 {
		t.Errorf("score = %v, want > 0.99", got.Score)
	}
}

func TestLeverage_ClosedDownstreamSkipped(t *testing.T) {
	view := fakeView{
		blocks: map[core.WorkItemID][]core.WorkItemID{
			"gm-1": {"gm-2", "gm-3"},
			"gm-2": {"gm-4"},
		},
		closed: map[core.WorkItemID]bool{"gm-3": true},
	}
	got := Leverage("gm-1", view, 0)
	// gm-2 + gm-4 count; gm-3 doesn't.
	if got.Weight != 2 {
		t.Errorf("weight = %d, want 2; blocked=%v", got.Weight, got.Blocked)
	}
}

func TestLeverage_DeterministicSortedOutput(t *testing.T) {
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-z", "gm-a", "gm-m"},
	}}
	got := Leverage("gm-1", view, 0)
	want := []core.WorkItemID{"gm-a", "gm-m", "gm-z"}
	if len(got.Blocked) != len(want) {
		t.Fatalf("len = %d, want %d", len(got.Blocked), len(want))
	}
	for i := range want {
		if got.Blocked[i] != want[i] {
			t.Errorf("[%d] = %s, want %s", i, got.Blocked[i], want[i])
		}
	}
}

func TestLeverage_CycleDoesNotInfiniteLoop(t *testing.T) {
	// gm-1 ↔ gm-2 — pathological but real (a buggy bead graph
	// shouldn't crash the planner).
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-2"},
		"gm-2": {"gm-1"},
	}}
	got := Leverage("gm-1", view, 0)
	if got.Weight != 1 {
		t.Errorf("cycle should count gm-2 once; weight=%d", got.Weight)
	}
}

func TestLeverage_CustomKShiftsCurve(t *testing.T) {
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-2"},
	}}
	gentle := Leverage("gm-1", view, 0.1)
	default_ := Leverage("gm-1", view, 0)
	if gentle.Score >= default_.Score {
		t.Errorf("smaller k should produce smaller score: gentle=%v default=%v", gentle.Score, default_.Score)
	}
}

func TestLeverage_NilViewReturnsZero(t *testing.T) {
	got := Leverage("gm-1", nil, 0)
	if got.Score != 0 || got.Weight != 0 {
		t.Errorf("nil view should return zero result; got %+v", got)
	}
}

func TestLeverage_EmptyBeadIDReturnsZero(t *testing.T) {
	view := fakeView{blocks: map[core.WorkItemID][]core.WorkItemID{
		"gm-1": {"gm-2"},
	}}
	got := Leverage("", view, 0)
	if got.Score != 0 {
		t.Errorf("empty beadID should return zero; got %+v", got)
	}
}
