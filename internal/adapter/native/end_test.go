package native

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func startForEnd(t *testing.T, fb *fakeBackend) (*OrchestrationPlane, core.Session) {
	t.Helper()
	p := NewWithConfig(Config{
		Backend:  fb,
		Registry: defaultRegistry(),
		RepoRoot: initRepo(t),
	})
	sess, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return p, sess
}

func TestEndSessionHappyPath(t *testing.T) {
	fb := newFakeBackend()
	p, sess := startForEnd(t, fb)

	ended, err := p.EndSession(context.Background(), sess.ID, core.SessionEndCompleted, core.ConfirmNonce("n1"))
	if err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if ended.Status != core.SessionCompleted {
		t.Errorf("status: got %q", ended.Status)
	}
	if ended.EndedAt == nil {
		t.Error("EndedAt should be populated")
	}
	if ended.CloseReason == nil {
		t.Error("CloseReason must be set on terminal session")
	}
	if len(fb.killCalls) == 0 {
		t.Error("backend.Kill should have been called (no pane-exit signal in fake)")
	}
}

func TestEndSessionUnknownReturnsNotFound(t *testing.T) {
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry(), RepoRoot: initRepo(t)})
	_, err := p.EndSession(context.Background(), "no-such", core.SessionEndCompleted, "")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindSessionNotFound {
		t.Errorf("want KindSessionNotFound, got %T: %v", err, err)
	}
}

func TestEndSessionTerminalAbsorbs(t *testing.T) {
	fb := newFakeBackend()
	p, sess := startForEnd(t, fb)
	if _, err := p.EndSession(context.Background(), sess.ID, core.SessionEndCompleted, ""); err != nil {
		t.Fatal(err)
	}
	// Second end on a terminal session returns the terminal session
	// without a second Kill call.
	initialKills := len(fb.killCalls)
	ended, err := p.EndSession(context.Background(), sess.ID, core.SessionEndCompleted, "")
	if err != nil {
		t.Fatalf("second end: %v", err)
	}
	if ended.Status != core.SessionCompleted {
		t.Errorf("status: got %q", ended.Status)
	}
	if len(fb.killCalls) != initialKills {
		t.Error("second EndSession should not re-kill the pane")
	}
}

func TestEndSessionNonceReplay(t *testing.T) {
	fb := newFakeBackend()
	p, sess := startForEnd(t, fb)
	nonce := core.ConfirmNonce("end-n1")
	first, err := p.EndSession(context.Background(), sess.ID, core.SessionEndCompleted, nonce)
	if err != nil {
		t.Fatal(err)
	}
	initialKills := len(fb.killCalls)
	second, err := p.EndSession(context.Background(), sess.ID, core.SessionEndCompleted, nonce)
	if err != nil {
		t.Fatalf("nonce replay: %v", err)
	}
	if first.Status != second.Status {
		t.Errorf("replay status mismatch: %q vs %q", first.Status, second.Status)
	}
	if len(fb.killCalls) != initialKills {
		t.Error("same-nonce replay must not re-kill")
	}
}

func TestEndSessionFreesBusyPaneForReuse(t *testing.T) {
	fb := newFakeBackend()
	p, sess := startForEnd(t, fb)
	if _, err := p.EndSession(context.Background(), sess.ID, core.SessionEndCompleted, ""); err != nil {
		t.Fatal(err)
	}
	// With the old session ended, starting a new one must succeed —
	// the pane occupancy map should be cleared.
	_, err := p.StartSession(context.Background(), "a2", freshPrompt("gm-bar", nil))
	if err != nil {
		t.Fatalf("post-end StartSession should succeed: %v", err)
	}
}

func TestEndSessionZeroConfigUnsupported(t *testing.T) {
	p := New()
	_, err := p.EndSession(context.Background(), "s", core.SessionEndCompleted, "")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindUnsupported {
		t.Errorf("want KindUnsupported, got %T: %v", err, err)
	}
}
