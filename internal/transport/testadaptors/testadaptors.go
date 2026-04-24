// Package testadaptors provides lightweight fake adaptors used by the
// transport host tests and the adaptor-register CLI tests. They are NOT
// production adaptors — `internal/adapter/noop` (gm-e3.7) will be the
// real in-memory reference implementation. These fakes exist only to
// exercise registration + version negotiation under a controlled manifest.
package testadaptors

import (
	"context"
	"errors"
	"sync"

	"github.com/MikeBengtson/gemba/internal/core"
)

// FakeWorkPlane is a programmable core.WorkPlane for handler / transport
// tests (gm-root.1.2). The manifest is exposed directly so Describe tests
// can control the wire shape; each query / mutation method delegates to
// an optional hook (GetFn, ListFn, …). When a hook is nil the method
// returns a sentinel error instead of silently succeeding — that way a
// test that exercises a path it forgot to program fails loudly.
//
// Every call is recorded under a mutex so a test can assert "the handler
// called ListWorkItems once with filter={}" without the fake having to
// wire up custom counters per test.
type FakeWorkPlane struct {
	Manifest    core.CapabilityManifest
	DescribeErr error // when non-nil, Describe returns this instead of Manifest

	// Programmable hooks. Nil → the sentinel-error default path fires.
	GetFn        func(ctx context.Context, id core.WorkItemID) (core.WorkItem, error)
	ListFn       func(ctx context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error)
	CreateFn     func(ctx context.Context, wi core.WorkItem) (core.WorkItem, error)
	UpdateFn     func(ctx context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error)
	AttachFn     func(ctx context.Context, id core.WorkItemID, ev core.Evidence) error
	SprintsFn    func(ctx context.Context) ([]core.Sprint, error)
	BudgetRollFn func(ctx context.Context, sprintID string) (core.BudgetRollup, error)

	// emitter fans WorkPlaneEvents out to Subscribe callers. Tests
	// drive it via EmitEvent — the fake doesn't auto-emit on
	// Create/Update so tests can control the exact event sequence.
	emitter *core.WorkPlaneEmitter

	mu          sync.Mutex
	describeN   int
	getCalls    []core.WorkItemID
	listCalls   []core.WorkItemFilter
	attachCalls []FakeAttachCall
	updateCalls []FakeUpdateCall
	createCalls []core.WorkItem
	budgetCalls []string
	sprintsN    int
}

// FakeAttachCall captures an AttachEvidence invocation.
type FakeAttachCall struct {
	ID       core.WorkItemID
	Evidence core.Evidence
}

// FakeUpdateCall captures an UpdateWorkItem invocation.
type FakeUpdateCall struct {
	ID    core.WorkItemID
	Patch core.WorkItemPatch
}

// NewFakeWorkPlane returns a FakeWorkPlane with a conformant manifest for
// the given transport. Callers override fields (Manifest, DescribeErr,
// any *Fn hook) as needed.
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
		emitter: core.NewWorkPlaneEmitter(),
	}
}

// DescribeCalls returns the number of times Describe has been invoked.
func (f *FakeWorkPlane) DescribeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.describeN
}

// GetCalls returns a copy of every GetWorkItem id observed.
func (f *FakeWorkPlane) GetCalls() []core.WorkItemID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]core.WorkItemID(nil), f.getCalls...)
}

// ListCalls returns a copy of every ListWorkItems filter observed.
func (f *FakeWorkPlane) ListCalls() []core.WorkItemFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]core.WorkItemFilter(nil), f.listCalls...)
}

// CreateCalls returns a copy of every CreateWorkItem payload observed.
func (f *FakeWorkPlane) CreateCalls() []core.WorkItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]core.WorkItem(nil), f.createCalls...)
}

// UpdateCalls returns a copy of every UpdateWorkItem invocation.
func (f *FakeWorkPlane) UpdateCalls() []FakeUpdateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeUpdateCall(nil), f.updateCalls...)
}

// AttachCalls returns a copy of every AttachEvidence invocation.
func (f *FakeWorkPlane) AttachCalls() []FakeAttachCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeAttachCall(nil), f.attachCalls...)
}

// BudgetCalls returns a copy of every ReadBudgetRollup sprint id observed.
func (f *FakeWorkPlane) BudgetCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.budgetCalls...)
}

// SprintsCalls returns the number of times ListSprints has been invoked.
func (f *FakeWorkPlane) SprintsCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sprintsN
}

func (f *FakeWorkPlane) Describe(_ context.Context) (core.CapabilityManifest, error) {
	f.mu.Lock()
	f.describeN++
	f.mu.Unlock()
	if f.DescribeErr != nil {
		return core.CapabilityManifest{}, f.DescribeErr
	}
	return f.Manifest, nil
}

