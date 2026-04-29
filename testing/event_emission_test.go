package gembatesting_test

import (
	"context"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	gembatesting "github.com/GembaCore/gemba-core/testing"
)

// TestProbe_MutationWithoutEvent_NamedFailure proves the contract from
// gm-e3.6.3's DoD: "A new adaptor that wires StartSession but forgets
// to emit session.transition fails the harness with a clear
// 'mutation_without_event' error message." The probes here drive a
// silent-mutator adaptor (state changes succeed, Subscribe yields no
// events) and assert the failure messages mention mutation_without_event
// by name.
func TestProbe_MutationWithoutEvent_NamedFailure(t *testing.T) {
	t.Run("workplane create probe surfaces tag", func(t *testing.T) {
		report := gembatesting.RunWorkPlaneProbes(silentWorkPlane{}, nil)
		assertReportNamesMutationWithoutEvent(t, report)
	})
	t.Run("orchestration pause probe surfaces tag", func(t *testing.T) {
		impl := newSilentOrchestration()
		report := gembatesting.RunOrchestrationProbes(impl, &gembatesting.OrchestrationFixture{
			ProgrammaticSessionStarter: func(_ core.OrchestrationPlaneAdaptor) (string, func(), error) {
				return impl.startTestSession(), nil, nil
			},
		})
		assertReportNamesMutationWithoutEvent(t, report)
	})
}

func assertReportNamesMutationWithoutEvent(t *testing.T, r *gembatesting.Report) {
	t.Helper()
	if r.Passed() {
		t.Fatal("expected silent-mutator adaptor to fail the conformance run")
	}
	for _, g := range r.Groups {
		for _, p := range g.Probes {
			if p.Passed || p.Skipped {
				continue
			}
			for _, m := range p.Messages {
				if strings.Contains(m, "mutation_without_event") {
					return // tag surfaced
				}
			}
		}
	}
	t.Fatalf("no failing probe carried the 'mutation_without_event' tag; report=%+v", r)
}

// silentWorkPlane mutates state but never emits — the failure mode the
// new event-emission probes exist to catch.
type silentWorkPlane struct{}

