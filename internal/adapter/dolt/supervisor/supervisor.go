// Package supervisor spawns and supervises an embedded `dolt sql-server`
// subprocess for the single-user OSS core (gm-o9t8.1.2.3).
//
// The supervisor owns the lifecycle:
//
//   - Start launches dolt sql-server bound to a loopback TCP port (the AC
//     calls for a UNIX socket, but Dolt's --socket support has historically
//     been spotty in OSS releases; default-to-TCP-on-loopback is the
//     documented deviation — see PORT mode in the package docs).
//   - Once started, Start blocks until a SELECT 1 round-trip succeeds (or
//     ctx is cancelled). It returns nil only when the server is reachable.
//   - A background goroutine then polls SELECT 1 every healthCheckInterval
//     and toggles the atomic Ready flag. On consecutive failures the
//     supervisor restarts the subprocess with exponential backoff.
//   - After maxConsecutiveFailures (default: 5) consecutive failed restarts
//     the supervisor gives up, closes the Wait channel with the last error,
//     and stays not-ready. /api/readyz then reflects 503 to the operator.
//
// All goroutines honor context cancellation; Stop is idempotent.
package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	// Imported for its side-effect registration of the "mysql" driver.
	_ "github.com/go-sql-driver/mysql"
)

// Backoff + health constants. v1 is intentionally non-configurable
// at the public API surface; per the bead's acceptance criteria,
// twiddling these belongs in a follow-up if the defaults bite an
// operator. They are package-level vars (not consts) only so the
// test package can shorten them for negative-path lifecycle tests
// — production code never assigns to them.
var (
	backoffBase             = 500 * time.Millisecond
	backoffFactor           = 2.0
	backoffMax              = 30 * time.Second
	maxConsecutiveFailures  = 5
	healthCheckInterval     = 5 * time.Second
	healthCheckTimeout      = 3 * time.Second
	startupReadyTimeout     = 30 * time.Second
	gracefulShutdownTimeout = 10 * time.Second
)

// Config is the supervisor's construction-time configuration. Zero-value
// is invalid — DataDir must be non-empty. Port=0 means "pick a free
// loopback port at start time."
type Config struct {
	// DoltBin is the path to the dolt binary. Empty means "look up
	// `dolt` on $PATH at Start time."
	DoltBin string

	// DataDir is the directory dolt sql-server will use as its
	// working dir (`--data-dir`). Created with 0700 if missing.
	// Required.
	DataDir string

	// Host is the loopback host to bind. Empty defaults to "127.0.0.1".
	Host string

	// Port is the TCP port to bind. Zero means "find a free port
	// at start time and remember it."
	Port int

	// User is the MySQL user the health check connects as. Empty
	// defaults to "root" which is dolt sql-server's default no-auth
	// superuser when --auth is not configured.
	User string

	// Logger is the slog logger subprocess stderr/stdout is plumbed
	// to. Nil falls back to slog.Default().
	Logger *slog.Logger

	// commandFactory is a test seam — when non-nil the supervisor
	// uses it instead of exec.CommandContext. Production code leaves
	// it nil. Exposed via WithCommandFactory.
	commandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

	// healthCheck is a test seam — when non-nil it overrides the
	// MySQL SELECT 1 probe. Useful for fake-process lifecycle tests
	// where there's no real SQL server to talk to.
	healthCheck func(ctx context.Context) error
}

// Supervisor is safe for concurrent use; all state transitions are
// guarded by mu or expressed as atomics. The only allowed lifecycle
// is: NewSupervisor → Start → (Ready/Wait observed) → Stop. Re-using
// the same Supervisor after Stop returns is undefined.
type Supervisor struct {
	cfg Config
	log *slog.Logger

	mu  sync.Mutex
	cmd *exec.Cmd

	ready   atomic.Bool
	stopped atomic.Bool

	// restartCount is incremented every time the supervisor decides
	// to (re)spawn a replacement subprocess after a failing health
	// probe. The initial Start spawn does NOT count — it's the steady
	// state. Read by RestartCount() for the /api/v1/workspaces/{wsid}/
	// status embedded-dolt stripe (gm-o9t8.1.9).
	restartCount atomic.Int64

	// lastErr stores the most recent terminal / probe error so the
	// status handler can surface a `last_error` in the degraded-state
	// banner. Cleared on a successful probe; sticky across the
	// "giving up" terminal transition.
	lastErr atomic.Pointer[error]

	// starting flips to true the moment Start is called and to false
	// once awaitReady returns nil for the first time. It lets the
	// status handler distinguish the "still starting" lane from the
	// "ready then went sideways → restarting" lane.
	starting atomic.Bool

	done    chan struct{}      // closed when the supervisor permanently fails
	doneErr atomic.Pointer[error]

	cancel context.CancelFunc // cancels the supervisor goroutines
	wg     sync.WaitGroup
}

