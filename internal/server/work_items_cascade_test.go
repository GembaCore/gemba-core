package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
)

type cascadeFixture struct {
	router  *Router
	items   map[core.WorkItemID]core.WorkItem
	op      *cascadeOrchestration
	updates []core.WorkItemID
}

type cascadeOrchestration struct {
	*testadaptors.FakeOrchestrationPlane
	starts []startCall
}

func (c *cascadeOrchestration) StartSession(_ context.Context, assignmentID string, prompt core.SessionPrompt) (core.Session, error) {
	c.starts = append(c.starts, startCall{assignmentID: assignmentID, prompt: prompt})
	return core.Session{ID: "sess-" + assignmentID, AssignmentID: assignmentID, Status: core.SessionWorking}, nil
}

func newCascadeFixture(t *testing.T, items []core.WorkItem) *cascadeFixture {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	fx := &cascadeFixture{
		items: make(map[core.WorkItemID]core.WorkItem, len(items)),
		op:    &cascadeOrchestration{FakeOrchestrationPlane: testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)},
	}
	for _, it := range items {
		fx.items[it.ID] = it
	}
	wp.GetFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		if it, ok := fx.items[id]; ok {
			return it, nil
		}
		return core.WorkItem{}, core.ErrNotFound
	}
	wp.UpdateFn = func(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		fx.updates = append(fx.updates, id)
		it, ok := fx.items[id]
		if !ok {
			return core.WorkItem{}, core.ErrNotFound
		}
		if patch.StateCategory != nil {
			it.StateCategory = *patch.StateCategory
		}
		if len(patch.Labels) > 0 {
			it.Labels = append([]string(nil), patch.Labels...)
		}
		fx.items[id] = it
		return it, nil
	}
	wp.CreateFn = func(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
		if wi.ID == "" {
			wi.ID = "gm-new"
		}
		for i := range wi.Relationships {
			if wi.Relationships[i].Kind == core.RelParentChild && wi.Relationships[i].To == "" {
				wi.Relationships[i].To = wi.ID
			}
		}
		fx.items[wi.ID] = wi
		return wi, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	if _, err := host.RegisterOrchestrationPlane(context.Background(), fx.op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}
	fx.router = NewRouter(config.ServeConfig{}, fakeSPA(), host)
	fx.router.AttachPools([]config.ResolvedPool{{AgentType: "claude", SizeEffective: 2}})
	return fx
}

