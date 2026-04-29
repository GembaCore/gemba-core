package gembatesting

import (
	"context"
	"fmt"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// probeWorkItemCreateGet exercises the Group B CRUD round-trip. It
// creates a work item, re-reads it via GetWorkItem, and checks the
// returned record carries the same Title. Kept deliberately narrow —
// adaptors with richer validation fill in Description/Status/etc. in
// their own probes. Every adaptor MUST at least preserve the Title the
// caller supplied.
func probeWorkItemCreateGet(t probeT, impl core.WorkPlane) {
	t.Helper()
	ctx := context.Background()
	id := conformanceWorkItemID("create-get")
	wi := core.WorkItem{
		ID:            id,
		Title:         "conformance: create/get round-trip",
		Status:        "open",
		StateCategory: core.StateUnstarted,
		UpdatedAt:     time.Now(),
	}
	created, err := impl.CreateWorkItem(ctx, wi)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateWorkItem: returned WorkItem has empty ID (adaptor must materialize a backend-assigned id)")
	}
	got, err := impl.GetWorkItem(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetWorkItem(%q): %v", created.ID, err)
	}
	if got.Title != wi.Title {
		t.Errorf("GetWorkItem: Title=%q, want %q (CRUD round-trip must preserve the caller-supplied title)",
			got.Title, wi.Title)
	}
}

// probeWorkItemUpdate exercises Group B's patch round-trip: a Title
// patch must land on the returned record.
//
// Drives the patch off created.ID rather than the caller-supplied id —
// adaptors like bd assign their own ids from a backend prefix pool and
// ignore the caller's value, matching Jira / GitHub / Linear semantics.
// The Group A probe already asserts the create response carries an id,
// so this is a safe handoff.
func probeWorkItemUpdate(t probeT, impl core.WorkPlane) {
	t.Helper()
	ctx := context.Background()
	id := conformanceWorkItemID("update")
	created, err := impl.CreateWorkItem(ctx, core.WorkItem{
		ID:            id,
		Title:         "before",
		Status:        "open",
		StateCategory: core.StateUnstarted,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateWorkItem: returned record has empty ID; cannot drive Update probe")
	}
	newTitle := "after"
	patched, err := impl.UpdateWorkItem(ctx, created.ID, core.WorkItemPatch{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	if patched.Title != newTitle {
		t.Errorf("UpdateWorkItem: Title=%q, want %q (patch field must land on returned record)",
			patched.Title, newTitle)
	}
}

// probeWorkItemList asserts a created item appears in ListWorkItems
// output. Drives the discovery query through the IDs filter so the
// probe stays robust against backends that paginate or apply a default
// limit (bd's `bd list` defaults to 50 rows; on a populated workspace
// a "list with zero filter" would naturally truncate the just-created
// item out of the result). The IDs-narrowed query is the canonical
// "find this specific record" path.
func probeWorkItemList(t probeT, impl core.WorkPlane) {
	t.Helper()
	ctx := context.Background()
	id := conformanceWorkItemID("list")
	created, err := impl.CreateWorkItem(ctx, core.WorkItem{
		ID:            id,
		Title:         "conformance: list discovery",
		Status:        "open",
		StateCategory: core.StateUnstarted,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateWorkItem: returned record has empty ID; cannot drive List probe")
	}
	items, err := impl.ListWorkItems(ctx, core.WorkItemFilter{
		IDs: []core.WorkItemID{created.ID},
	})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	for _, it := range items {
		if it.ID == created.ID {
			return
		}
	}
	t.Errorf("ListWorkItems did not return the just-created item %q (got %d items)", created.ID, len(items))
}

// conformanceWorkItemID mints a unique id for each probe run so
// re-running the harness against an adaptor with a persistent backend
// doesn't collide with a prior run.
func conformanceWorkItemID(tag string) core.WorkItemID {
	return core.WorkItemID(fmt.Sprintf("gemba/gemba/gm-conformance-%s-%d", tag, time.Now().UnixNano()))
}