// New constructs a Supervisor from cfg. Returns an error if cfg is
// invalid (e.g. empty DataDir).
func New(cfg Config) (*Supervisor, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("supervisor: Config.DataDir is required")
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Supervisor{
		cfg:  cfg,
		log:  log,
		done: make(chan struct{}),
	}, nil
}

// WithCommandFactory installs a custom exec.Cmd factory for tests.
// Must be called before Start. Production callers leave the factory
// alone.
func (s *Supervisor) WithCommandFactory(f func(ctx context.Context, name string, args ...string) *exec.Cmd) {
	s.cfg.commandFactory = f
}

// WithHealthCheck installs a custom health-check probe for tests.
// Must be called before Start.
func (s *Supervisor) WithHealthCheck(f func(ctx context.Context) error) {
	s.cfg.healthCheck = f
}

// Port returns the port the dolt server is (or will be) bound to.
// After Start returns nil this is guaranteed to be the actual port —
// a Config.Port==0 input will have been resolved to a concrete free
// port by then.
func (s *Supervisor) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Port
}

// DSN returns a go-sql-driver compatible MySQL DSN pointing at the
// supervised server. The path component is empty (no default database).
func (s *Supervisor) DSN() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%s@tcp(%s:%d)/", s.cfg.User, s.cfg.Host, s.cfg.Port)
}

// Ready reports whether the latest health check observed a reachable
// dolt server. Drops to false the instant a probe fails; flips back
// after a successful restart + SELECT 1.
func (s *Supervisor) Ready() bool { return s.ready.Load() }

// Wait returns a channel that closes when the supervisor permanently
// fails (after maxConsecutiveFailures consecutive failed restarts) or
// is intentionally stopped.
func (s *Supervisor) Wait() <-chan struct{} { return s.done }

// Err returns the terminal error after Wait fires. Returns nil for a
// clean Stop, non-nil for an exhausted-backoff failure.
func (s *Supervisor) Err() error {
	if p := s.doneErr.Load(); p != nil {
		return *p
	}
	return nil
}

// RestartCount returns the cumulative number of subprocess respawns
// triggered by failing health probes since this Supervisor was
// constructed. The initial Start spawn does not count.
func (s *Supervisor) RestartCount() int64 { return s.restartCount.Load() }

// LastError returns the most recent probe / supervise error, or nil
// when none has been observed (or when the last probe succeeded).
// Cleared on a successful health check; sticky across the "giving up"
// terminal transition (in which case Err() == LastError()).
func (s *Supervisor) LastError() error {
	if p := s.lastErr.Load(); p != nil {
		return *p
	}
	return nil
}

// State returns the supervisor's coarse-grained lifecycle state for
// status / UI purposes. Values: "starting", "ready", "restarting",
// "failed", "stopped". Distinct from Ready() which is a boolean.
func (s *Supervisor) State() string {
	if s.stopped.Load() {
		return "stopped"
	}
	// "failed" beats every other state — done channel closed with a
	// non-nil terminal error.
	select {
	case <-s.done:
		if s.Err() != nil {
			return "failed"
		}
		return "stopped"
	default:
	}
	if s.ready.Load() {
		return "ready"
	}
	if s.starting.Load() {
		return "starting"
	}
	return "restarting"
}

