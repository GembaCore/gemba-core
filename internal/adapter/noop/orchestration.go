package noop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// OrchestrationPlane is a minimal in-memory core.OrchestrationPlaneAdaptor.
// It tracks assignments, sessions, and workspaces in memory and implements
// just enough of the lifecycle for the conformance harness:
//
//   - StartSession against an assignment created via CreateAssignment.
//   - EndSession idempotent under a same-nonce replay and absorbing on a
//     terminal session under a fresh nonce (t3code audit).
//   - Typed SessionCloseReason populated on first terminal close.
//   - ListPendingRequests returning an empty slice (not nil) for a
//     running session, KindSessionNotFound for an unknown id.
//
// Everything else returns KindProcessFailed with a "not implemented by
// noop" message so callers can still branch on the tagged envelope.
type OrchestrationPlane struct {
	mu          sync.Mutex
	assignments map[string]core.Assignment
	sessions    map[string]*core.Session
	// closureNonces deduplicates EndSession same-nonce replays.
	closureNonces map[string]core.ConfirmNonce
}

// NewOrchestrationPlane returns a OrchestrationPlane with empty stores.
func NewOrchestrationPlane() *OrchestrationPlane {
	return &OrchestrationPlane{
		assignments:   make(map[string]core.Assignment),
		sessions:      make(map[string]*core.Session),
		closureNonces: make(map[string]core.ConfirmNonce),
	}
}

var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)

var noopOrchestrationManifest = core.OrchestrationCapabilityManifest{
	AdaptorID:               "noop",
	AdaptorVersion:          "0.1.0",
	OrchestrationAPIVersion: core.ProtocolVersion,
	Transport:               core.TransportJSONL,
	WorkspaceKinds:          []core.WorkspaceKind{core.WorkspaceSubprocess},
	DefaultWorkspaceKind:    core.WorkspaceSubprocess,
	PerKindIsolation: map[core.WorkspaceKind]core.IsolationCapabilities{
		core.WorkspaceSubprocess: {FSScoped: true},
	},
	GroupModes:      []core.GroupMode{core.GroupStatic},
	CostAxes:        []core.CostAxis{core.CostWallclock},
	EscalationKinds: []core.EscalationKind{core.EscalationHITLApproval},
	PeekModes:       []core.PeekMode{core.PeekTranscript},
	EventDelivery:   core.EventDeliveryPoll,
}

// CreateAssignment registers an Assignment so a subsequent StartSession
// with the same id succeeds. Used by conformance fixtures.
func (o *OrchestrationPlane) CreateAssignment(a core.Assignment) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if a.ID == "" {
		a.ID = fmt.Sprintf("assign-%d", len(o.assignments)+1)
	}
	o.assignments[a.ID] = a
}

func (o *OrchestrationPlane) Describe() core.OrchestrationCapabilityManifest {
	return noopOrchestrationManifest
}

func (o *OrchestrationPlane) DeclaredState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{CapturedAt: time.Now()}, nil
}

func (o *OrchestrationPlane) ObservedState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{CapturedAt: time.Now()}, nil
}

func (o *OrchestrationPlane) ListAgents(context.Context, core.AgentFilter) ([]core.AgentRef, error) {
	return nil, nil
}

func (o *OrchestrationPlane) ReadAgent(context.Context, core.AgentID) (*core.AgentRef, error) {
	return nil, nil
}

func (o *OrchestrationPlane) ListGroups(context.Context) ([]core.AgentGroup, error) {
	return nil, nil
}

func (o *OrchestrationPlane) ResolveGroupMembers(_ context.Context, groupID string) ([]core.AgentRef, error) {
	return nil, core.NewAdaptorError(core.KindSessionNotFound,
		"noop: group %q unknown", groupID)
}

func (o *OrchestrationPlane) ClaimNextReady(context.Context, core.ReadyFilter, core.AgentRef) (*core.Reservation, error) {
	return nil, nil
}

func (o *OrchestrationPlane) ReleaseReservation(_ context.Context, id string) error {
	return core.NewAdaptorError(core.KindSessionNotFound,
		"noop: reservation %q unknown", id)
}

func (o *OrchestrationPlane) StartSession(_ context.Context, assignmentID string, _ core.SessionPrompt) (core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	a, ok := o.assignments[assignmentID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: assignment %q unknown", assignmentID)
	}
	sessID := fmt.Sprintf("sess-%s-%d", assignmentID, len(o.sessions)+1)
	s := &core.Session{
		ID:           sessID,
		AssignmentID: a.ID,
		AgentID:      a.AgentID,
		Status:       core.SessionRunning,
		StartedAt:    time.Now(),
	}
	o.sessions[sessID] = s
	return *s, nil
}

