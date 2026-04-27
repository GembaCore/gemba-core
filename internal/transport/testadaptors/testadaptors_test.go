package testadaptors

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

// Sanity-check the programmable-hook + call-recording contract documented
// on FakeWorkPlane (gm-root.1.2). These tests exist so a future change to
// the helper can't silently break consumers (beads_test.go, capabilities_test.go,
// router_test.go) that depend on the defaults.

func TestFakeWorkPlane_Describe_DefaultReturnsManifest(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)
	got, err := f.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got.AdaptorName != "fake" || got.Transport != core.TransportAPI {
		t.Fatalf("default manifest mismatch: %+v", got)
	}
	if n := f.DescribeCalls(); n != 1 {
		t.Fatalf("DescribeCalls: want 1, got %d", n)
	}
}

func TestFakeWorkPlane_Describe_SurfacesDescribeErr(t *testing.T) {
	sentinel := errors.New("negotiation flap")
	f := NewFakeWorkPlane(core.TransportAPI)
	f.DescribeErr = sentinel

	if _, err := f.Describe(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}
}

// ListWorkItems / GetWorkItem default paths must return loud errors so a
// test that forgets to program the hook doesn't silently pass. The handler
// contract in beads.go depends on this for the "unprogrammed call" case to
// surface as an untagged-error (500) test signal.
func TestFakeWorkPlane_List_DefaultReturnsError(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)
	_, err := f.ListWorkItems(context.Background(), core.WorkItemFilter{})
	if err == nil {
		t.Fatal("want error when ListFn is nil")
	}
}

func TestFakeWorkPlane_Get_DefaultReturnsErrNotFound(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)
	_, err := f.GetWorkItem(context.Background(), "gm-x")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFakeWorkPlane_List_HookIsCalledAndRecorded(t *testing.T) {
	want := []core.WorkItem{{ID: "gm-1"}, {ID: "gm-2"}}
	f := NewFakeWorkPlane(core.TransportAPI)
	f.ListFn = func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
		return want, nil
	}

	filter := core.WorkItemFilter{Limit: 10}
	got, err := f.ListWorkItems(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(got) != 2 || got[0].ID != "gm-1" {
		t.Fatalf("unexpected list: %+v", got)
	}
	calls := f.ListCalls()
	if len(calls) != 1 || calls[0].Limit != 10 {
		t.Fatalf("ListCalls: want 1 filter with Limit=10, got %+v", calls)
	}
}

func TestFakeWorkPlane_Get_HookIsCalledAndRecorded(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)
	f.GetFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		if id == "gm-1" {
			return core.WorkItem{ID: id, Title: "hit"}, nil
		}
		return core.WorkItem{}, core.ErrNotFound
	}

	wi, err := f.GetWorkItem(context.Background(), "gm-1")
	if err != nil || wi.Title != "hit" {
		t.Fatalf("GetWorkItem(gm-1): %+v, err=%v", wi, err)
	}
	if _, err := f.GetWorkItem(context.Background(), "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetWorkItem(missing): want ErrNotFound, got %v", err)
	}
	calls := f.GetCalls()
	if len(calls) != 2 || calls[0] != "gm-1" || calls[1] != "missing" {
		t.Fatalf("GetCalls: %+v", calls)
	}
}

func TestFakeWorkPlane_Create_Update_Attach_RecordAndDispatch(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)

	// Defaults return sentinel errors.
	if _, err := f.CreateWorkItem(context.Background(), core.WorkItem{ID: "new"}); err == nil {
		t.Fatal("CreateWorkItem default: want error")
	}
	if _, err := f.UpdateWorkItem(context.Background(), "gm-1", core.WorkItemPatch{}); err == nil {
		t.Fatal("UpdateWorkItem default: want error")
	}
	if err := f.AttachEvidence(context.Background(), "gm-1", core.Evidence{ID: "e"}); !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("AttachEvidence default: want ErrUnsupported, got %v", err)
	}

	// Hooks take over and calls are recorded.
	f.CreateFn = func(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
		wi.ID = "assigned"
		return wi, nil
	}
	f.UpdateFn = func(_ context.Context, id core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
		return core.WorkItem{ID: id, Title: "patched"}, nil
	}
	f.AttachFn = func(context.Context, core.WorkItemID, core.Evidence) error { return nil }

	wi, err := f.CreateWorkItem(context.Background(), core.WorkItem{Title: "t"})
	if err != nil || wi.ID != "assigned" {
		t.Fatalf("CreateWorkItem: wi=%+v err=%v", wi, err)
	}
	if _, err := f.UpdateWorkItem(context.Background(), "gm-1", core.WorkItemPatch{}); err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	if err := f.AttachEvidence(context.Background(), "gm-1", core.Evidence{ID: "ev-1"}); err != nil {
		t.Fatalf("AttachEvidence: %v", err)
	}

	// There are two attempts on Create and Update (one default + one hook).
	if got := f.CreateCalls(); len(got) != 2 {
		t.Fatalf("CreateCalls: want 2, got %d", len(got))
	}
	if got := f.UpdateCalls(); len(got) != 2 || got[0].ID != "gm-1" {
		t.Fatalf("UpdateCalls: %+v", got)
	}
	if got := f.AttachCalls(); len(got) != 2 || got[1].Evidence.ID != "ev-1" {
		t.Fatalf("AttachCalls: %+v", got)
	}
}

func TestFakeWorkPlane_Sprints_BudgetDefaults(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)

	sprints, err := f.ListSprints(context.Background())
	if err != nil || sprints != nil {
		t.Fatalf("ListSprints default: got %v, err=%v", sprints, err)
	}
	if _, err := f.ReadBudgetRollup(context.Background(), "sp-1"); !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("ReadBudgetRollup default: want ErrUnsupported, got %v", err)
	}
	if n := f.SprintsCalls(); n != 1 {
		t.Fatalf("SprintsCalls: want 1, got %d", n)
	}
	if calls := f.BudgetCalls(); len(calls) != 1 || calls[0] != "sp-1" {
		t.Fatalf("BudgetCalls: %+v", calls)
	}
}

// The helper is used concurrently across transport tests; make sure the
// recording side doesn't race. `go test -race` surfaces breakage here.
func TestFakeWorkPlane_Recording_IsConcurrencySafe(t *testing.T) {
	f := NewFakeWorkPlane(core.TransportAPI)
	f.GetFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{ID: id}, nil
	}

	const N = 50
	done := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			_, _ = f.GetWorkItem(context.Background(), "gm-z")
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}
	if got := len(f.GetCalls()); got != N {
		t.Fatalf("GetCalls: want %d, got %d", N, got)
	}
}
