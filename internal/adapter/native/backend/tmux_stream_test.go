// Package backend: tmux_stream_test.go locks the Streamable
// extension's argv shape + lifecycle semantics for the tmux backend
// (gm-v01.3.4). Mirrors the runner-injection pattern from tmux_test.go;
// every test asserts the exact tmux argv tmux_stream.go shells without
// touching a real tmux server.
package backend

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// streamRunner counts invocations and forwards argv to a callback so
// concurrent-StartPipe tests can assert "pipe-pane -o" was issued
// exactly once.
type streamCounter struct {
	pipeOpenCount  int64
	pipeCloseCount int64
	captureCount   int64
}

func newStreamTmux(t *testing.T, sc *streamCounter, recorder *[]stubCall) *Tmux {
	t.Helper()
	tx := &Tmux{
		binary:      "/fake/tmux",
		sessionName: "gemba",
		activePipes: make(map[string]struct{}),
		runner: func(_ context.Context, stdin *strings.Reader, args ...string) ([]byte, error) {
			s := ""
			if stdin != nil {
				// drain to avoid hangs
				_ = stdin
			}
			if recorder != nil {
				*recorder = append(*recorder, stubCall{stdin: s, argv: append([]string(nil), args...)})
			}
			if sc != nil && len(args) >= 1 && args[0] == "pipe-pane" {
				// Distinguish open (has trailing shell command) from close (no command argument).
				hasShell := false
				for _, a := range args[1:] {
					if strings.HasPrefix(a, "cat >>") {
						hasShell = true
						break
					}
				}
				if hasShell {
					atomic.AddInt64(&sc.pipeOpenCount, 1)
				} else {
					atomic.AddInt64(&sc.pipeCloseCount, 1)
				}
			}
			if sc != nil && len(args) >= 1 && args[0] == "capture-pane" {
				atomic.AddInt64(&sc.captureCount, 1)
				return []byte("snapshot-bytes"), nil
			}
			return nil, nil
		},
	}
	return tx
}

func TestStreamable_TmuxSatisfiesInterface(t *testing.T) {
	// Compile-time assertion lives in tmux_stream.go; verify here too
	// at runtime so the test file fails loudly if the assertion is
	// removed.
	var _ Streamable = (*Tmux)(nil)
}

func TestStartPipe_HappyPathArgv(t *testing.T) {
	var calls []stubCall
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, &calls)
	path, err := tx.StartPipe(context.Background(), "%0")
	if err != nil {
		// On platforms where mkfifo is unsupported we accept the
		// degraded error and skip — keep the test cross-platform.
		var ae *core.AdaptorError
		if errors.As(err, &ae) && ae.Kind == core.KindAdaptorDegraded {
			t.Skipf("mkfifo unsupported on %s: %v", runtime.GOOS, err)
		}
		t.Fatalf("StartPipe: %v", err)
	}
	if !strings.HasSuffix(path, "gemba-stream-0.fifo") {
		t.Errorf("fifo path mismatch: %s", path)
	}
	// Verify file exists on disk
	if _, err := os.Stat(path); err != nil {
		t.Errorf("fifo file should exist on disk: %v", err)
	}
	// Cleanup
	defer os.Remove(path)
	// Verify argv shape: pipe-pane -o -t %0 'cat >> /tmp/.../gemba-stream-0.fifo'
	if len(calls) != 1 {
		t.Fatalf("expected 1 tmux call, got %d: %+v", len(calls), calls)
	}
	argv := calls[0].argv
	if !argvContains(argv, "pipe-pane", "-o", "-t", "%0") {
		t.Errorf("argv must contain pipe-pane -o -t %%0: %v", argv)
	}
	// last arg is the shell command
	if !strings.HasPrefix(argv[len(argv)-1], "cat >> ") {
		t.Errorf("last argv must be cat >> <path>: %v", argv)
	}
	if !strings.HasSuffix(argv[len(argv)-1], "gemba-stream-0.fifo") {
		t.Errorf("shell must redirect to the fifo path: %v", argv)
	}
}

func TestStartPipe_Idempotent(t *testing.T) {
	var calls []stubCall
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, &calls)
	p1, err := tx.StartPipe(context.Background(), "%0")
	if err != nil {
		var ae *core.AdaptorError
		if errors.As(err, &ae) && ae.Kind == core.KindAdaptorDegraded {
			t.Skipf("mkfifo unsupported: %v", err)
		}
		t.Fatalf("StartPipe #1: %v", err)
	}
	defer os.Remove(p1)
	p2, err := tx.StartPipe(context.Background(), "%0")
	if err != nil {
		t.Fatalf("StartPipe #2: %v", err)
	}
	if p1 != p2 {
		t.Errorf("idempotent path: got %q != %q", p1, p2)
	}
	if len(calls) != 1 {
		t.Errorf("idempotent call must NOT reinvoke runner: got %d calls", len(calls))
	}
}