func (o *OrchestrationPlane) PauseSession(_ context.Context, sessionID string, _ core.ConfirmNonce) (core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: session %q unknown", sessionID)
	}
	if isTerminal(s.Status) {
		return *s, core.NewAdaptorError(core.KindSessionClosed,
			"noop: session %q already terminal", sessionID)
	}
	s.Status = core.SessionSuspended
	return *s, nil
}

func (o *OrchestrationPlane) ResumeSession(_ context.Context, sessionID string, _ core.ConfirmNonce) (core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: session %q unknown", sessionID)
	}
	if isTerminal(s.Status) {
		return *s, core.NewAdaptorError(core.KindSessionClosed,
			"noop: session %q already terminal", sessionID)
	}
	s.Status = core.SessionRunning
	return *s, nil
}

// EndSession is doubly idempotent per the contract:
//
//  1. Same-nonce replay: return the prior Session, no error, no state change.
//  2. Terminal-absorbing: fresh nonce on an already-terminal session is a
//     no-op — return the terminal Session, no error.
func (o *OrchestrationPlane) EndSession(
	_ context.Context, sessionID string, mode core.SessionEndMode, nonce core.ConfirmNonce,
) (core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	s, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: session %q unknown", sessionID)
	}

	if prior, seen := o.closureNonces[sessionID]; seen && prior == nonce {
		return *s, nil
	}
	if isTerminal(s.Status) {
		return *s, nil
	}

	switch mode {
	case core.SessionEndFailed:
		s.Status = core.SessionFailed
	default:
		s.Status = core.SessionCompleted
	}
	now := time.Now()
	s.EndedAt = &now
	reason := core.CloseUserStop
	s.CloseReason = &reason
	o.closureNonces[sessionID] = nonce
	return *s, nil
}

func (o *OrchestrationPlane) PeekSession(_ context.Context, sessionID string) (core.SessionPeek, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.sessions[sessionID]
	if !ok {
		return core.SessionPeek{}, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: session %q unknown", sessionID)
	}
	return core.SessionPeek{
		SessionID:  s.ID,
		Status:     s.Status,
		CapturedAt: time.Now(),
	}, nil
}

func (o *OrchestrationPlane) ListPendingRequests(_ context.Context, sessionID string) ([]core.EscalationRequest, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.sessions[sessionID]; !ok {
		return nil, core.NewAdaptorError(core.KindSessionNotFound,
			"noop: session %q unknown", sessionID)
	}
	// Contract: return an empty slice (not nil) when nothing is pending
	// so callers can distinguish "no escalations" from "unknown session".
	return []core.EscalationRequest{}, nil
}

func (o *OrchestrationPlane) AcquireWorkspace(_ context.Context, req core.WorkspaceRequest) (core.Workspace, error) {
	return core.Workspace{
		ID:         fmt.Sprintf("ws-%s", req.AssignmentID),
		Kind:       core.WorkspaceSubprocess,
		Repository: req.Repository,
		Branch:     req.Branch,
		Status:     core.WorkspaceReady,
		Isolation:  core.IsolationCapabilities{FSScoped: true},
		CreatedAt:  time.Now(),
	}, nil
}

func (o *OrchestrationPlane) ReleaseWorkspace(_ context.Context, _ string) error {
	return nil
}

func (o *OrchestrationPlane) InspectWorkspace(_ context.Context, workspaceID string) (core.Workspace, error) {
	return core.Workspace{}, core.NewAdaptorError(core.KindSessionNotFound,
		"noop: workspace %q unknown", workspaceID)
}

func (o *OrchestrationPlane) ListOpenEscalations(context.Context, core.EscalationFilter) ([]core.EscalationRequest, error) {
	return nil, nil
}

func (o *OrchestrationPlane) ResolveEscalation(
	_ context.Context, escalationID string, _ core.EscalationResolution, _ core.ConfirmNonce,
) (core.EscalationRequest, error) {
	return core.EscalationRequest{}, core.NewAdaptorError(core.KindSessionNotFound,
		"noop: escalation %q unknown", escalationID)
}

func (o *OrchestrationPlane) Subscribe(ctx context.Context, _ core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	ch := make(chan core.OrchestrationEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

func isTerminal(s core.SessionStatus) bool {
	return s == core.SessionCompleted || s == core.SessionFailed
}
