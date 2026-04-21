package noop

import (
	"context"
	"sync"

	"github.com/MikeBengtson/gemba/internal/core"
)

// WorkPlane is a minimal in-memory core.WorkPlane. It declares a
// capability-minimal manifest (sprint_native=false, token_budget_enforced=false,
// evidence_synthesis_required=false) so every optional feature group is
// gated off; the adaptor then fails fast with capability_denied on the
// corresponding methods. This is the canonical shape a new adaptor
// starts from before it opts into extension features.
type WorkPlane struct {
	mu    sync.Mutex
	items map[core.WorkItemID]core.WorkItem
}

// NewWorkPlane returns a WorkPlane with an empty in-memory store.
func NewWorkPlane() *WorkPlane {
	return &WorkPlane{items: make(map[core.WorkItemID]core.WorkItem)}
}

var _ core.WorkPlane = (*WorkPlane)(nil)

var noopWorkPlaneManifest = core.CapabilityManifest{
	AdaptorName:     "noop",
	AdaptorVersion:  "0.1.0",
	ProtocolVersion: core.ProtocolVersion,
	Transport:       core.TransportJSONL,
	StateMap: core.StateMap{
		"open":        core.StateUnstarted,
		"in_progress": core.StateStarted,
		"closed":      core.StateCompleted,
	},
	SprintNative:              false,
	TokenBudgetEnforced:       false,
	EvidenceSynthesisRequired: false,
}

func (w *WorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	return noopWorkPlaneManifest, nil
}

func (w *WorkPlane) ListWorkItems(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]core.WorkItem, 0, len(w.items))
	for _, it := range w.items {
		out = append(out, it)
	}
	return out, nil
}

func (w *WorkPlane) GetWorkItem(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	it, ok := w.items[id]
	if !ok {
		return core.WorkItem{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: work item %q not found", id)
	}
	return it, nil
}

func (w *WorkPlane) CreateWorkItem(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.items[wi.ID] = wi
	return wi, nil
}

func (w *WorkPlane) UpdateWorkItem(
	_ context.Context, id core.WorkItemID, patch core.WorkItemPatch,
) (core.WorkItem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	it, ok := w.items[id]
	if !ok {
		return core.WorkItem{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: work item %q not found", id)
	}
	if patch.Title != nil {
		it.Title = *patch.Title
	}
	if patch.Description != nil {
		it.Description = *patch.Description
	}
	if patch.Status != nil {
		it.Status = *patch.Status
	}
	if patch.StateCategory != nil {
		it.StateCategory = *patch.StateCategory
	}
	w.items[id] = it
	return it, nil
}

func (w *WorkPlane) AttachEvidence(_ context.Context, _ core.WorkItemID, _ core.Evidence) error {
	return core.EnforceCapability(noopWorkPlaneManifest, core.OpAttachEvidence)
}

func (w *WorkPlane) ListSprints(_ context.Context) ([]core.Sprint, error) {
	return nil, core.EnforceCapability(noopWorkPlaneManifest, core.OpListSprints)
}

func (w *WorkPlane) ReadBudgetRollup(_ context.Context, _ string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, core.EnforceCapability(noopWorkPlaneManifest, core.OpReadBudgetRollup)
}