// Start launches the dolt subprocess and blocks until either a
// SELECT 1 probe succeeds, ctx is cancelled, or startupReadyTimeout
// elapses. Returns nil on success. The caller MUST eventually call
// Stop to release the subprocess and goroutines, even on error.
func (s *Supervisor) Start(ctx context.Context) error {
	s.starting.Store(true)
	defer s.starting.Store(false)
	// Ensure data dir exists with 0700 mode.
	if err := os.MkdirAll(s.cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("supervisor: mkdir %q: %w", s.cfg.DataDir, err)
	}

	// Resolve dolt binary if unspecified. Defer the lookup error to
	// the first spawn; the test path (custom commandFactory) doesn't
	// need a real dolt on PATH.
	if s.cfg.DoltBin == "" && s.cfg.commandFactory == nil {
		path, err := exec.LookPath("dolt")
		if err != nil {
			return fmt.Errorf("supervisor: %w (dolt not on PATH; install Dolt or pass --dolt-url)", err)
		}
		s.cfg.DoltBin = path
	}

	// Resolve a free port if unspecified.
	if s.cfg.Port == 0 {
		p, err := pickFreePort(s.cfg.Host)
		if err != nil {
			return fmt.Errorf("supervisor: pick free port: %w", err)
		}
		s.cfg.Port = p
	}

	// Launch the subprocess.
	if err := s.spawn(ctx); err != nil {
		return fmt.Errorf("supervisor: spawn: %w", err)
	}

	// Wait for the server to accept a SELECT 1, bounded by
	// startupReadyTimeout. Spin with the backoff base interval so we
	// don't hammer the loopback while dolt is still opening its
	// listener.
	waitCtx, cancel := context.WithTimeout(ctx, startupReadyTimeout)
	defer cancel()
	if err := s.awaitReady(waitCtx); err != nil {
		// Roll back the spawn — we don't want an orphan dolt running
		// against the caller's expectation that Start failed.
		_ = s.killChild()
		return fmt.Errorf("supervisor: never became ready: %w", err)
	}

	// Promote to ready and start the long-running supervisor loop.
	s.ready.Store(true)
	superCtx, cancelSuper := context.WithCancel(context.Background())
	s.cancel = cancelSuper
	s.wg.Add(1)
	go s.supervise(superCtx)
	return nil
}

// Stop sends SIGTERM, waits up to gracefulShutdownTimeout for the
// child to exit, and then SIGKILLs anything still alive. Safe to
// call multiple times.
func (s *Supervisor) Stop(ctx context.Context) error {
	if !s.stopped.CompareAndSwap(false, true) {
		return nil
	}
	s.ready.Store(false)
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()

	if err := s.killChild(); err != nil {
		s.log.Warn("supervisor: kill child returned error", "err", err)
	}

	// Mark Wait()'s channel closed if it isn't already (clean stop).
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	return nil
}

// spawn constructs and starts a fresh dolt sql-server subprocess.
// Caller holds no locks.
func (s *Supervisor) spawn(parentCtx context.Context) error {
	args := []string{
		"sql-server",
		"-H", s.cfg.Host,
		"-P", strconv.Itoa(s.cfg.Port),
		"--data-dir", s.cfg.DataDir,
		"--loglevel=warning",
	}

	// Use a fresh exec context per spawn so we can kill the child
	// independently of the supervisor's lifetime context.
	cmdCtx := context.Background()
	var cmd *exec.Cmd
	if s.cfg.commandFactory != nil {
		cmd = s.cfg.commandFactory(cmdCtx, s.cfg.DoltBin, args...)
	} else {
		cmd = exec.CommandContext(cmdCtx, s.cfg.DoltBin, args...)
	}

	// Plumb stdout+stderr to slog DEBUG via line-oriented pipes.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// New process group so SIGTERM to gemba doesn't propagate as
	// SIGINT to dolt before we get to drain the HTTP server.
	cmd.SysProcAttr = newProcAttr()

	if err := cmd.Start(); err != nil {
		return err
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	go pipeToSlog(stdoutPipe, s.log, slog.LevelDebug, "dolt.stdout")
	go pipeToSlog(stderrPipe, s.log, slog.LevelDebug, "dolt.stderr")

	s.log.Info("supervisor: dolt sql-server spawned",
		"pid", cmd.Process.Pid,
		"host", s.cfg.Host,
		"port", s.cfg.Port,
		"data_dir", s.cfg.DataDir)
	return nil
}

// awaitReady polls the health check until it succeeds or ctx fires.
func (s *Supervisor) awaitReady(ctx context.Context) error {
	probe := s.cfg.healthCheck
	if probe == nil {
		probe = s.defaultHealthCheck
	}
	ticker := time.NewTicker(backoffBase)
	defer ticker.Stop()
	// Immediate first attempt so a fast-starting server is detected
	// promptly.
	if err := probe(ctx); err == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := probe(ctx); err == nil {
				return nil
			}
		}
	}
}

