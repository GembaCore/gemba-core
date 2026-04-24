package native

import (
	"context"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/native/bridge"
	"github.com/MikeBengtson/gemba/internal/core"
)

// OrchestrationPlane is the native adaptor. gm-native.9 extends the
// scaffold with a real StartSession path; methods that haven't been
// implemented yet still return KindUnsupported.
type OrchestrationPlane struct {
	cfg    Config
	fanout *bridge.Fanout

	mu sync.Mutex
	// sessions by id — populated on StartSession, removed on EndSession.
	sessions map[string]*core.Session
	// panes -> active session id so we can refuse double-dispatch on
	// a pane that's already running an assignment.
	paneActive map[string]string
	// nonces dedupes StartSession retries by assignment id. Value is
	// the session id we returned the first time so replays echo it.
	nonces map[string]string
	// escalations is the in-memory index populated by the Fanout
	// observer (gm-native.13). nil when Backend is nil (zero-config
	// adaptor still has no escalation surface).
	escalations *escalationIndex
}

// New constructs the native OrchestrationPlane with zero config.
// Useful for tests that don't exercise StartSession. For real use
// call NewWithConfig.
func New() *OrchestrationPlane {
	return NewWithConfig(Config{})
}

// NewWithConfig constructs the native OrchestrationPlane with the
// given dependencies. Missing deps degrade gracefully — a nil
// Backend means StartSession returns KindUnsupported, a nil WorkPlane
// means bead-mutation correlation is off, etc.
func NewWithConfig(cfg Config) *OrchestrationPlane {
	fo := cfg.Fanout
	if fo == nil {
		fo = bridge.NewFanout()
	}
	p := &OrchestrationPlane{
		cfg:         cfg,
		fanout:      fo,
		sessions:    make(map[string]*core.Session),
		paneActive:  make(map[string]string),
		nonces:      make(map[string]string),
		escalations: newEscalationIndex(),
	}
	// Wire the observer chain: escalation index first, then the
	// session-state handler. Fanout supports a single observer; we
	// fan out here so each handler is independent and testable.
	fo.SetObserver(func(ev core.OrchestrationEvent) {
		p.escalations.handleEvent(ev)
		p.handleStateEvent(ev)
	})
	return p
}

// Fanout exposes the bridge fanout so the server can register new
// session tailers when it wires in the SSE hub.
func (o *OrchestrationPlane) Fanout() *bridge.Fanout { return o.fanout }

var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)

// unsupported is the canonical stub-method error. Using KindUnsupported
// keeps the server's typed-error routing path clean (client sees
// {"code":"unsupported","retryable":false}).
func unsupported(method string) error {
	return core.NewAdaptorError(core.KindUnsupported,
		"native: %s not implemented yet", method)
}

func (*OrchestrationPlane) Describe() core.OrchestrationCapabilityManifest {
	return Manifest
}

func (*OrchestrationPlane) DeclaredState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{CapturedAt: time.Now()}, nil
}

func (*OrchestrationPlane) ObservedState(context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{CapturedAt: time.Now()}, nil
}

func (*OrchestrationPlane) ListAgents(context.Context, core.AgentFilter) ([]core.AgentRef, error) {
	// Contract: empty slice (not nil) is valid for "no agents visible".
	// The real impl (gm-native.4) replaces this with a backend poll.
	return []core.AgentRef{}, nil
}

func (*OrchestrationPlane) ReadAgent(_ context.Context, _ core.AgentID) (*core.AgentRef, error) {
	return nil, unsupported("ReadAgent")
}

func (*OrchestrationPlane) ListGroups(context.Context) ([]core.AgentGroup, error) {
	return []core.AgentGroup{}, nil
}

func (*OrchestrationPlane) ResolveGroupMembers(_ context.Context, _ string) ([]core.AgentRef, error) {
	return nil, unsupported("ResolveGroupMembers")
}

func (*OrchestrationPlane) ClaimNextReady(context.Context, core.ReadyFilter, core.AgentRef) (*core.Reservation, error) {
	// Native is push-only (see Manifest). Pull-style claim is never
	// supported — surface that as a non-retryable unsupported instead
	// of a transient error.
	return nil, unsupported("ClaimNextReady")
}

func (*OrchestrationPlane) ReleaseReservation(context.Context, string) error {
	return unsupported("ReleaseReservation")
}

// StartSession is implemented in start.go so the full lifecycle
// (worktree provisioning, bridge install, backend spawn, state
// recording, event emission) has room to breathe without bloating
// this dispatch file. gm-native.9.

func (*OrchestrationPlane) PauseSession(context.Context, string, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("PauseSession")
}

func (*OrchestrationPlane) ResumeSession(context.Context, string, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("ResumeSession")
}

// EndSession lives in end.go (gm-native.12).

func (*OrchestrationPlane) PeekSession(context.Context, string) (core.SessionPeek, error) {
	return core.SessionPeek{}, unsupported("PeekSession")
}

// ListSessions returns a snapshot of every live session, filtered by f.
// Used by the SPA's /sessions inventory page (gm-native.15). Returns a
// fresh slice + copies of the Session values so callers can't mutate
// adaptor state.
func (o *OrchestrationPlane) ListSessions(_ context.Context, f core.SessionFilter) ([]core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]core.Session, 0, len(o.sessions))
	for _, s := range o.sessions {
		if !f.IncludeTerminal && isTerminalStatus(s.Status) {
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

// ListPendingRequests / ListOpenEscalations / ResolveEscalation are
// implemented in escalations.go (gm-native.13).

func (*OrchestrationPlane) AcquireWorkspace(context.Context, core.WorkspaceRequest) (core.Workspace, error) {
	return core.Workspace{}, unsupported("AcquireWorkspace")
}

func (*OrchestrationPlane) ReleaseWorkspace(context.Context, string) error {
	return unsupported("ReleaseWorkspace")
}

func (*OrchestrationPlane) InspectWorkspace(context.Context, string) (core.Workspace, error) {
	return core.Workspace{}, unsupported("InspectWorkspace")
}

func (o *OrchestrationPlane) Subscribe(ctx context.Context, _ core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	// Delegate to the Fanout's broadcast subscribe; caller cancels
	// their ctx to unregister.
	return o.fanout.Subscribe(ctx), nil
}
