package native

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
)

// peekBackend is a fakeBackend with a programmable CapturePane so peek
// tests can drive both happy and error paths deterministically. The
// embedded fakeBackend covers Spawn / Send / Kill so StartSession (the
// natural way to register a session) keeps working.
type peekBackend struct {
	*fakeBackend
	captureFn func(ctx context.Context, paneID string, lines int) (string, error)
}

func newPeekBackend(fn func(context.Context, string, int) (string, error)) *peekBackend {
	return &peekBackend{fakeBackend: newFakeBackend(), captureFn: fn}
}

func (p *peekBackend) CapturePane(ctx context.Context, paneID string, lines int) (string, error) {
	if p.captureFn != nil {
		return p.captureFn(ctx, paneID, lines)
	}
	return "", nil
}

// startedSessionFor is a small helper that drives a real StartSession
// against the given backend so the resulting Session record carries
// the same provider metadata production flows produce. Returns the
// freshly-started session id.
func startedSessionFor(t *testing.T, p *OrchestrationPlane) string {
	t.Helper()
	sess, err := p.StartSession(context.Background(), "assign-peek",
		freshPrompt("gm-peek", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return sess.ID
}

func newPeekPlane(t *testing.T, b *peekBackend) *OrchestrationPlane {
	t.Helper()
	return NewWithConfig(Config{
		Backend:  b,
		Registry: defaultRegistry(),
		RepoRoot: initRepo(t),
	})
}

// gm-e7.3 happy path: PeekSession on a live session returns the
// transcript the backend produced, plus the session's current status.
func TestPeekSession_ReturnsTranscript(t *testing.T) {
	const want = "line 1\nline 2\nclaude > "
	var gotPaneID string
	var gotLines int
	be := newPeekBackend(func(_ context.Context, paneID string, lines int) (string, error) {
		gotPaneID = paneID
		gotLines = lines
		return want, nil
	})
	p := newPeekPlane(t, be)
	id := startedSessionFor(t, p)

	peek, err := p.PeekSession(context.Background(), id)
	if err != nil {
		t.Fatalf("PeekSession: %v", err)
	}
	if peek.SessionID != id {
		t.Errorf("SessionID: want %q got %q", id, peek.SessionID)
	}
	if peek.Transcript != want {
		t.Errorf("Transcript: got %q", peek.Transcript)
	}
	if peek.CapturedAt.IsZero() {
		t.Error("CapturedAt must be populated")
	}
	if !strings.HasPrefix(gotPaneID, "%") {
		t.Errorf("backend got paneID=%q; expected fakeBackend %%-prefixed id", gotPaneID)
	}
	if gotLines != peekDefaultLines {
		t.Errorf("backend got lines=%d; want %d", gotLines, peekDefaultLines)
	}
}

// gm-e7.3 contract: an unknown sessionID surfaces as KindSessionNotFound
// — the SPA's peek surface differentiates that from KindAdaptorDegraded
// when deciding whether to retry vs prompt the operator.
func TestPeekSession_UnknownIDIsSessionNotFound(t *testing.T) {
	be := newPeekBackend(nil)
	p := newPeekPlane(t, be)

	_, err := p.PeekSession(context.Background(), "no-such-session")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) {
		t.Fatalf("want AdaptorError, got %T: %v", err, err)
	}
	if aerr.Kind != core.KindSessionNotFound {
		t.Errorf("Kind: want %q got %q", core.KindSessionNotFound, aerr.Kind)
	}
}

// gm-e7.3 contract: PeekSession on a zero-config adaptor (no Backend)
// continues to surface KindUnsupported so the existing
// TestStubMethodsReturnUnsupported assertion holds.
func TestPeekSession_NoBackend_IsUnsupported(t *testing.T) {
	p := New()
	_, err := p.PeekSession(context.Background(), "anything")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindUnsupported {
		t.Errorf("Kind: want unsupported, got %T: %v", err, err)
	}
}

// gm-e7.3 contract: backend errors propagate as KindAdaptorDegraded so
// the SPA's banner and the /api/adaptors health surface treat the
// failure consistently with other typed adaptor errors.
func TestPeekSession_BackendError_IsAdaptorDegraded(t *testing.T) {
	be := newPeekBackend(func(context.Context, string, int) (string, error) {
		return "", errors.New("tmux: server not running")
	})
	p := newPeekPlane(t, be)
	id := startedSessionFor(t, p)

	_, err := p.PeekSession(context.Background(), id)
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) {
		t.Fatalf("want AdaptorError, got %T: %v", err, err)
	}
	if aerr.Kind != core.KindAdaptorDegraded {
		t.Errorf("Kind: want %q got %q", core.KindAdaptorDegraded, aerr.Kind)
	}
}

// gm-e7.3 DoD: peek returns within 200ms for a typical session. We
// pin the property by injecting a 50ms backend (well below the
// budget) and asserting the round trip stays under 200ms — the
// margin guards against future regressions in the adaptor's own
// path (lock contention, copy churn) without depending on real
// tmux timing.
func TestPeekSession_CompletesUnder200msBudget(t *testing.T) {
	be := newPeekBackend(func(_ context.Context, _ string, _ int) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "ok", nil
	})
	p := newPeekPlane(t, be)
	id := startedSessionFor(t, p)

	start := time.Now()
	_, err := p.PeekSession(context.Background(), id)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("PeekSession: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("PeekSession took %v; DoD budget is 200ms", elapsed)
	}
}

// gm-e7.3: the backend gets the actual pane id we recorded in the
// session's provider metadata, so a multi-session plane peeks the
// correct pane and never crosses streams.
func TestPeekSession_RoutesToCorrectPane(t *testing.T) {
	captured := make(map[string]string) // paneID -> response
	be := newPeekBackend(func(_ context.Context, paneID string, _ int) (string, error) {
		return captured[paneID], nil
	})
	p := newPeekPlane(t, be)

	id1 := startedSessionFor(t, p)
	pane1 := paneIDOf(t, p, id1)
	captured[pane1] = "from-pane-1"

	// Second session needs a different assignment id (StartSession
	// dedups by nonce when the same id replays).
	sess2, err := p.StartSession(context.Background(), "assign-peek-2",
		freshPrompt("gm-peek-2", nil))
	if err != nil {
		t.Fatalf("StartSession 2: %v", err)
	}
	id2 := sess2.ID
	pane2 := paneIDOf(t, p, id2)
	captured[pane2] = "from-pane-2"

	peek1, err := p.PeekSession(context.Background(), id1)
	if err != nil {
		t.Fatalf("PeekSession 1: %v", err)
	}
	peek2, err := p.PeekSession(context.Background(), id2)
	if err != nil {
		t.Fatalf("PeekSession 2: %v", err)
	}
	if peek1.Transcript != "from-pane-1" {
		t.Errorf("session 1 got: %q", peek1.Transcript)
	}
	if peek2.Transcript != "from-pane-2" {
		t.Errorf("session 2 got: %q", peek2.Transcript)
	}
}

func paneIDOf(t *testing.T, p *OrchestrationPlane, sessionID string) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	sess, ok := p.sessions[sessionID]
	if !ok {
		t.Fatalf("no session %q on plane", sessionID)
	}
	pid, _ := sess.ProviderMetadata["pane_id"].(string)
	if pid == "" {
		t.Fatalf("session %q missing pane_id", sessionID)
	}
	return pid
}

// Compile-time check: peekBackend really does satisfy backend.Backend.
var _ backend.Backend = (*peekBackend)(nil)