func TestCascadeDispatch_StartsUnblockedDescendantLeavesOnly(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicID := core.WorkItemID("gm-e1")
	readyA := core.WorkItemID("gm-a")
	readyB := core.WorkItemID("gm-b")
	blocked := core.WorkItemID("gm-c")

	fx := newCascadeFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{parentChildRel(milestoneID, epicID)},
		},
		{
			ID: epicID, Kind: "epic", Title: "Epic",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicID),
				parentChildRel(epicID, readyA),
				parentChildRel(epicID, readyB),
				parentChildRel(epicID, blocked),
			},
		},
		{
			ID: readyA, Kind: "task", Title: "A", StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(epicID, readyA)},
		},
		{
			ID: readyB, Kind: "bug", Title: "B", StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(epicID, readyB)},
		},
		{
			ID: blocked, Kind: "feature", Title: "C", StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(epicID, blocked),
				{Kind: core.RelBlocks, From: readyA, To: blocked},
			},
		},
	})

	body := bytes.NewBufferString(`{"agent_type":"claude"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/work-items/"+string(milestoneID)+"/cascade-dispatch", body)
	req.Header.Set(ConfirmHeader, "nonce-cascade-1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp cascadeDispatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Dispatched) != 2 {
		t.Fatalf("dispatched=%+v, want two ready leaves", resp.Dispatched)
	}
	if len(fx.op.starts) != 2 {
		t.Fatalf("StartSession calls=%d, want 2", len(fx.op.starts))
	}
	if fx.items[readyA].StateCategory != core.StateStarted || fx.items[readyB].StateCategory != core.StateStarted {
		t.Fatalf("ready leaves not moved to started: a=%s b=%s",
			fx.items[readyA].StateCategory, fx.items[readyB].StateCategory)
	}
	if fx.items[blocked].StateCategory != core.StateUnstarted {
		t.Fatalf("blocked leaf moved unexpectedly: %s", fx.items[blocked].StateCategory)
	}
	if !isCascadeActive(fx.items[milestoneID].Labels) {
		t.Fatalf("wrapper labels=%v, want cascade-active", fx.items[milestoneID].Labels)
	}
}

func TestCascadeDispatch_StagesBacklogDescendantsBeforeDispatch(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicID := core.WorkItemID("gm-e1")
	ready := core.WorkItemID("gm-a")
	blocked := core.WorkItemID("gm-b")
	blocker := core.WorkItemID("gm-c")

	fx := newCascadeFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{parentChildRel(milestoneID, epicID)},
		},
		{
			ID: epicID, Kind: "epic", Title: "Epic",
			StateCategory: core.StateBacklog,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicID),
				parentChildRel(epicID, ready),
				parentChildRel(epicID, blocked),
				parentChildRel(epicID, blocker),
			},
		},
		{
			ID: ready, Kind: "task", Title: "Ready backlog", StateCategory: core.StateBacklog,
			Relationships: []core.Relationship{parentChildRel(epicID, ready)},
		},
		{
			ID: blocked, Kind: "task", Title: "Blocked backlog", StateCategory: core.StateBacklog,
			Relationships: []core.Relationship{
				parentChildRel(epicID, blocked),
				{Kind: core.RelBlocks, From: blocker, To: blocked},
			},
		},
		{
			ID: blocker, Kind: "task", Title: "Blocker", StateCategory: core.StateStarted,
			Relationships: []core.Relationship{parentChildRel(epicID, blocker)},
		},
	})

	body := bytes.NewBufferString(`{"agent_type":"claude"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/work-items/"+string(milestoneID)+"/cascade-dispatch", body)
	req.Header.Set(ConfirmHeader, "nonce-cascade-stage")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp cascadeDispatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Staged) != 2 {
		t.Fatalf("staged=%+v, want both backlog descendants staged", resp.Staged)
	}
	if len(resp.Dispatched) != 1 || resp.Dispatched[0].WorkItemID != string(ready) {
		t.Fatalf("dispatched=%+v, want ready backlog leaf", resp.Dispatched)
	}
	if fx.items[ready].StateCategory != core.StateStarted {
		t.Fatalf("ready leaf state=%s, want started", fx.items[ready].StateCategory)
	}
	if fx.items[blocked].StateCategory != core.StateStaged {
		t.Fatalf("blocked leaf state=%s, want staged", fx.items[blocked].StateCategory)
	}
	if len(fx.op.starts) != 1 || fx.op.starts[0].assignmentID != string(ready) {
		t.Fatalf("StartSession calls=%+v, want ready backlog leaf only", fx.op.starts)
	}
}

func TestCreateWorkItem_UnderActiveCascadeStagesAndDispatches(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicID := core.WorkItemID("gm-e1")
	childID := core.WorkItemID("gm-new")

	fx := newCascadeFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateStarted,
			Labels:        cascadeLabels(nil, "claude"),
			Relationships: []core.Relationship{parentChildRel(milestoneID, epicID)},
		},
		{
			ID: epicID, Kind: "epic", Title: "Epic",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicID),
				parentChildRel(epicID, childID),
			},
		},
	})

	body := map[string]any{
		"item": map[string]any{
			"title":          "Session-created follow-up",
			"kind":           "task",
			"status":         "open",
			"state_category": "backlog",
			"relationships": []map[string]any{
				{"kind": string(core.RelParentChild), "from": string(epicID), "to": ""},
			},
		},
	}
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, postCreateReq(t, "nonce-cascade-create", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var created core.WorkItem
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.StateCategory != core.StateStarted {
		t.Fatalf("response state=%s, want started after active cascade", created.StateCategory)
	}
	if fx.items[childID].StateCategory != core.StateStarted {
		t.Fatalf("persisted child state=%s, want started", fx.items[childID].StateCategory)
	}
	if len(fx.op.starts) != 1 || fx.op.starts[0].assignmentID != string(childID) {
		t.Fatalf("StartSession calls=%+v, want session-created child", fx.op.starts)
	}
}

