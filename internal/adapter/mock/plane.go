// Package mock implements a deterministic, in-process orchestration
// plane that conforms to core.OrchestrationPlaneAdaptor without
// spawning real claude sessions. It is the dispatch backend for the
// gm-root.27 acceptance test and is also operator-usable as a
// "dry-run" mode (gm-root.28, decision D21 / gm-x4i9).
//
// Per-bead work is performed by an in-Go template library — see
// templates.go. The mock plane mints Session objects, transitions
// them through the full Spawning → Working → SessionReady lifecycle,
// and re-uses sessions via RecycleSession (warm pool path; gm-s47n.11
// parity).
//
// Operator caveat: mock-mode does NOT run real LLM sessions or
// produce production code. It is documented as a peer adaptor
// (autonomous-dispatch.md §"Mock mode") so an operator picking up
// `gemba serve --orchestration=mock` knows what they're getting.

package mock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// OrchestrationPlane is the in-process mock plane.
type OrchestrationPlane struct {
	mu sync.RWMutex

	// sessions holds the live Session map keyed by session id.
	sessions map[string]core.Session

	// nextSessionN gives every minted session a monotonically
	// increasing suffix so log lines stay legible.
	nextSessionN int

	// projectDir is the project root the runner writes against.
	// Each StartSession resolves files relative to this.
	projectDir string

	// transport advertises core.TransportAPI by default. Carried
	// for parity with native — see Describe().
	transport core.Transport
}

// Config carries plane construction options.
type Config struct {
	// ProjectDir is the absolute path to the bd workspace + target
	// project root. Required.
	ProjectDir string

	// Transport advertised in the capability manifest. Defaults to
	// core.TransportAPI.
	Transport core.Transport
}

// NewOrchestrationPlane returns a configured mock plane. The plane
// does not start any background loops on construction — sessions are
// minted lazily via StartSession.
func NewOrchestrationPlane(cfg Config) *OrchestrationPlane {
	transport := cfg.Transport
	if transport == "" {
		transport = core.TransportAPI
	}
	return &OrchestrationPlane{
		sessions:   make(map[string]core.Session),
		projectDir: cfg.ProjectDir,
		transport:  transport,
	}
}

// Describe returns the capability manifest the SPA + daemon consume
// when deciding what features to expose.
func (o *OrchestrationPlane) Describe() core.OrchestrationCapabilityManifest {
	return core.OrchestrationCapabilityManifest{
		AdaptorID:               "mock",
		AdaptorVersion:          "0.1.0",
		OrchestrationAPIVersion: "1.0.0",
		Transport:               o.transport,
	}
}

// Close releases plane-level resources. Idempotent.
func (o *OrchestrationPlane) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessions = make(map[string]core.Session)
}

// ─── Stubs (filled in by sibling beads) ───────────────────────────

// StartSession lifecycle is implemented in start.go (gm-root.28.2).
func (o *OrchestrationPlane) StartSession(_ context.Context, _ string, _ core.SessionPrompt) (core.Session, error) {
	return core.Session{}, unsupported("StartSession (gm-root.28.2 — pending)")
}

// EndSession is implemented in end.go (gm-root.28.2).
func (o *OrchestrationPlane) EndSession(_ context.Context, _ string, _ core.SessionEndMode, _ core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("EndSession (gm-root.28.2 — pending)")
}

// RecycleSession is implemented in recycle.go (gm-root.28.2).
func (o *OrchestrationPlane) RecycleSession(_ context.Context, _ string) (core.Session, error) {
	return core.Session{}, unsupported("RecycleSession (gm-root.28.2 — pending)")
}

// PauseSession / ResumeSession are not exercised by the autodispatch
// daemon's mock path; surface them as unsupported.
func (o *OrchestrationPlane) PauseSession(_ context.Context, _ string, _ core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("PauseSession")
}

func (o *OrchestrationPlane) ResumeSession(_ context.Context, _ string, _ core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("ResumeSession")
}

func (o *OrchestrationPlane) PeekSession(_ context.Context, _ string) (core.SessionPeek, error) {
	return core.SessionPeek{}, unsupported("PeekSession")
}