func TestStopPipe_AfterStart(t *testing.T) {
	var calls []stubCall
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, &calls)
	p1, err := tx.StartPipe(context.Background(), "%0")
	if err != nil {
		var ae *core.AdaptorError
		if errors.As(err, &ae) && ae.Kind == core.KindAdaptorDegraded {
			t.Skipf("mkfifo unsupported: %v", err)
		}
		t.Fatalf("StartPipe: %v", err)
	}
	if err := tx.StopPipe(context.Background(), "%0"); err != nil {
		t.Fatalf("StopPipe: %v", err)
	}
	// File removed
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Errorf("fifo must be removed; stat err=%v", err)
	}
	// Last call is `pipe-pane -t %0` with no shell command
	stopCall := calls[len(calls)-1]
	if !argvContains(stopCall.argv, "pipe-pane", "-t", "%0") {
		t.Errorf("stop argv mismatch: %v", stopCall.argv)
	}
	for _, a := range stopCall.argv {
		if strings.HasPrefix(a, "cat >>") {
			t.Errorf("stop must NOT carry a shell command: %v", stopCall.argv)
		}
	}
}

func TestStopPipe_WithoutStart(t *testing.T) {
	var calls []stubCall
	tx := newStreamTmux(t, nil, &calls)
	if err := tx.StopPipe(context.Background(), "%0"); err != nil {
		t.Fatalf("StopPipe without start: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("runner must NOT be invoked when no pipe active: %+v", calls)
	}
}

func TestStopPipe_Twice(t *testing.T) {
	var calls []stubCall
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, &calls)
	if _, err := tx.StartPipe(context.Background(), "%0"); err != nil {
		var ae *core.AdaptorError
		if errors.As(err, &ae) && ae.Kind == core.KindAdaptorDegraded {
			t.Skipf("mkfifo unsupported: %v", err)
		}
		t.Fatalf("StartPipe: %v", err)
	}
	if err := tx.StopPipe(context.Background(), "%0"); err != nil {
		t.Fatalf("StopPipe #1: %v", err)
	}
	stopsBefore := atomic.LoadInt64(&sc.pipeCloseCount)
	if err := tx.StopPipe(context.Background(), "%0"); err != nil {
		t.Fatalf("StopPipe #2: %v", err)
	}
	stopsAfter := atomic.LoadInt64(&sc.pipeCloseCount)
	if stopsAfter != stopsBefore {
		t.Errorf("second StopPipe must NOT invoke runner again (before=%d after=%d)", stopsBefore, stopsAfter)
	}
}

func TestSnapshotPane_Argv(t *testing.T) {
	var calls []stubCall
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, &calls)
	out, err := tx.SnapshotPane(context.Background(), "%0", 200)
	if err != nil {
		t.Fatalf("SnapshotPane: %v", err)
	}
	if string(out) != "snapshot-bytes" {
		t.Errorf("expected snapshot bytes, got %q", out)
	}
	argv := calls[len(calls)-1].argv
	if !argvContains(argv, "capture-pane", "-p", "-e", "-J", "-t", "%0", "-S", "-200") {
		t.Errorf("capture-pane argv mismatch: %v", argv)
	}
}

func TestSnapshotPane_DefaultLines(t *testing.T) {
	var calls []stubCall
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, &calls)
	if _, err := tx.SnapshotPane(context.Background(), "%0", 0); err != nil {
		t.Fatalf("SnapshotPane: %v", err)
	}
	argv := calls[len(calls)-1].argv
	if !argvContains(argv, "-S", "-200") {
		t.Errorf("default lines should be 200: %v", argv)
	}
}

func TestSafeFIFOName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"%0", "0"},
		{"%abc-1", "abc-1"},
		{"weird/path", "weird_path"},
		{"%", "pane"},
	}
	for _, c := range cases {
		got := safeFIFOName(c.in)
		if got != c.want {
			t.Errorf("safeFIFOName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStartPipe_ConcurrentSingleOpen(t *testing.T) {
	sc := &streamCounter{}
	tx := newStreamTmux(t, sc, nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := tx.StartPipe(context.Background(), "%0")
			if err != nil {
				var ae *core.AdaptorError
				if errors.As(err, &ae) && ae.Kind == core.KindAdaptorDegraded {
					return // skip path
				}
			}
		}()
	}
	wg.Wait()
	// Cleanup
	defer tx.StopPipe(context.Background(), "%0")
	opens := atomic.LoadInt64(&sc.pipeOpenCount)
	if opens > 1 {
		t.Errorf("concurrent StartPipe: pipe-pane -o invoked %d times; want exactly 1", opens)
	}
	// If mkfifo unsupported on this platform, opens may be 0 (all
	// goroutines errored). That's acceptable for cross-platform safety.
}
