package noop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
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
	// subscribers holds the per-Subscribe-call sink channels. Mutators
	// fan-out a typed OrchestrationEvent through these so the
	// conformance harness's Group D event-emission probes (gm-e3.6.3)
	// can observe state-changing methods. Slow subscribers drop events
	// rather than block the mutator.
	subscribers []chan core.OrchestrationEvent
	seq         int
	// manifest is per-instance so NewOrchestrationPlaneWithTransport can
	// override Transport without mutating package state (gm-e3.7).
	manifest core.OrchestrationCapabilityManifest
}

// NewOrchestrationPlane returns an OrchestrationPlane with empty stores
// and the default jsonl transport. Use NewOrchestrationPlaneWithTransport
// when binding to a different transport host (e.g. the api Host).
func NewOrchestrationPlane() *OrchestrationPlane {
	return NewOrchestrationPlaneWithTransport(core.TransportJSONL)
}

// NewOrchestrationPlaneWithTransport returns an OrchestrationPlane whose
// manifest declares the given transport. Used by `gemba serve --noop` to
// bind on the api host (gm-e3.7) without the transport-mismatch guard
// rejecting the registration.
func NewOrchestrationPlaneWithTransport(t core.Transport) *OrchestrationPlane {
	m := defaultOrchestrationManifest
	m.Transport = t
	return &OrchestrationPlane{
		assignments:   make(map[string]core.Assignment),
		sessions:      make(map[string]*core.Session),
		closureNonces: make(map[string]core.ConfirmNonce),
		manifest:      m,
	}
}

// publish delivers ev to every active subscriber. Caller MUST hold
// o.mu — that keeps subscriber registration and event ordering
// consistent under concurrent mutators. Drops on a full sink so a
// slow subscriber can't stall the mutator path; the harness uses
// buffered channels so tests don't hit the drop branch.
func (o *OrchestrationPlane) publish(ev core.OrchestrationEvent) {
	o.seq++
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("noop-evt-%d", o.seq)
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	for _, ch := range o.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)

var defaultOrchestrationManifest = core.OrchestrationCapabilityManifest{
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
	// ClaimModel: the noop adaptor mints sessions inline against an
	// existing Assignment; there is no out-of-band reservation
	// surface. Inline matches the production adaptors (gt, native)
	// the harness exercises.
	ClaimModel: core.ClaimModelInline,
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
	return o.manifest
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
		Status:       core.SessionWorking,
		StartedAt:    time.Now(),
	}
	o.sessions[sessID] = s
	o.publish(core.OrchestrationEvent{
		Kind:         "session_transition",
		AssignmentID: a.ID,
		SessionID:    sessID,
		AgentID:      a.AgentID,
		Payload: map[string]any{
			"from": "",
			"to":   string(s.Status),
		},
	})
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
	prior := s.Status
	s.Status = core.SessionSuspended
	o.publish(core.OrchestrationEvent{
		Kind:         "session_transition",
		AssignmentID: s.AssignmentID,
		SessionID:    s.ID,
		AgentID:      s.AgentID,
		Payload: map[string]any{
			"from": string(prior),
			"to":   string(s.Status),
		},
	})
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
	prior := s.Status
	s.Status = core.SessionWorking
	o.publish(core.OrchestrationEvent{
		Kind:         "session_transition",
		AssignmentID: s.AssignmentID,
		SessionID:    s.ID,
		AgentID:      s.AgentID,
		Payload: map[string]any{
			"from": string(prior),
			"to":   string(s.Status),
		},
	})
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

	priorStatus := s.Status
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
	o.publish(core.OrchestrationEvent{
		Kind:         "session_transition",
		AssignmentID: s.AssignmentID,
		SessionID:    s.ID,
		AgentID:      s.AgentID,
		Payload: map[string]any{
			"from":         string(priorStatus),
			"to":           string(s.Status),
			"close_reason": string(reason),
		},
	})
	return *s, nil
}

// RecycleSession is the optional pool-lifecycle capability
// (session-pool.md §5.2). The noop adaptor doesn't model pool
// semantics — return KindUnsupported so the daemon's recycle gate
// becomes a no-op against this backend.
func (o *OrchestrationPlane) RecycleSession(_ context.Context, _ string) (core.Session, error) {
	return core.Session{}, core.NewAdaptorError(core.KindUnsupported,
		"noop: RecycleSession not supported")
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

// ListSessions returns every session the noop adaptor tracks, filtered
// by f. Useful for the conformance harness's snapshot expectations.
func (o *OrchestrationPlane) ListSessions(_ context.Context, f core.SessionFilter) ([]core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]core.Session, 0, len(o.sessions))
	for _, s := range o.sessions {
		if !f.IncludeTerminal && isTerminal(s.Status) {
			continue
		}
		if len(f.Status) > 0 {
			match := false
			for _, want := range f.Status {
				if s.Status == want {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		if f.AgentID != "" && s.AgentID != f.AgentID {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
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
	// Buffer keeps publish() non-blocking under bursts (start + pause +
	// end in one test step) without forcing the caller to drain
	// promptly. Slow subscribers still get backpressure-dropped at the
	// publish edge per the channel's docstring.
	ch := make(chan core.OrchestrationEvent, 16)
	o.mu.Lock()
	o.subscribers = append(o.subscribers, ch)
	o.mu.Unlock()
	go func() {
		<-ctx.Done()
		o.mu.Lock()
		defer o.mu.Unlock()
		for i, sub := range o.subscribers {
			if sub == ch {
				o.subscribers = append(o.subscribers[:i], o.subscribers[i+1:]...)
				break
			}
		}
		close(ch)
	}()
	return ch, nil
}

func isTerminal(s core.SessionStatus) bool {
	return s == core.SessionCompleted || s == core.SessionFailed
}