func TestPatchWorkItem_ActiveCascadeContinuesWhenBlockerCompletes(t *testing.T) {
	milestoneID := core.WorkItemID("gm-m1")
	epicID := core.WorkItemID("gm-e1")
	blocker := core.WorkItemID("gm-a")
	blocked := core.WorkItemID("gm-b")

	fx := newCascadeFixture(t, []core.WorkItem{
		{
			ID: milestoneID, Kind: core.KindMilestone, Title: "M1",
			StateCategory: core.StateStarted,
			Labels:        cascadeLabels(nil, "claude"),
			Relationships: []core.Relationship{parentChildRel(milestoneID, epicID)},
		},
		{
			ID: epicID, Kind: "epic", Title: "Epic",
			StateCategory: core.StateStarted,
			Relationships: []core.Relationship{
				parentChildRel(milestoneID, epicID),
				parentChildRel(epicID, blocker),
				parentChildRel(epicID, blocked),
			},
		},
		{
			ID: blocker, Kind: "task", Title: "A", StateCategory: core.StateStarted,
			Relationships: []core.Relationship{parentChildRel(epicID, blocker)},
		},
		{
			ID: blocked, Kind: "task", Title: "B", StateCategory: core.StateStaged,
			Relationships: []core.Relationship{
				parentChildRel(epicID, blocked),
				{Kind: core.RelBlocks, From: blocker, To: blocked},
			},
		},
	})

	completed := core.StateCompleted
	reqBody := core.WorkItemPatch{StateCategory: &completed}
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, patchReq(t, string(blocker), "nonce-cascade-2", reqBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH blocker: status=%d body=%s", rec.Code, rec.Body.String())
	}

	if fx.items[blocked].StateCategory != core.StateStarted {
		t.Fatalf("blocked leaf state=%s, want started after blocker completed", fx.items[blocked].StateCategory)
	}
	if len(fx.op.starts) != 1 || fx.op.starts[0].assignmentID != string(blocked) {
		t.Fatalf("StartSession calls=%+v, want blocked leaf", fx.op.starts)
	}
}

func TestPatchWorkItem_ManualLeafStartDoesNotCascadeSiblings(t *testing.T) {
	epicID := core.WorkItemID("gm-e1")
	leafA := core.WorkItemID("gm-a")
	leafB := core.WorkItemID("gm-b")

	fx := newCascadeFixture(t, []core.WorkItem{
		{
			ID: epicID, Kind: "epic", Title: "Epic", StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				parentChildRel(epicID, leafA),
				parentChildRel(epicID, leafB),
			},
		},
		{
			ID: leafA, Kind: "task", Title: "A", StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(epicID, leafA)},
		},
		{
			ID: leafB, Kind: "task", Title: "B", StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{parentChildRel(epicID, leafB)},
		},
	})

	started := core.StateStarted
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, patchReq(t, string(leafA), "nonce-cascade-3",
		core.WorkItemPatch{StateCategory: &started}))
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH leaf: status=%d body=%s", rec.Code, rec.Body.String())
	}

	if fx.items[leafB].StateCategory != core.StateUnstarted {
		t.Fatalf("sibling leaf state=%s, want unstarted", fx.items[leafB].StateCategory)
	}
	if len(fx.op.starts) != 0 {
		t.Fatalf("manual leaf start should not cascade; starts=%+v", fx.op.starts)
	}
}
