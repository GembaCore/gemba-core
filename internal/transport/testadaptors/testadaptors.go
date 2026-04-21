// Package testadaptors provides lightweight fake adaptors used by the
// transport host tests and the adaptor-register CLI tests. They are NOT
// production adaptors — `internal/adapter/noop` (gm-e3.7) will be the
// real in-memory reference implementation. These fakes exist only to
// exercise registration + version negotiation under a controlled manifest.
package testadaptors

import (
	"context"
	"errors"

	"github.com/MikeBengtson/gemba/internal/core"
)

// FakeWorkPlane is a WorkPlane whose Describe result the test fully
// controls. Every query method returns ErrUnsupported so a fake is never
// mistaken for a usable backend.
type FakeWorkPlane struct {
	Manifest core.CapabilityManifest
	// DescribeErr, when non-nil, is returned instead of Manifest.
	DescribeErr error
}

// NewFakeWorkPlane returns a FakeWorkPlane with a conformant manifest for
// the given transport. Callers override fields as needed for mismatch tests.
func NewFakeWorkPlane(transport core.Transport) *FakeWorkPlane {
	return &FakeWorkPlane{
		Manifest: core.CapabilityManifest{
			AdaptorName:     "fake",
			AdaptorVersion:  "0.1.0",
			ProtocolVersion: core.ProtocolVersion,
			Transport:       transport,
			StateMap: core.StateMap{
				"open":   core.StateBacklog,
				"closed": core.StateCompleted,
			},
		},
	}
}

func (f *FakeWorkPlane) Describe(_ context.Context) (core.CapabilityManifest, error) {
	if f.DescribeErr != nil {
		return core.CapabilityManifest{}, f.DescribeErr
	}
	return f.Manifest, nil
}

func (*FakeWorkPlane) ListWorkItems(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
	return nil, errors.New("fake: ListWorkItems not implemented")
}
func (*FakeWorkPlane) GetWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error) {
	return core.WorkItem{}, core.ErrNotFound
}
func (*FakeWorkPlane) CreateWorkItem(context.Context, core.WorkItem) (core.WorkItem, error) {
	return core.WorkItem{}, errors.New("fake: CreateWorkItem not implemented")
}
func (*FakeWorkPlane) UpdateWorkItem(context.Context, core.WorkItemID, core.WorkItemPatch) (core.WorkItem, error) {
	return core.WorkItem{}, errors.New("fake: UpdateWorkItem not implemented")
}
func (*FakeWorkPlane) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error {
	return core.ErrUnsupported
}
func (*FakeWorkPlane) ListSprints(context.Context) ([]core.Sprint, error) { return nil, nil }
func (*FakeWorkPlane) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, core.ErrUnsupported
}

// FakeOrchestrationPlane is the OrchestrationPlaneAdaptor analogue.
type FakeOrchestrationPlane struct {
	Manifest core.OrchestrationCapabilityManifest
}

// NewFakeOrchestrationPlane returns a FakeOrchestrationPlane carrying a
// conformant manifest for the given transport.
func NewFakeOrchestrationPlane(transport core.Transport) *FakeOrchestrationPlane {
	return &FakeOrchestrationPlane{
		Manifest: core.OrchestrationCapabilityManifest{
			AdaptorID:               "fake-orch",
			AdaptorVersion:          "0.1.0",
			OrchestrationAPIVersion: core.ProtocolVersion,
			Transport:               transport,
			WorkspaceKinds:          []core.WorkspaceKind{core.WorkspaceExec},
			GroupModes:              []core.GroupMode{core.GroupStatic},
			CostAxes:                []core.CostAxis{core.CostWallclock},
			EscalationKinds:         []core.EscalationKind{core.EscalationHITLApproval},
			PeekModes:               []core.PeekMode{core.PeekStructured},
		},
	}
}

func (f *FakeOrchestrationPlane) Describe() core.OrchestrationCapabilityManifest { return f.Manifest }

func (*FakeOrchestrationPlane) DeclaredState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{}, nil
}
func (*FakeOrchestrationPlane) ObservedState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{}, nil
}
func (*FakeOrchestrationPlane) ListAgents(context.Context, core.AgentFilter) ([]core.AgentRef, error) {
	return nil, nil
}
func (*FakeOrchestrationPlane) ReadAgent(context.Context, core.AgentID) (*core.AgentRef, error) {
	return nil, nil
}
func (*FakeOrchestrationPlane) ListGroups(context.Context) ([]core.AgentGroup, error) {
	return nil, nil
}
func (*FakeOrchestrationPlane) ResolveGroupMembers(context.Context, string) ([]core.AgentRef, error) {
	return nil, nil
}
func (*FakeOrchestrationPlane) ClaimNextReady(context.Context, core.ReadyFilter, core.AgentRef) (*core.Reservation, error) {
	return nil, nil
}
func (*FakeOrchestrationPlane) ReleaseReservation(context.Context, string) error { return nil }
func (*FakeOrchestrationPlane) StartSession(context.Context, string, core.SessionPrompt) (core.Session, error) {
	return core.Session{}, errors.New("fake: StartSession not implemented")
}
func (*FakeOrchestrationPlane) PauseSession(context.Context, string, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, errors.New("fake: PauseSession not implemented")
}
func (*FakeOrchestrationPlane) ResumeSession(context.Context, string, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, errors.New("fake: ResumeSession not implemented")
}
func (*FakeOrchestrationPlane) EndSession(context.Context, string, core.SessionEndMode, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, errors.New("fake: EndSession not implemented")
}
func (*FakeOrchestrationPlane) PeekSession(context.Context, string) (core.SessionPeek, error) {
	return core.SessionPeek{}, errors.New("fake: PeekSession not implemented")
}
func (*FakeOrchestrationPlane) ListPendingRequests(context.Context, string) ([]core.EscalationRequest, error) {
	return []core.EscalationRequest{}, nil
}
func (*FakeOrchestrationPlane) AcquireWorkspace(context.Context, core.WorkspaceRequest) (core.Workspace, error) {
	return core.Workspace{}, errors.New("fake: AcquireWorkspace not implemented")
}
func (*FakeOrchestrationPlane) ReleaseWorkspace(context.Context, string) error { return nil }
func (*FakeOrchestrationPlane) InspectWorkspace(context.Context, string) (core.Workspace, error) {
	return core.Workspace{}, errors.New("fake: InspectWorkspace not implemented")
}
func (*FakeOrchestrationPlane) ListOpenEscalations(context.Context, core.EscalationFilter) ([]core.EscalationRequest, error) {
	return nil, nil
}
func (*FakeOrchestrationPlane) ResolveEscalation(context.Context, string, core.EscalationResolution, core.ConfirmNonce) (core.EscalationRequest, error) {
	return core.EscalationRequest{}, errors.New("fake: ResolveEscalation not implemented")
}
func (*FakeOrchestrationPlane) Subscribe(context.Context, core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	ch := make(chan core.OrchestrationEvent)
	close(ch)
	return ch, nil
}
