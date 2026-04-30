package native

import (
	"context"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
	"github.com/GembaCore/gemba-core/internal/adapter/native/bridge"
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
	// paneSessions tracks every session id currently sharing a pane.
	// A pane with one entry is the historical single-bead case; panes
	// with multiple entries are the intra-parallelism case (gm-root.16):
	// an `intra_parallel=true` agent type can carry up to its
	// MaxParallel beads in one pane, with each bead represented as its
	// own logical Session record sharing pane_id. Cap enforcement and
	// session_parallel_changed event emission key off len(slice).
	paneSessions map[string][]string
	// nonces dedupes StartSession retries by assignment id. Value is
	// the session id we returned the first time so replays echo it.
	nonces map[string]string
	// escalations is the in-memory index populated by the Fanout
	// observer (gm-native.13). nil when Backend is nil (zero-config
	// adaptor still has no escalation surface).
	escalations *escalationIndex
	// stopReaper / stopReconcile are returned by the goroutine
	// launchers so Close() can shut them down cleanly. nil when the
	// adaptor was constructed with reaper/reconcile disabled (tests).
	stopReaper    func()
	stopReconcile func()
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
		cfg:          cfg,
		fanout:       fo,
		sessions:     make(map[string]*core.Session),
		paneSessions: make(map[string][]string),
		nonces:       make(map[string]string),
		escalations:  newEscalationIndex(),
	}
	// Wire the observer chain: escalation index first, then the
	// session-state handler. Fanout supports a single observer; we
	// fan out here so each handler is independent and testable.
	fo.SetObserver(func(ev core.OrchestrationEvent) {
		p.escalations.handleEvent(ev)
		p.handleStateEvent(ev)
	})
	// Pool-lifecycle background loops (gm-s47n.11). Both are opt-in
	// via Config — zero-config adaptors (no Backend) skip them so
	// today's unit tests stay deterministic.
	if cfg.Backend != nil && !cfg.DisableReaper {
		p.stopReaper = p.startIdleReaper(cfg.Reaper)
	}
	if cfg.Backend != nil && cfg.WorkPlane != nil && !cfg.DisableReconcile {
		p.stopReconcile = p.startReconcileLoop(cfg.Reconcile)
	}
	return p
}

// Close stops the background goroutines (reaper, reconcile loop)
// the constructor launched. Call from the server's shutdown path.
// Safe to call multiple times; safe on a zero-config adaptor.
func (o *OrchestrationPlane) Close() {
	if o.stopReaper != nil {
		o.stopReaper()
	}
	if o.stopReconcile != nil {
		o.stopReconcile()
	}
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

func (o *OrchestrationPlane) ListAgents(context.Context, core.AgentFilter) ([]core.AgentRef, error) {
	// Surface the agents.toml roster as AgentRefs so the SPA's agent-
	// type picker (NewSessionDialog, drag-to-In-Progress auto-dispatch)
	// has something to render. One AgentRef per registered type; the
	// adaptor doesn't track live human/agent membership yet, so Kind is
	// always AgentKindAgent and Dialect mirrors the type name — the SPA
	// uses Dialect as the token it posts back in StartSession.
	types := o.cfg.Registry.Agents
	out := make([]core.AgentRef, 0, len(types))
	for _, a := range types {
		dialect := a.Name
		out = append(out, core.AgentRef{
			ID:      core.AgentID(a.Name),
			Name:    a.Name,
			Kind:    core.AgentKindAgent,
			Dialect: &dialect,
		})
	}
	return out, nil
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

// PauseSession freezes a running session's processes without closing
// the session. Only supported when the backend satisfies
// backend.Pausable — today that's the Docker backend (gm-root.15.12).
// Tmux / iTerm / Terminal.app return KindUnsupported.
//
// Successful pause transitions the Session.Status to SessionSuspended.
// Idempotent: pausing an already-suspended session is a no-op (and
// returns the current session record rather than erroring).
func (o *OrchestrationPlane) PauseSession(ctx context.Context, sessionID string, _ core.ConfirmNonce) (core.Session, error) {
	if o.cfg.Backend == nil {
		return core.Session{}, unsupported("PauseSession")
	}
	pausable, ok := o.cfg.Backend.(backend.Pausable)
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindUnsupported,
			"native: PauseSession not supported by backend %q", o.cfg.Backend.Name())
	}
	if sessionID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: PauseSession requires session id")
	}

	paneID, sess, err := o.lookupSessionPane(sessionID)
	if err != nil {
		return core.Session{}, err
	}
	if sess.Status == core.SessionSuspended {
		return *sess, nil
	}
	if err := pausable.Pause(ctx, paneID); err != nil {
		return core.Session{}, core.WrapAdaptorError(core.KindProcessFailed, err,
			"native: pause %s", sessionID)
	}
	return o.updateSessionStatus(sessionID, core.SessionSuspended), nil
}

