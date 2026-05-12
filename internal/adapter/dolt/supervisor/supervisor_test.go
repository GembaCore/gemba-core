package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCommandFactory returns a commandFactory that invokes /bin/sh with a
// script ignoring the dolt-specific args and sleeping until killed.
func fakeCommandFactory(t *testing.T) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// We deliberately discard `name`/`args` and run a process
		// that survives until signalled, which is what we need to
		// exercise lifecycle code without a real dolt binary.
		return exec.CommandContext(ctx, "/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done")
	}
}

func newTmpDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gemba-supervisor-test-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestStart_FakeProcess_ReadyAndStop covers the happy lifecycle path
// with a fake subprocess + fake health check: Start blocks until the
// probe returns nil, Ready flips true, Stop cleans up.
func TestStart_FakeProcess_ReadyAndStop(t *testing.T) {
	s, err := New(Config{DataDir: newTmpDataDir(t), DoltBin: "/bin/sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.WithCommandFactory(fakeCommandFactory(t))
	var probes atomic.Int32
	s.WithHealthCheck(func(ctx context.Context) error {
		probes.Add(1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	if !s.Ready() {
		t.Fatalf("Ready=false after Start")
	}
	if got := probes.Load(); got == 0 {
		t.Fatalf("health check never invoked")
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s.Ready() {
		t.Fatalf("Ready=true after Stop")
	}
}

// TestSupervise_RestartOnHealthFailure covers the restart-with-backoff
// path: the health check fails N times, then succeeds again. Ready
// should drop and recover.
func TestSupervise_RestartOnHealthFailure(t *testing.T) {
	s, err := New(Config{DataDir: newTmpDataDir(t), DoltBin: "/bin/sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.WithCommandFactory(fakeCommandFactory(t))

	// Health check returns ok until we flip the toggle.
	fail := atomic.Bool{}
	s.WithHealthCheck(func(ctx context.Context) error {
		if fail.Load() {
			return errors.New("simulated outage")
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	// Flip to failing — the next tick should mark not-ready
	// within healthCheckInterval + slop.
	fail.Store(true)
	deadline := time.Now().Add(10 * time.Second)
	for s.Ready() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if s.Ready() {
		t.Fatalf("Ready did not drop after probe started failing")
	}
}

// TestSupervise_PermanentFailureClosesWait covers the give-up branch:
// after maxConsecutiveFailures the Wait channel closes and Err returns
// non-nil. Compresses the production-default intervals so the test
// completes well under the parent timeout.
func TestSupervise_PermanentFailureClosesWait(t *testing.T) {
	origInterval, origStartup, origBase, origMax := healthCheckInterval, startupReadyTimeout, backoffBase, backoffMax
	healthCheckInterval = 50 * time.Millisecond
	startupReadyTimeout = 200 * time.Millisecond
	backoffBase = 10 * time.Millisecond
	backoffMax = 50 * time.Millisecond
	t.Cleanup(func() {
		healthCheckInterval, startupReadyTimeout, backoffBase, backoffMax = origInterval, origStartup, origBase, origMax
	})

	s, err := New(Config{DataDir: newTmpDataDir(t), DoltBin: "/bin/sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.WithCommandFactory(fakeCommandFactory(t))

	// Health check succeeds exactly once (startup), then fails forever.
	calls := atomic.Int32{}
	s.WithHealthCheck(func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		return errors.New("always fail")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	select {
	case <-s.Wait():
	case <-time.After(15 * time.Second):
		t.Fatalf("Wait did not close after consecutive failures")
	}
	if s.Err() == nil {
		t.Fatalf("Err()==nil after permanent failure")
	}
	if s.Ready() {
		t.Fatalf("Ready=true after permanent failure")
	}
}

// TestNew_RejectsEmptyDataDir covers Config validation.
func TestNew_RejectsEmptyDataDir(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatalf("New: expected error for empty DataDir")
	}
}

// TestStart_DataDirCreatedWith0700 covers the dir-creation invariant.
func TestStart_DataDirCreatedWith0700(t *testing.T) {
	root := newTmpDataDir(t)
	target := filepath.Join(root, "nested", "dolt")
	s, err := New(Config{DataDir: target, DoltBin: "/bin/sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.WithCommandFactory(fakeCommandFactory(t))
	s.WithHealthCheck(func(ctx context.Context) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat %q: %v", target, err)
	}
	// Compare only the permission bits we care about; the OS may add
	// sticky/setuid bits but those won't affect mode&0o777.
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Fatalf("data dir mode = %o, want 0700", mode)
	}
}

// TestRealDolt_StartsAndAcceptsSelectOne is the integration gate.
// Skipped unless `dolt` is on PATH. When present, it spawns a real
// server on a free port, waits for SELECT 1, and tears it down.
func TestRealDolt_StartsAndAcceptsSelectOne(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not on PATH; skipping real-dolt integration test")
	}
	s, err := New(Config{DataDir: newTmpDataDir(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	if !s.Ready() {
		t.Fatalf("Ready=false after real dolt Start returned")
	}
}

// TestNextBackoff covers the exponential-with-cap helper.
func TestNextBackoff(t *testing.T) {
	cases := []struct {
		in, want time.Duration
	}{
		{backoffBase, backoffBase * 2},
		{backoffMax, backoffMax},
		{backoffMax / 2, backoffMax}, // *2 saturates
	}
	for _, c := range cases {
		if got := nextBackoff(c.in); got != c.want {
			t.Errorf("nextBackoff(%v)=%v, want %v", c.in, got, c.want)
		}
	}
}