func (silentWorkPlane) Describe(context.Context) (core.CapabilityManifest, error) {
	return core.CapabilityManifest{
		AdaptorName:     "silent",
		AdaptorVersion:  "0.0.1",
		ProtocolVersion: core.ProtocolVersion,
		Transport:       core.TransportJSONL,
		StateMap:        core.StateMap{"open": core.StateUnstarted},
	}, nil
}
func (silentWorkPlane) ListWorkItems(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
	return nil, nil
}
func (silentWorkPlane) GetWorkItem(context.Context, core.WorkItemID) (core.WorkItem, error) {
	return core.WorkItem{}, nil
}
func (silentWorkPlane) CreateWorkItem(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
	if wi.ID == "" {
		wi.ID = "silent-1"
	}
	return wi, nil
}
func (silentWorkPlane) UpdateWorkItem(_ context.Context, _ core.WorkItemID, _ core.WorkItemPatch) (core.WorkItem, error) {
	return core.WorkItem{Title: "after"}, nil
}
func (silentWorkPlane) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error {
	return core.ErrUnsupported
}
func (silentWorkPlane) ListSprints(context.Context) ([]core.Sprint, error) { return nil, nil }
func (silentWorkPlane) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, core.ErrUnsupported
}
func (silentWorkPlane) Subscribe(ctx context.Context, _ core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	ch := make(chan core.WorkPlaneEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

// silentOrchestration mirrors silentWorkPlane on the OrchestrationPlane
// side: state mutations succeed, Subscribe never emits.
type silentOrchestration struct {
	sessions map[string]*core.Session
}

func newSilentOrchestration() *silentOrchestration {
	return &silentOrchestration{sessions: map[string]*core.Session{}}
}

func (o *silentOrchestration) startTestSession() string {
	id := "silent-session"
	o.sessions[id] = &core.Session{ID: id, Status: core.SessionWorking}
	return id
}

func (o *silentOrchestration) Describe() core.OrchestrationCapabilityManifest {
	return core.OrchestrationCapabilityManifest{
		AdaptorID:               "silent",
		AdaptorVersion:          "0.0.1",
		OrchestrationAPIVersion: core.ProtocolVersion,
		Transport:               core.TransportJSONL,
		WorkspaceKinds:          []core.WorkspaceKind{core.WorkspaceSubprocess},
		GroupModes:              []core.GroupMode{core.GroupStatic},
		CostAxes:                []core.CostAxis{core.CostWallclock},
		EscalationKinds:         []core.EscalationKind{core.EscalationHITLApproval},
		PeekModes:               []core.PeekMode{core.PeekTranscript},
		EventDelivery:           core.EventDeliveryPush,
	}
}
func (o *silentOrchestration) DeclaredState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{}, nil
}
func (o *silentOrchestration) ObservedState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{}, nil
}
func (o *silentOrchestration) ListAgents(context.Context, core.AgentFilter) ([]core.AgentRef, error) {
	return nil, nil
}
func (o *silentOrchestration) ReadAgent(context.Context, core.AgentID) (*core.AgentRef, error) {
	return nil, nil
}
func (o *silentOrchestration) ListGroups(context.Context) ([]core.AgentGroup, error) {
	return nil, nil
}
func (o *silentOrchestration) ResolveGroupMembers(context.Context, string) ([]core.AgentRef, error) {
	return nil, nil
}
func (o *silentOrchestration) ClaimNextReady(context.Context, core.ReadyFilter, core.AgentRef) (*core.Reservation, error) {
	return nil, nil
}
func (o *silentOrchestration) ReleaseReservation(context.Context, string) error { return nil }
func (o *silentOrchestration) StartSession(context.Context, string, core.SessionPrompt) (core.Session, error) {
	return o.startSession(), nil
}
func (o *silentOrchestration) startSession() core.Session {
	return *o.sessions[o.startTestSession()]
}
func (o *silentOrchestration) PauseSession(_ context.Context, sessionID string, _ core.ConfirmNonce) (core.Session, error) {
	s, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound, "silent: %q unknown", sessionID)
	}
	s.Status = core.SessionSuspended
	return *s, nil
}
func (o *silentOrchestration) ResumeSession(_ context.Context, sessionID string, _ core.ConfirmNonce) (core.Session, error) {
	s, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound, "silent: %q unknown", sessionID)
	}
	s.Status = core.SessionWorking
	return *s, nil
}
func (o *silentOrchestration) EndSession(_ context.Context, sessionID string, _ core.SessionEndMode, _ core.ConfirmNonce) (core.Session, error) {
	s, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound, "silent: %q unknown", sessionID)
	}
	s.Status = core.SessionCompleted
	r := core.CloseUserStop
	s.CloseReason = &r
	return *s, nil
}
func (o *silentOrchestration) PeekSession(context.Context, string) (core.SessionPeek, error) {
	return core.SessionPeek{}, nil
}
func (o *silentOrchestration) ListPendingRequests(context.Context, string) ([]core.EscalationRequest, error) {
	return []core.EscalationRequest{}, nil
}
func (o *silentOrchestration) ListSessions(context.Context, core.SessionFilter) ([]core.Session, error) {
	return []core.Session{}, nil
}
func (o *silentOrchestration) AcquireWorkspace(context.Context, core.WorkspaceRequest) (core.Workspace, error) {
	return core.Workspace{}, nil
}
func (o *silentOrchestration) ReleaseWorkspace(context.Context, string) error { return nil }
func (o *silentOrchestration) InspectWorkspace(context.Context, string) (core.Workspace, error) {
	return core.Workspace{}, nil
}
func (o *silentOrchestration) ListOpenEscalations(context.Context, core.EscalationFilter) ([]core.EscalationRequest, error) {
	return nil, nil
}
func (o *silentOrchestration) ResolveEscalation(context.Context, string, core.EscalationResolution, core.ConfirmNonce) (core.EscalationRequest, error) {
	return core.EscalationRequest{}, nil
}
func (o *silentOrchestration) Subscribe(ctx context.Context, _ core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	ch := make(chan core.OrchestrationEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}
