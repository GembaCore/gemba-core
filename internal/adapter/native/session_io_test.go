package native

import (
	"context"
	"errors"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// startedSession is a tiny helper that drives StartSession against the
// shared fakeBackend so the resulting Session has the same
// ProviderMetadata["pane_id"] shape production flows produce. Returns
// the session id and the pane id the adapter assigned.
func startedSession(t *testing.T, p *OrchestrationPlane) (sessionID, paneID string) {
	t.Helper()
	withCleanWorktree(t, true)
	sess, err := p.StartSession(context.Background(), "assign-sio",
		freshPrompt("gm-sio", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	pid, _ := sess.ProviderMetadata["pane_id"].(string)
	if pid == "" {
		t.Fatal("started session has no pane id")
	}
	return sess.ID, pid
}

// newSIOPlane wires a fresh OrchestrationPlane with the shared
// fakeBackend so we can exercise both happy-path dispatch (Backend set)
// and the zero-config degradation path (Backend nil).
func newSIOPlane(t *testing.T, fb *fakeBackend) *OrchestrationPlane {
	t.Helper()
	return NewWithConfig(Config{
		Backend:          fb,
		Registry:         defaultRegistry(),
		RepoRoot:         initRepo(t),
		DisableReaper:    true,
		DisableReconcile: true,
	})
}

// ---- SendInput ----------------------------------------------------

func TestSendInput_BackendNil_Unsupported(t *testing.T) {
	p := NewWithConfig(Config{Registry: defaultRegistry()})
	err := p.SendInput(context.Background(), "sess-1",
		core.SessionInput{Mode: core.InputLiteral, Keys: "x"})
	if err == nil {
		t.Fatal("want error when Backend nil")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindUnsupported {
		t.Fatalf("want KindUnsupported, got %v (%T)", err, err)
	}
}

func TestSendInput_UnknownMode_Validation(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	sid, _ := startedSession(t, p)

	err := p.SendInput(context.Background(), sid,
		core.SessionInput{Mode: core.SessionInputMode("bogus"), Keys: "x"})
	if err == nil {
		t.Fatal("want validation error")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindValidation {
		t.Fatalf("want KindValidation, got %v (%T)", err, err)
	}
	if got := len(fb.sendInputCalls); got != 0 {
		t.Fatalf("backend SendInput must NOT be called on validation failure; got %d calls", got)
	}
}

func TestSendInput_UnknownSession_NotFound(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	err := p.SendInput(context.Background(), "no-such-session",
		core.SessionInput{Mode: core.InputLiteral, Keys: "x"})
	if err == nil {
		t.Fatal("want session-not-found error")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindSessionNotFound {
		t.Fatalf("want KindSessionNotFound, got %v (%T)", err, err)
	}
}

func TestSendInput_HappyPath_Literal(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	sid, pid := startedSession(t, p)

	// Snapshot pre-call count: StartSession's preamble path may have
	// recorded SendInput calls in some agent flows; lock in delta.
	before := len(fb.sendInputCalls)

	in := core.SessionInput{Mode: core.InputLiteral, Keys: "hello"}
	if err := p.SendInput(context.Background(), sid, in); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if got := len(fb.sendInputCalls) - before; got != 1 {
		t.Fatalf("want exactly 1 backend SendInput call, got %d", got)
	}
	last := fb.sendInputCalls[len(fb.sendInputCalls)-1]
	if last.paneID != pid {
		t.Errorf("paneID: got %q want %q", last.paneID, pid)
	}
	if last.in != in {
		t.Errorf("input mismatch: got %+v want %+v", last.in, in)
	}
}

func TestSendInput_BackendNonTypedError_WrappedProcessFailed(t *testing.T) {
	fb := newFakeBackend()
	fb.sendInputErr = errors.New("boom")
	p := newSIOPlane(t, fb)
	sid, _ := startedSession(t, p)

	err := p.SendInput(context.Background(), sid,
		core.SessionInput{Mode: core.InputLiteral, Keys: "x"})
	if err == nil {
		t.Fatal("want error")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindProcessFailed {
		t.Fatalf("want KindProcessFailed, got %v (%T)", err, err)
	}
}

// ---- ResizeSession -----------------------------------------------

func TestResizeSession_BackendNil_Unsupported(t *testing.T) {
	p := NewWithConfig(Config{Registry: defaultRegistry()})
	err := p.ResizeSession(context.Background(), "sess-1", 80, 24)
	if err == nil {
		t.Fatal("want error when Backend nil")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindUnsupported {
		t.Fatalf("want KindUnsupported, got %v (%T)", err, err)
	}
}

func TestResizeSession_EmptySessionID_Validation(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	err := p.ResizeSession(context.Background(), "", 80, 24)
	if err == nil {
		t.Fatal("want validation error")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindValidation {
		t.Fatalf("want KindValidation, got %v (%T)", err, err)
	}
}

func TestResizeSession_ZeroCols_Validation(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	err := p.ResizeSession(context.Background(), "sess-1", 0, 24)
	if err == nil {
		t.Fatal("want validation error")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindValidation {
		t.Fatalf("want KindValidation, got %v (%T)", err, err)
	}
}

func TestResizeSession_UnknownSession_NotFound(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	err := p.ResizeSession(context.Background(), "no-such-session", 80, 24)
	if err == nil {
		t.Fatal("want session-not-found error")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindSessionNotFound {
		t.Fatalf("want KindSessionNotFound, got %v (%T)", err, err)
	}
}

func TestResizeSession_HappyPath_NoOp(t *testing.T) {
	fb := newFakeBackend()
	p := newSIOPlane(t, fb)
	sid, _ := startedSession(t, p)

	beforeSI := len(fb.sendInputCalls)
	beforeSpawn := len(fb.spawnCalls)
	beforeKill := len(fb.killCalls)

	if err := p.ResizeSession(context.Background(), sid, 120, 40); err != nil {
		t.Fatalf("ResizeSession: %v", err)
	}
	// No-op: backend MUST NOT be touched.
	if got := len(fb.sendInputCalls); got != beforeSI {
		t.Errorf("Resize must not call SendInput; got %d new calls", got-beforeSI)
	}
	if got := len(fb.spawnCalls); got != beforeSpawn {
		t.Errorf("Resize must not spawn; got %d new spawns", got-beforeSpawn)
	}
	if got := len(fb.killCalls); got != beforeKill {
		t.Errorf("Resize must not kill; got %d new kills", got-beforeKill)
	}
}