func (f *FakeWorkPlane) ListWorkItems(ctx context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
	f.mu.Lock()
	f.listCalls = append(f.listCalls, filter)
	f.mu.Unlock()
	if f.ListFn == nil {
		return nil, errors.New("fake: ListWorkItems not programmed")
	}
	return f.ListFn(ctx, filter)
}

func (f *FakeWorkPlane) GetWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error) {
	f.mu.Lock()
	f.getCalls = append(f.getCalls, id)
	f.mu.Unlock()
	if f.GetFn == nil {
		return core.WorkItem{}, core.ErrNotFound
	}
	return f.GetFn(ctx, id)
}

func (f *FakeWorkPlane) CreateWorkItem(ctx context.Context, wi core.WorkItem) (core.WorkItem, error) {
	f.mu.Lock()
	f.createCalls = append(f.createCalls, wi)
	f.mu.Unlock()
	if f.CreateFn == nil {
		return core.WorkItem{}, errors.New("fake: CreateWorkItem not programmed")
	}
	return f.CreateFn(ctx, wi)
}

func (f *FakeWorkPlane) UpdateWorkItem(ctx context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
	f.mu.Lock()
	f.updateCalls = append(f.updateCalls, FakeUpdateCall{ID: id, Patch: patch})
	f.mu.Unlock()
	if f.UpdateFn == nil {
		return core.WorkItem{}, errors.New("fake: UpdateWorkItem not programmed")
	}
	return f.UpdateFn(ctx, id, patch)
}

func (f *FakeWorkPlane) AttachEvidence(ctx context.Context, id core.WorkItemID, ev core.Evidence) error {
	f.mu.Lock()
	f.attachCalls = append(f.attachCalls, FakeAttachCall{ID: id, Evidence: ev})
	f.mu.Unlock()
	if f.AttachFn == nil {
		return core.ErrUnsupported
	}
	return f.AttachFn(ctx, id, ev)
}

func (f *FakeWorkPlane) ListSprints(ctx context.Context) ([]core.Sprint, error) {
	f.mu.Lock()
	f.sprintsN++
	f.mu.Unlock()
	if f.SprintsFn == nil {
		return nil, nil
	}
	return f.SprintsFn(ctx)
}

func (f *FakeWorkPlane) ReadBudgetRollup(ctx context.Context, sprintID string) (core.BudgetRollup, error) {
	f.mu.Lock()
	f.budgetCalls = append(f.budgetCalls, sprintID)
	f.mu.Unlock()
	if f.BudgetRollFn == nil {
		return core.BudgetRollup{}, core.ErrUnsupported
	}
	return f.BudgetRollFn(ctx, sprintID)
}

// Subscribe returns a channel fed by EmitEvent. Tests use it to
// verify the /events fan-out chain (Subscribe → translate →
// GembaEvent → hub → SSE) without needing a live bd subprocess.
func (f *FakeWorkPlane) Subscribe(ctx context.Context, filter core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	if f.emitter == nil {
		f.emitter = core.NewWorkPlaneEmitter()
	}
	return f.emitter.Subscribe(ctx, filter), nil
}

// EmitEvent publishes ev through the fake's emitter. Tests that want
// to exercise the Subscribe pipeline call this after whatever setup
// they need.
func (f *FakeWorkPlane) EmitEvent(ev core.WorkPlaneEvent) {
	if f.emitter == nil {
		return
	}
	f.emitter.Publish(ev)
}

// FakeOrchestrationPlane is the OrchestrationPlaneAdaptor analogue.
// Programmable hooks let tests dispatch concrete responses without a
// custom struct. Same convention as FakeWorkPlane: nil hook → the
// per-method default (typically nil/empty).
type FakeOrchestrationPlane struct {
	Manifest core.OrchestrationCapabilityManifest

	// AgentsFn — when set, ListAgents dispatches through it. Default
	// returns nil, nil (no agents known).
	AgentsFn func(ctx context.Context, f core.AgentFilter) ([]core.AgentRef, error)

	// SessionsFn — when set, ListSessions dispatches through it.
	// Default returns nil, nil so tests that don't care about session
	// inventory get an empty list.
	SessionsFn func(ctx context.Context, f core.SessionFilter) ([]core.Session, error)
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
func (f *FakeOrchestrationPlane) ListAgents(ctx context.Context, filter core.AgentFilter) ([]core.AgentRef, error) {
	if f.AgentsFn != nil {
		return f.AgentsFn(ctx, filter)
	}
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
func (f *FakeOrchestrationPlane) ListSessions(ctx context.Context, filter core.SessionFilter) ([]core.Session, error) {
	if f.SessionsFn != nil {
		return f.SessionsFn(ctx, filter)
	}
	return nil, nil
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