// supervise watches the running child + health-checks on a 5s
// cadence. On consecutive failures it restarts with exponential
// backoff up to maxConsecutiveFailures, then gives up.
func (s *Supervisor) supervise(ctx context.Context) {
	defer s.wg.Done()

	probe := s.cfg.healthCheck
	if probe == nil {
		probe = s.defaultHealthCheck
	}

	failures := 0
	delay := backoffBase
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		probeCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
		err := probe(probeCtx)
		cancel()
		if err == nil {
			s.ready.Store(true)
			s.lastErr.Store(nil)
			failures = 0
			delay = backoffBase
			continue
		}

		s.ready.Store(false)
		probeErr := err
		s.lastErr.Store(&probeErr)
		failures++
		s.log.Warn("supervisor: dolt health check failed",
			"err", err, "consecutive_failures", failures)

		if failures >= maxConsecutiveFailures {
			finalErr := fmt.Errorf("dolt sql-server failed %d consecutive health checks: %w",
				failures, err)
			s.doneErr.Store(&finalErr)
			s.lastErr.Store(&finalErr)
			s.log.Error("supervisor: giving up on dolt",
				"err", finalErr)
			select {
			case <-s.done:
			default:
				close(s.done)
			}
			return
		}

		// Kill the (possibly half-alive) child and restart.
		_ = s.killChild()

		// Backoff before restart.
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = nextBackoff(delay)

		s.restartCount.Add(1)
		if err := s.spawn(ctx); err != nil {
			respawnErr := err
			s.lastErr.Store(&respawnErr)
			s.log.Warn("supervisor: respawn failed",
				"err", err, "consecutive_failures", failures)
			continue
		}
		// Quick post-respawn ready probe; success resets the counter
		// on the next tick.
		readyCtx, readyCancel := context.WithTimeout(ctx, startupReadyTimeout)
		if err := s.awaitReady(readyCtx); err == nil {
			s.ready.Store(true)
			failures = 0
			delay = backoffBase
		}
		readyCancel()
	}
}

// killChild sends SIGTERM, waits up to gracefulShutdownTimeout, then
// SIGKILLs. Safe when no child is running.
func (s *Supervisor) killChild() error {
	s.mu.Lock()
	cmd := s.cmd
	s.cmd = nil
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid

	// SIGTERM the whole process group when possible so any dolt
	// children also receive the signal.
	if err := terminateProcess(cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		s.log.Debug("supervisor: SIGTERM returned error",
			"pid", pid, "err", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
		return nil
	case <-time.After(gracefulShutdownTimeout):
		s.log.Warn("supervisor: dolt did not exit after SIGTERM; sending SIGKILL",
			"pid", pid)
		_ = cmd.Process.Signal(syscall.SIGKILL)
		<-exited
		return nil
	}
}

// defaultHealthCheck opens a fresh MySQL connection, runs SELECT 1,
// and closes it. We don't pool connections — the supervisor probes
// at 5s intervals and an idle pooled conn would just rot.
func (s *Supervisor) defaultHealthCheck(ctx context.Context) error {
	db, err := sql.Open("mysql", s.DSN())
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	row := db.QueryRowContext(ctx, "SELECT 1")
	var v int
	if err := row.Scan(&v); err != nil {
		return fmt.Errorf("SELECT 1: %w", err)
	}
	return nil
}

// pickFreePort asks the kernel for an ephemeral port by binding,
// reading the assignment, and closing. There's a tiny race between
// close and the subprocess binding the same port; on a single-user
// dev box this is overwhelmingly fine in practice.
func pickFreePort(host string) (int, error) {
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener addr type %T", l.Addr())
	}
	return addr.Port, nil
}

func nextBackoff(d time.Duration) time.Duration {
	next := time.Duration(float64(d) * backoffFactor)
	if next > backoffMax {
		return backoffMax
	}
	return next
}

// DataDir returns the supervisor's resolved data directory (mostly
// for logging / display).
func (s *Supervisor) DataDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return filepath.Clean(s.cfg.DataDir)
}
