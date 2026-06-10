package native

import (
	"context"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

func TestIdleReaper_DrainsStaleReadySessions(t *testing.T) {
	fb := newFakeBackend()
	p := NewWithConfig(Config{
		Backend:          fb,
		Registry:         defaultRegistry(),
		RepoRoot:         initRepo(t),
		DisableReaper:    true, // we drive ticks manually
		DisableReconcile: true,
	})
	sess, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	// Force the session into Ready with an old idle timestamp so the
	// reaper should drain it.
	old := time.Now().Add(-time.Hour)
	p.mu.Lock()
	p.sessions[sess.ID].Status = core.SessionReady
	p.sessions[sess.ID].ProviderMetadata["last_bead_done_at"] =
		old.UTC().Format(time.RFC3339Nano)
	p.mu.Unlock()

	r := &idleReaper{
		plane: p,
		cfg: ReaperConfig{
			IdleCeiling: time.Minute,
			Now:         func() time.Time { return time.Now() },
		},
	}
	r.tickOnce(context.Background())

	p.mu.Lock()
	s := p.sessions[sess.ID]
	p.mu.Unlock()
	if s == nil {
		t.Fatal("session removed entirely; expected terminal record")
	}
	if !isTerminalStatus(s.Status) {
		t.Errorf("expected terminal status post-reap; got %q", s.Status)
	}
}

func TestIdleReaper_LeavesFreshIdleSessionsAlone(t *testing.T) {
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
	// Recently went idle.
	now := time.Now()
	p.mu.Lock()
	p.sessions[sess.ID].Status = core.SessionReady
	p.sessions[sess.ID].ProviderMetadata["last_bead_done_at"] =
		now.UTC().Format(time.RFC3339Nano)
	p.mu.Unlock()

	r := &idleReaper{
		plane: p,
		cfg: ReaperConfig{
			IdleCeiling: time.Hour,
			Now:         func() time.Time { return now },
		},
	}
	r.tickOnce(context.Background())

	p.mu.Lock()
	s := p.sessions[sess.ID]
	p.mu.Unlock()
	if s == nil || s.Status != core.SessionReady {
		t.Errorf("fresh idle session should be untouched; got %v", s)
	}
}

func TestIdleReaper_SkipsNonReadySessions(t *testing.T) {
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
	// Working sessions are off-limits to the reaper even with old
	// timestamps.
	p.mu.Lock()
	p.sessions[sess.ID].Status = core.SessionWorking
	p.sessions[sess.ID].ProviderMetadata["last_bead_done_at"] =
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	p.mu.Unlock()

	r := &idleReaper{
		plane: p,
		cfg: ReaperConfig{
			IdleCeiling: time.Minute,
			Now:         func() time.Time { return time.Now() },
		},
	}
	r.tickOnce(context.Background())

	p.mu.Lock()
	s := p.sessions[sess.ID]
	p.mu.Unlock()
	if s == nil || s.Status != core.SessionWorking {
		t.Errorf("Working session should not be reaped; got %v", s)
	}
}
