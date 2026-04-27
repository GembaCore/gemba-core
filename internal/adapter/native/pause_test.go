package native

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
	"github.com/MikeBengtson/gemba/core"
)

// pausableBackend wraps fakeBackend with Pause/Unpause so tests can
// exercise the gm-root.15.12 code path without a real docker daemon.
type pausableBackend struct {
	*fakeBackend
	mu       sync.Mutex
	paused   map[string]bool
	pauseErr error
}

func newPausable() *pausableBackend {
	return &pausableBackend{
		fakeBackend: newFakeBackend(),
		paused:      map[string]bool{},
	}
}

func (p *pausableBackend) Pause(_ context.Context, id string) error {
	if p.pauseErr != nil {
		return p.pauseErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused[id] = true
	return nil
}

func (p *pausableBackend) Unpause(_ context.Context, id string) error {
	if p.pauseErr != nil {
		return p.pauseErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.paused, id)
	return nil
}

// Compile-time check the fixture satisfies the interface.
var _ backend.Pausable = (*pausableBackend)(nil)

func insertSession(o *OrchestrationPlane, id, paneID string, status core.SessionStatus) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessions[id] = &core.Session{
		ID:     id,
		Status: status,
		ProviderMetadata: map[string]any{
			"pane_id": paneID,
		},
	}
}

func TestPauseSession_UnsupportedOnTmux(t *testing.T) {
	o := NewWithConfig(Config{Backend: newFakeBackend()})
	insertSession(o, "s1", "%1", core.SessionReady)
	_, err := o.PauseSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err == nil {
		t.Fatal("want unsupported error")
	}
	ae, ok := err.(*core.AdaptorError)
	if !ok || ae.Kind != core.KindUnsupported {
		t.Errorf("want KindUnsupported, got %v", err)
	}
}

func TestPauseSession_NilBackend(t *testing.T) {
	o := NewWithConfig(Config{})
	_, err := o.PauseSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err == nil {
		t.Fatal("want unsupported error")
	}
}

func TestPauseResume_HappyPath(t *testing.T) {
	pb := newPausable()
	o := NewWithConfig(Config{Backend: pb})
	insertSession(o, "s1", "cid-1", core.SessionWorking)

	sess, err := o.PauseSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != core.SessionSuspended {
		t.Errorf("status after pause: %q", sess.Status)
	}
	if !pb.paused["cid-1"] {
		t.Error("backend Pause not called")
	}

	resumed, err := o.ResumeSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != core.SessionReady {
		t.Errorf("status after resume: %q", resumed.Status)
	}
	if pb.paused["cid-1"] {
		t.Error("backend Unpause not called")
	}
}

func TestPauseSession_IdempotentOnAlreadyPaused(t *testing.T) {
	pb := newPausable()
	o := NewWithConfig(Config{Backend: pb})
	insertSession(o, "s1", "cid-1", core.SessionSuspended)
	// First call must be a no-op — the backend Pause should NOT fire.
	_, err := o.PauseSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err != nil {
		t.Fatal(err)
	}
	if pb.paused["cid-1"] {
		t.Error("backend Pause fired on already-suspended session")
	}
}

func TestResumeSession_NoOpOnRunning(t *testing.T) {
	pb := newPausable()
	o := NewWithConfig(Config{Backend: pb})
	insertSession(o, "s1", "cid-1", core.SessionWorking)
	sess, err := o.ResumeSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != core.SessionWorking {
		t.Errorf("status should be unchanged, got %q", sess.Status)
	}
}

func TestPauseSession_UnknownID(t *testing.T) {
	pb := newPausable()
	o := NewWithConfig(Config{Backend: pb})
	_, err := o.PauseSession(context.Background(), "missing", core.ConfirmNonce("n"))
	if err == nil {
		t.Fatal("want session-not-found error")
	}
	ae, ok := err.(*core.AdaptorError)
	if !ok || ae.Kind != core.KindSessionNotFound {
		t.Errorf("want KindSessionNotFound, got %v", err)
	}
}

func TestPauseSession_BackendError(t *testing.T) {
	pb := newPausable()
	pb.pauseErr = fmt.Errorf("simulated daemon failure")
	o := NewWithConfig(Config{Backend: pb})
	insertSession(o, "s1", "cid-1", core.SessionReady)
	_, err := o.PauseSession(context.Background(), "s1", core.ConfirmNonce("n"))
	if err == nil {
		t.Fatal("want wrapped error")
	}
	ae, ok := err.(*core.AdaptorError)
	if !ok || ae.Kind != core.KindProcessFailed {
		t.Errorf("want KindProcessFailed, got %v", err)
	}
}
