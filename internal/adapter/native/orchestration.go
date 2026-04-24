package native

import (
	"context"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// OrchestrationPlane is the native adaptor. gm-native.2 ships the
// scaffold — every non-Describe method returns core.KindUnsupported
// so the server can mount the adaptor without exercising any of the
// unimplemented surfaces. Subsequent beads (gm-native.9 StartSession,
// gm-native.12 EndSession, gm-native.13 escalations, …) fill in real
// behavior method by method.
type OrchestrationPlane struct {
	// Placeholder for dependencies that land in later beads (backend,
	// agent-type registry, bridge tailer). Keeping the struct exported
	// but minimal lets tests construct it via New() without having to
	// thread config through yet.
}

// New constructs the native OrchestrationPlane. Takes no arguments
// today; backend + agent-registry wiring (gm-native.4/.6) will extend
// this signature with explicit deps (prefer option funcs over a big
// config struct when they land).
func New() *OrchestrationPlane {
	return &OrchestrationPlane{}
}

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

func (*OrchestrationPlane) StartSession(context.Context, string, core.SessionPrompt) (core.Session, error) {
	return core.Session{}, unsupported("StartSession")
}

func (*OrchestrationPlane) PauseSession(context.Context, string, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("PauseSession")
}

func (*OrchestrationPlane) ResumeSession(context.Context, string, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("ResumeSession")
}

func (*OrchestrationPlane) EndSession(context.Context, string, core.SessionEndMode, core.ConfirmNonce) (core.Session, error) {
	return core.Session{}, unsupported("EndSession")
}

func (*OrchestrationPlane) PeekSession(context.Context, string) (core.SessionPeek, error) {
	return core.SessionPeek{}, unsupported("PeekSession")
}

func (*OrchestrationPlane) ListPendingRequests(context.Context, string) ([]core.EscalationRequest, error) {
	return nil, unsupported("ListPendingRequests")
}

func (*OrchestrationPlane) AcquireWorkspace(context.Context, core.WorkspaceRequest) (core.Workspace, error) {
	return core.Workspace{}, unsupported("AcquireWorkspace")
}

func (*OrchestrationPlane) ReleaseWorkspace(context.Context, string) error {
	return unsupported("ReleaseWorkspace")
}

func (*OrchestrationPlane) InspectWorkspace(context.Context, string) (core.Workspace, error) {
	return core.Workspace{}, unsupported("InspectWorkspace")
}

func (*OrchestrationPlane) ListOpenEscalations(context.Context, core.EscalationFilter) ([]core.EscalationRequest, error) {
	return []core.EscalationRequest{}, nil
}

func (*OrchestrationPlane) ResolveEscalation(context.Context, string, core.EscalationResolution, core.ConfirmNonce) (core.EscalationRequest, error) {
	return core.EscalationRequest{}, unsupported("ResolveEscalation")
}

func (*OrchestrationPlane) Subscribe(ctx context.Context, _ core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	// Scaffold: a closed-on-ctx-done channel. gm-native.8 replaces this
	// with the bridge-log tail fan-out.
	ch := make(chan core.OrchestrationEvent)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}
