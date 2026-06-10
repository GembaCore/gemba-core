package native

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// withCleanWorktree swaps the worktreeIsClean stub so the test gets
// a deterministic answer without a real git checkout.
func withCleanWorktree(t *testing.T, clean bool) {
	t.Helper()
	prior := worktreeIsClean
	worktreeIsClean = func(string) bool { return clean }
	t.Cleanup(func() { worktreeIsClean = prior })
}

// makeReadySession is a shared helper: spin up a session, then flip
// it to SessionReady directly (skipping the bead-done state-event
// path so the test focuses on recycle alone).
func makeReadySession(t *testing.T) (*OrchestrationPlane, core.Session) {
	t.Helper()
	fb := newFakeBackend()
	p := NewWithConfig(Config{
		Backend:          fb,
		Registry:         defaultRegistry(),
		RepoRoot:         initRepo(t),
		DisableReaper:    true,
		DisableReconcile: true,
	})
	sess, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	p.mu.Lock()
	p.sessions[sess.ID].Status = core.SessionReady
	p.sessions[sess.ID].ProviderMetadata["last_bead_done_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	p.mu.Unlock()
	return p, sess
}

func TestRecycleSession_RejectsMidBead(t *testing.T) {
	withCleanWorktree(t, true)
	p, sess := makeReadySession(t)
	// Force the session back to Working — recycle on a non-Ready
	// session must fail with KindValidation.
	p.mu.Lock()
	p.sessions[sess.ID].Status = core.SessionWorking
	p.mu.Unlock()

	_, err := p.RecycleSession(context.Background(), sess.ID)
	if err == nil {
		t.Fatal("expected error recycling Working session")
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T (%v)", err, err)
	}
}

func TestRecycleSession_HappyPath_MintsNewID(t *testing.T) {
	withCleanWorktree(t, true)
	p, sess := makeReadySession(t)

	newSess, err := p.RecycleSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("RecycleSession: %v", err)
	}
	if newSess.ID == sess.ID {
		t.Errorf("recycle should mint a fresh id; got same %q", newSess.ID)
	}
	if newSess.Status != core.SessionInitializing {
		t.Errorf("post-recycle status: got %q want Initializing", newSess.Status)
	}
	// Pane id stable.
	if got, want := newSess.ProviderMetadata["pane_id"], sess.ProviderMetadata["pane_id"]; got != want {
		t.Errorf("pane id changed: got %v want %v", got, want)
	}
	// Old session removed from sessions map; new session registered.
	p.mu.Lock()
	_, oldOk := p.sessions[sess.ID]
	_, newOk := p.sessions[newSess.ID]
	p.mu.Unlock()
	if oldOk {
		t.Error("old session still in adaptor map")
	}
	if !newOk {
		t.Error("new session missing from adaptor map")
	}
}

func TestRecycleSession_RefusesDirtyWorktree(t *testing.T) {
	withCleanWorktree(t, false)
	p, sess := makeReadySession(t)

	_, err := p.RecycleSession(context.Background(), sess.ID)
	if err == nil {
		t.Fatal("expected error on dirty-worktree recycle")
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error should reference uncommitted changes; got %v", err)
	}
	// Refused recycle converts to End, so the session is now terminal.
	p.mu.Lock()
	s, ok := p.sessions[sess.ID]
	p.mu.Unlock()
	if ok && s != nil && !isTerminalStatus(s.Status) {
		t.Errorf("after refused recycle, session should be terminal; got %q", s.Status)
	}
}

func TestRecycleSession_UnknownSession(t *testing.T) {
	fb := newFakeBackend()
	p := NewWithConfig(Config{
		Backend:          fb,
		Registry:         defaultRegistry(),
		DisableReaper:    true,
		DisableReconcile: true,
	})
	_, err := p.RecycleSession(context.Background(), "no-such")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindSessionNotFound {
		t.Errorf("want KindSessionNotFound, got %T (%v)", err, err)
	}
}

func TestRecycleSession_ZeroConfigUnsupported(t *testing.T) {
	p := New()
	_, err := p.RecycleSession(context.Background(), "x")
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindUnsupported {
		t.Errorf("want KindUnsupported, got %T (%v)", err, err)
	}
}
