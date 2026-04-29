package milestones

import (
	"context"
	"errors"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// fakeWP is the smallest WorkPlane stub the walker needs. We only
// implement GetWorkItem; calls to anything else are surfaced as a
// hard test failure so a regression that relies on a different method
// is loud.
type fakeWP struct {
	items map[core.WorkItemID]core.WorkItem
	err   error // returned from GetWorkItem when set
	calls int
	t     *testing.T
}

func (f *fakeWP) GetWorkItem(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
	f.calls++
	if f.err != nil {
		return core.WorkItem{}, f.err
	}
	it, ok := f.items[id]
	if !ok {
		return core.WorkItem{}, core.ErrNotFound
	}
	return it, nil
}

func (f *fakeWP) Describe(context.Context) (core.CapabilityManifest, error) {
	f.t.Fatal("Describe should not be called")
	return core.CapabilityManifest{}, nil
}
func (f *fakeWP) ListWorkItems(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
	f.t.Fatal("ListWorkItems should not be called")
	return nil, nil
}
func (f *fakeWP) CreateWorkItem(context.Context, core.WorkItem) (core.WorkItem, error) {
	f.t.Fatal("CreateWorkItem should not be called")
	return core.WorkItem{}, nil
}
func (f *fakeWP) UpdateWorkItem(context.Context, core.WorkItemID, core.WorkItemPatch) (core.WorkItem, error) {
	f.t.Fatal("UpdateWorkItem should not be called")
	return core.WorkItem{}, nil
}
func (f *fakeWP) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error {
	f.t.Fatal("AttachEvidence should not be called")
	return nil
}
func (f *fakeWP) ListSprints(context.Context) ([]core.Sprint, error) {
	f.t.Fatal("ListSprints should not be called")
	return nil, nil
}
func (f *fakeWP) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	f.t.Fatal("ReadBudgetRollup should not be called")
	return core.BudgetRollup{}, nil
}
func (f *fakeWP) Subscribe(context.Context, core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	f.t.Fatal("Subscribe should not be called")
	return nil, nil
}

// childWith builds a bead with one inbound parent_child edge — the
// convention adaptors emit for inbound edges (see internal/adapter/bd/edges.go).
func childWith(id, parent core.WorkItemID, kind string) core.WorkItem {
	return core.WorkItem{
		ID:   id,
		Kind: kind,
		Relationships: []core.Relationship{
			{Kind: core.RelParentChild, From: parent, To: id},
		},
	}
}

func TestFindMilestoneAncestor_NoParent(t *testing.T) {
	// Bead with no parent_child edge — common shape for orphan tasks.
	// Walker must return (nil, nil) without calling GetWorkItem at all.
	wp := &fakeWP{t: t, items: map[core.WorkItemID]core.WorkItem{}}
	item := core.WorkItem{ID: "gm-a", Kind: "task"}
	got, err := FindMilestoneAncestor(context.Background(), wp, item)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil ancestor, got %+v", got)
	}
	if wp.calls != 0 {
		t.Errorf("expected 0 GetWorkItem calls, got %d", wp.calls)
	}
}

func TestFindMilestoneAncestor_ParentNotMilestone(t *testing.T) {
	// task → epic chain with no milestone above the epic → (nil, nil).
	epic := core.WorkItem{ID: "gm-e", Kind: "epic"}
	wp := &fakeWP{t: t, items: map[core.WorkItemID]core.WorkItem{"gm-e": epic}}
	task := childWith("gm-t", "gm-e", "task")
	got, err := FindMilestoneAncestor(context.Background(), wp, task)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil ancestor, got %+v", got)
	}
}

func TestFindMilestoneAncestor_DirectParent(t *testing.T) {
	// epic→milestone, walker started from the epic returns the milestone.
	ms := core.WorkItem{ID: "gm-m1", Kind: core.KindMilestone, Title: "M1 Beta"}
	wp := &fakeWP{t: t, items: map[core.WorkItemID]core.WorkItem{"gm-m1": ms}}
	epic := childWith("gm-e", "gm-m1", "epic")
	got, err := FindMilestoneAncestor(context.Background(), wp, epic)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil || got.ID != "gm-m1" {
		t.Fatalf("expected milestone gm-m1, got %+v", got)
	}
}

func TestFindMilestoneAncestor_DeepChain(t *testing.T) {
	// task → story → epic → milestone. Walker returns the milestone
	// after exactly three hops.
	ms := core.WorkItem{ID: "gm-m2", Kind: core.KindMilestone, Title: "M2"}
	epic := childWith("gm-e", "gm-m2", "epic")
	story := childWith("gm-s", "gm-e", "story")
	wp := &fakeWP{t: t, items: map[core.WorkItemID]core.WorkItem{
		"gm-m2": ms,
		"gm-e":  epic,
		"gm-s":  story,
	}}
	task := childWith("gm-t", "gm-s", "task")
	got, err := FindMilestoneAncestor(context.Background(), wp, task)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil || got.ID != "gm-m2" {
		t.Fatalf("expected milestone gm-m2, got %+v", got)
	}
	if wp.calls != 3 {
		t.Errorf("expected 3 lookups (story→epic→milestone), got %d", wp.calls)
	}
}

func TestFindMilestoneAncestor_SelfMilestone(t *testing.T) {
	// Starting from a milestone returns the milestone itself without
	// any GetWorkItem calls — keeps the helper safe to use from
	// surfaces that already have a milestone in hand.
	ms := core.WorkItem{ID: "gm-m3", Kind: core.KindMilestone, Title: "M3"}
	wp := &fakeWP{t: t, items: map[core.WorkItemID]core.WorkItem{}}
	got, err := FindMilestoneAncestor(context.Background(), wp, ms)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil || got.ID != "gm-m3" {
		t.Fatalf("expected self, got %+v", got)
	}
	if wp.calls != 0 {
		t.Errorf("expected 0 lookups, got %d", wp.calls)
	}
}

func TestFindMilestoneAncestor_NilWorkPlane(t *testing.T) {
	// Defensive: a nil WorkPlane (degraded mode, tests) returns nil
	// without panicking. Callers in start.go already gate on this but
	// the helper guards independently.
	got, err := FindMilestoneAncestor(context.Background(), nil, core.WorkItem{ID: "gm-x"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil ancestor, got %+v", got)
	}
}

func TestFindMilestoneAncestor_WorkPlaneError(t *testing.T) {
	// Errors propagate so the caller can decide policy. start.go's
	// policy is "ship unbound preamble" — that's tested at the caller,
	// not here.
	wp := &fakeWP{t: t, err: errors.New("transport down")}
	task := childWith("gm-t", "gm-e", "task")
	_, err := FindMilestoneAncestor(context.Background(), wp, task)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestFindMilestoneAncestor_CycleGuard(t *testing.T) {
	// Pathological graph: A→B→A. Validation rejects this, but the
	// walker must still terminate rather than spin forever if a bad
	// adaptor leaks one in.
	a := core.WorkItem{
		ID: "gm-a", Kind: "epic",
		Relationships: []core.Relationship{{Kind: core.RelParentChild, From: "gm-b", To: "gm-a"}},
	}
	b := core.WorkItem{
		ID: "gm-b", Kind: "epic",
		Relationships: []core.Relationship{{Kind: core.RelParentChild, From: "gm-a", To: "gm-b"}},
	}
	wp := &fakeWP{t: t, items: map[core.WorkItemID]core.WorkItem{"gm-a": a, "gm-b": b}}
	got, err := FindMilestoneAncestor(context.Background(), wp, a)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil ancestor on cycle, got %+v", got)
	}
}