// ResumeSession is the PauseSession counterpart. The container's
// memory state is identical to pre-pause; the agent continues from
// exactly where it was. Status returns to SessionReady — the
// correlator will re-emit SessionWorking on the next observable
// activity if the agent is mid-task.
func (o *OrchestrationPlane) ResumeSession(ctx context.Context, sessionID string, _ core.ConfirmNonce) (core.Session, error) {
	if o.cfg.Backend == nil {
		return core.Session{}, unsupported("ResumeSession")
	}
	pausable, ok := o.cfg.Backend.(backend.Pausable)
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindUnsupported,
			"native: ResumeSession not supported by backend %q", o.cfg.Backend.Name())
	}
	if sessionID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"native: ResumeSession requires session id")
	}

	paneID, sess, err := o.lookupSessionPane(sessionID)
	if err != nil {
		return core.Session{}, err
	}
	if sess.Status != core.SessionSuspended {
		// Not paused — treat as no-op rather than erroring, matching
		// the Docker daemon's behavior where `docker unpause` on a
		// running container returns an error we'd rather smooth over.
		return *sess, nil
	}
	if err := pausable.Unpause(ctx, paneID); err != nil {
		return core.Session{}, core.WrapAdaptorError(core.KindProcessFailed, err,
			"native: resume %s", sessionID)
	}
	return o.updateSessionStatus(sessionID, core.SessionReady), nil
}

// lookupSessionPane returns the pane id + session record for the
// given session id. Error uses KindNotFound so the HTTP surface maps
// it to 404 cleanly.
func (o *OrchestrationPlane) lookupSessionPane(sessionID string) (string, *core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		return "", nil, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q not found", sessionID)
	}
	paneID, _ := sess.ProviderMetadata["pane_id"].(string)
	if paneID == "" {
		return "", nil, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q has no pane id", sessionID)
	}
	cp := *sess
	return paneID, &cp, nil
}

// updateSessionStatus atomically mutates a live session's status and
// returns a copy. Callers get the new state without holding the lock.
func (o *OrchestrationPlane) updateSessionStatus(sessionID string, status core.SessionStatus) core.Session {
	o.mu.Lock()
	defer o.mu.Unlock()
	sess := o.sessions[sessionID]
	if sess == nil {
		return core.Session{}
	}
	sess.Status = status
	return *sess
}

// EndSession lives in end.go (gm-native.12).

// peekDefaultLines is how many lines the adaptor asks the backend to
// capture when a caller invokes PeekSession. Big enough to surface a
// useful slice of the agent's recent output (a few screenfuls) but
// small enough that a typical scrape stays well under the bead's
// 200ms DoD even on a slow filesystem.
const peekDefaultLines = 200

// PeekSession returns the last peekDefaultLines lines from the backing
// pane plus the live Session metadata. Behaviour:
//
//   - Backend nil → KindUnsupported (zero-config adaptor).
//   - sessionID unknown to this adaptor → KindSessionNotFound.
//   - Session record is missing pane_id (shouldn't happen for a session
//     this adaptor created) → KindAdaptorDegraded with a clear message.
//   - Backend CapturePane error → wrapped as KindAdaptorDegraded so the
//     SPA banner can surface a typed envelope.
//
// gm-e7.3 / gm-native.16. Conformance Group B's PeekSession case is now
// authoritative for this adaptor.
func (o *OrchestrationPlane) PeekSession(ctx context.Context, sessionID string) (core.SessionPeek, error) {
	if o.cfg.Backend == nil {
		return core.SessionPeek{}, unsupported("PeekSession")
	}

	o.mu.Lock()
	sess, ok := o.sessions[sessionID]
	var paneID string
	var status core.SessionStatus
	if ok && sess != nil {
		status = sess.Status
		if v, vok := sess.ProviderMetadata["pane_id"]; vok {
			if s, sok := v.(string); sok {
				paneID = s
			}
		}
	}
	o.mu.Unlock()

	if !ok {
		return core.SessionPeek{}, core.NewAdaptorError(core.KindSessionNotFound,
			"native: session %q not found", sessionID)
	}
	if paneID == "" {
		return core.SessionPeek{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"native: session %q is missing pane_id in provider metadata", sessionID)
	}

	transcript, err := o.cfg.Backend.CapturePane(ctx, paneID, peekDefaultLines)
	if err != nil {
		return core.SessionPeek{}, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"native: capture-pane on %s", paneID)
	}

	return core.SessionPeek{
		SessionID:  sessionID,
		Status:     status,
		CapturedAt: time.Now().UTC(),
		Transcript: transcript,
	}, nil
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