// ListSessions returns the in-memory session set, optionally filtered.
func (o *OrchestrationPlane) ListSessions(_ context.Context, f core.SessionFilter) ([]core.Session, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]core.Session, 0, len(o.sessions))
	for _, s := range o.sessions {
		if !sessionMatches(s, f) {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// ─── Reservation / agent stubs ────────────────────────────────────

func (o *OrchestrationPlane) ListAgents(_ context.Context, _ core.AgentFilter) ([]core.AgentRef, error) {
	return []core.AgentRef{}, nil
}

func (o *OrchestrationPlane) ReadAgent(_ context.Context, _ core.AgentID) (*core.AgentRef, error) {
	return nil, unsupported("ReadAgent")
}

func (o *OrchestrationPlane) ListGroups(_ context.Context) ([]core.AgentGroup, error) {
	return []core.AgentGroup{}, nil
}

func (o *OrchestrationPlane) ResolveGroupMembers(_ context.Context, _ string) ([]core.AgentRef, error) {
	return []core.AgentRef{}, nil
}

func (o *OrchestrationPlane) ClaimNextReady(_ context.Context, _ core.ReadyFilter, _ core.AgentRef) (*core.Reservation, error) {
	return nil, unsupported("ClaimNextReady")
}

func (o *OrchestrationPlane) ReleaseReservation(_ context.Context, _ string) error {
	return unsupported("ReleaseReservation")
}

// ─── Workspace stubs ──────────────────────────────────────────────

func (o *OrchestrationPlane) AcquireWorkspace(_ context.Context, _ core.WorkspaceRequest) (core.Workspace, error) {
	return core.Workspace{}, unsupported("AcquireWorkspace")
}

func (o *OrchestrationPlane) ReleaseWorkspace(_ context.Context, _ string) error {
	return unsupported("ReleaseWorkspace")
}

func (o *OrchestrationPlane) InspectWorkspace(_ context.Context, _ string) (core.Workspace, error) {
	return core.Workspace{}, unsupported("InspectWorkspace")
}

// ─── State stubs ──────────────────────────────────────────────────

func (o *OrchestrationPlane) DeclaredState(_ context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{}, nil
}

func (o *OrchestrationPlane) ObservedState(_ context.Context) (core.WorkspaceTopology, error) {
	return core.WorkspaceTopology{}, nil
}

// ─── Escalation stubs (the test injects synthetic via gm-root.27.22 path) ──

func (o *OrchestrationPlane) ListPendingRequests(_ context.Context, _ string) ([]core.EscalationRequest, error) {
	return []core.EscalationRequest{}, nil
}

func (o *OrchestrationPlane) ListOpenEscalations(_ context.Context, _ core.EscalationFilter) ([]core.EscalationRequest, error) {
	return []core.EscalationRequest{}, nil
}

func (o *OrchestrationPlane) ResolveEscalation(_ context.Context, _ string, _ core.EscalationResolution, _ core.ConfirmNonce) (core.EscalationRequest, error) {
	return core.EscalationRequest{}, unsupported("ResolveEscalation")
}

// ─── Subscribe stub ───────────────────────────────────────────────

func (o *OrchestrationPlane) Subscribe(_ context.Context, _ core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	// Closed channel — mock plane doesn't emit raw orchestration
	// events. The bridge that adapts native's terminal events into
	// core.OrchestrationEvent has no peer here; the daemon instead
	// observes lifecycle by polling ListSessions.
	ch := make(chan core.OrchestrationEvent)
	close(ch)
	return ch, nil
}

// ─── Helpers ──────────────────────────────────────────────────────

func unsupported(method string) error {
	return core.NewAdaptorError(core.KindUnsupported,
		"mock: %s not supported", method)
}

func sessionMatches(s core.Session, f core.SessionFilter) bool {
	if f.AgentID != "" && s.AgentID != f.AgentID {
		return false
	}
	if len(f.Status) > 0 {
		ok := false
		for _, st := range f.Status {
			if s.Status == st {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if !f.IncludeTerminal && (s.Status == core.SessionCompleted || s.Status == core.SessionFailed) {
		return false
	}
	return true
}

// Compile-time assertion that *OrchestrationPlane implements the
// adaptor interface. If a new method is added to the interface,
// the build breaks here until the stub lands.
var _ core.OrchestrationPlaneAdaptor = (*OrchestrationPlane)(nil)

// time import retained for the lifecycle file's sake (start.go
// will use it once gm-root.28.2 lands). Keeps the import block
// stable across file additions.
var _ = time.Now
var _ = fmt.Errorf
