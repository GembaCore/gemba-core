package mock

import (
	"context"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

type fakeRunnerWorkPlane struct {
	items map[core.WorkItemID]core.WorkItem
}

func (f *fakeRunnerWorkPlane) GetWorkItem(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
	if it, ok := f.items[id]; ok {
		return it, nil
	}
	return core.WorkItem{}, core.ErrNotFound
}

func (f *fakeRunnerWorkPlane) UpdateWorkItem(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
	it, ok := f.items[id]
	if !ok {
		return core.WorkItem{}, core.ErrNotFound
	}
	if patch.StateCategory != nil {
		it.StateCategory = *patch.StateCategory
	}
	f.items[id] = it
	return it, nil
}

func TestCloseCompletedAncestorsRollsUpEpicAndMilestone(t *testing.T) {
	ctx := context.Background()
	milestoneID := core.WorkItemID("e2e0-m1")
	epicID := core.WorkItemID("e2e0-e1")
	firstID := core.WorkItemID("e2e0-1")
	secondID := core.WorkItemID("e2e0-2")

	wp := &fakeRunnerWorkPlane{items: map[core.WorkItemID]core.WorkItem{
		milestoneID: {
			ID:            milestoneID,
			Kind:          core.KindMilestone,
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: milestoneID, To: epicID},
			},
		},
		epicID: {
			ID:            epicID,
			Kind:          "epic",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: milestoneID, To: epicID},
				{Kind: core.RelParentChild, From: epicID, To: firstID},
				{Kind: core.RelParentChild, From: epicID, To: secondID},
			},
		},
		firstID: {
			ID:            firstID,
			Kind:          "task",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: epicID, To: firstID},
			},
		},
		secondID: {
			ID:            secondID,
			Kind:          "task",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: epicID, To: secondID},
			},
		},
	}}

	if err := closeCompletedAncestors(ctx, wp, string(secondID), map[core.WorkItemID]bool{}); err != nil {
		t.Fatalf("closeCompletedAncestors: %v", err)
	}
	if got := wp.items[epicID].StateCategory; got != core.StateCompleted {
		t.Fatalf("epic StateCategory=%q, want %q", got, core.StateCompleted)
	}
	if got := wp.items[milestoneID].StateCategory; got != core.StateCompleted {
		t.Fatalf("milestone StateCategory=%q, want %q", got, core.StateCompleted)
	}
}

func TestCloseCompletedAncestorsLeavesParentOpenWhenSiblingOpen(t *testing.T) {
	ctx := context.Background()
	epicID := core.WorkItemID("e2e0-e1")
	firstID := core.WorkItemID("e2e0-1")
	secondID := core.WorkItemID("e2e0-2")

	wp := &fakeRunnerWorkPlane{items: map[core.WorkItemID]core.WorkItem{
		epicID: {
			ID:            epicID,
			Kind:          "epic",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: epicID, To: firstID},
				{Kind: core.RelParentChild, From: epicID, To: secondID},
			},
		},
		firstID: {
			ID:            firstID,
			Kind:          "task",
			StateCategory: core.StateCompleted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: epicID, To: firstID},
			},
		},
		secondID: {
			ID:            secondID,
			Kind:          "task",
			StateCategory: core.StateUnstarted,
			Relationships: []core.Relationship{
				{Kind: core.RelParentChild, From: epicID, To: secondID},
			},
		},
	}}

	if err := closeCompletedAncestors(ctx, wp, string(firstID), map[core.WorkItemID]bool{}); err != nil {
		t.Fatalf("closeCompletedAncestors: %v", err)
	}
	if got := wp.items[epicID].StateCategory; got != core.StateUnstarted {
		t.Fatalf("epic StateCategory=%q, want %q", got, core.StateUnstarted)
	}
}
