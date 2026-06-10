//go:build integration

// Package native: session_io_integration_test.go is the Phase B
// real-tmux acceptance suite (Plan B-04, bead gm-v01.3.6). Three of the
// four Phase B ROADMAP success criteria (live echo round-trip,
// multi-subscriber fan-out, disconnect-storm FIFO cleanup) are
// integration-shaped — the unit suites in Plans 01-03 stub out tmux
// and so cannot discharge them directly. This file exercises the full
// OrchestrationPlane SessionIO trio against a real, isolated tmux
// server and asserts the criteria executably.
//
// Build-tag-guarded: `go test ./...` skips this file entirely. CI runs
// it via `go test -tags integration ./internal/adapter/native/...`.
// On hosts without tmux every test reports SKIP (not FAIL) so the
// build-tag knob is the only flag CI needs.
//
// All tests share a 10s top-level context so a wedged tmux pane cannot
// hang a CI agent indefinitely.
package native

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

const integrationDeadline = 10 * time.Second

// waitForBytes drains events from ch until one matches contains() or
// ctx expires. Returns the matching event (for further inspection) or
// fails the test on timeout. Centralized so every test uses the same
// deadlined-select shape rather than ad-hoc sleeps.
func waitForBytes(t *testing.T, ch <-chan core.SessionEvent, want []byte, timeout time.Duration, label string) core.SessionEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("%s: channel closed before bytes %q arrived", label, want)
			}
			if ev.Kind == "output" && bytes.Contains(ev.Bytes, want) {
				return ev
			}
			// Other events (snapshot before, status, etc.) just drain.
		case <-deadline:
			t.Fatalf("%s: timed out waiting for bytes %q", label, want)
		}
	}
}

// drainSnapshot reads the first event off ch (the per-subscriber
// snapshot the hub emits before any live output). Asserts Kind="output"
// — bytes can be empty for a fresh pane.
func drainSnapshot(t *testing.T, ch <-chan core.SessionEvent, label string) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed before snapshot arrived", label)
		}
		if ev.Kind != "output" {
			t.Fatalf("%s: snapshot kind=%q, want output", label, ev.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: timed out waiting for snapshot", label)
	}
}

// fifoFilesFor returns the FIFO paths under $TMPDIR matching this
// pane's expected name. Phase B uses a single naming scheme
// (gemba-stream-<safe-paneID>.fifo) so we glob on the safe-pane-id
// suffix to ignore other developers' sessions.
func fifoFilesFor(t *testing.T, paneID string) []string {
	t.Helper()
	// safeFIFOName strips the leading "%" from the pane id; replicate
	// the transformation here so we glob on the right suffix without
	// reaching into the backend package.
	safe := strings.TrimPrefix(paneID, "%")
	pattern := filepath.Join(os.TempDir(), "gemba-stream-"+safe+".fifo")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	return matches
}

// TestIntegrationEchoRoundTrip — Plan B-04 success criterion #1:
// a known string sent through SendInput is echoed back through
// StreamSession via the real `tmux pipe-pane` server-side fan-out.
// The pane runs `cat` so every byte written to stdin is mirrored to
// stdout, which the pipe-pane FIFO carries to the hub.
func TestIntegrationEchoRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationDeadline)
	defer cancel()

	plane := newIntegrationPlane(t, "cat")
	attachCtx, attachCancel := context.WithCancel(ctx)
	defer attachCancel()

	events, err := plane.StreamSession(attachCtx, plane.sessionID)
	if err != nil {
		t.Fatalf("StreamSession: %v", err)
	}
	drainSnapshot(t, events, "echo-snapshot")

	in := core.SessionInput{Mode: core.InputLiteral, Keys: "hello-gemba\n"}
	if err := plane.SendInput(ctx, plane.sessionID, in); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	waitForBytes(t, events, []byte("hello-gemba"), 3*time.Second, "echo-roundtrip")

	// Cancel ctx and assert the channel closes within 1s. Drain any
	// trailing events the hub emits during teardown.
	attachCancel()
	closed := time.After(1 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // success: channel closed cleanly
			}
		case <-closed:
			t.Fatal("events channel did not close within 1s of ctx cancel")
		}
	}
}

// TestIntegrationMultiSubscriberFanOut — Plan B-04 success criterion
// #2: two subscribers on the same sessionID share ONE backend
// pipe-pane (single FIFO file) and both receive every byte from the
// stream. Validates the hub's refcounted fan-out under real tmux.
func TestIntegrationMultiSubscriberFanOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationDeadline)
	defer cancel()

	plane := newIntegrationPlane(t, "cat")

	ctxA, cancelA := context.WithCancel(ctx)
	defer cancelA()
	chA, err := plane.StreamSession(ctxA, plane.sessionID)
	if err != nil {
		t.Fatalf("StreamSession A: %v", err)
	}
	drainSnapshot(t, chA, "fanout-A-snapshot")

	ctxB, cancelB := context.WithCancel(ctx)
	defer cancelB()
	chB, err := plane.StreamSession(ctxB, plane.sessionID)
	if err != nil {
		t.Fatalf("StreamSession B: %v", err)
	}
	drainSnapshot(t, chB, "fanout-B-snapshot")

	// Exactly one FIFO file should exist while both subscribers are
	// attached to one paneID. The hub's refcount guarantees this.
	if files := fifoFilesFor(t, plane.paneID); len(files) != 1 {
		t.Fatalf("expected 1 FIFO for paneID %s, got %d: %v", plane.paneID, len(files), files)
	}

	// One input -> both subscribers see it.
	in := core.SessionInput{Mode: core.InputLiteral, Keys: "shared-bytes\n"}
	if err := plane.SendInput(ctx, plane.sessionID, in); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	waitForBytes(t, chA, []byte("shared-bytes"), 3*time.Second, "fanout-A")
	waitForBytes(t, chB, []byte("shared-bytes"), 3*time.Second, "fanout-B")

	// Drop A; FIFO must still exist while B is attached and B must
	// continue to receive output. Give detach a beat to settle.
	cancelA()
	time.Sleep(200 * time.Millisecond)
	if files := fifoFilesFor(t, plane.paneID); len(files) != 1 {
		t.Fatalf("after cancel(A): expected 1 FIFO, got %d: %v", len(files), files)
	}

	in2 := core.SessionInput{Mode: core.InputLiteral, Keys: "only-B\n"}
	if err := plane.SendInput(ctx, plane.sessionID, in2); err != nil {
		t.Fatalf("SendInput#2: %v", err)
	}
	waitForBytes(t, chB, []byte("only-B"), 3*time.Second, "fanout-B-after-A-detached")

	// Drop B; the last detach must trigger StopPipe + FIFO removal.
	cancelB()
	// Allow up to 1s for teardown — readLoop has to unblock from
	// the FIFO read, fire its defers, and run StopPipe.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(fifoFilesFor(t, plane.paneID)) == 0 {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("FIFO not removed after last subscriber detached: %v", fifoFilesFor(t, plane.paneID))
}

// TestIntegrationDisconnectStorm — Plan B-04 success criterion #3:
// 50 rapid attach/detach cycles against one real tmux pane leave zero
// leaked FIFO files in $TMPDIR. Also asserts the live goroutine count
// settles back within a small tolerance of baseline so we catch
// goroutine leaks in the hub's per-subscriber ctx-watcher.
func TestIntegrationDisconnectStorm(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationDeadline)
	defer cancel()

	plane := newIntegrationPlane(t, "cat")

	// Baseline: another developer might have leftover FIFOs from
	// prior sessions. We only assert ZERO files for THIS pane id,
	// not zero files in $TMPDIR overall.
	if files := fifoFilesFor(t, plane.paneID); len(files) != 0 {
		t.Fatalf("baseline FIFO count for paneID %s: want 0, got %d (%v)", plane.paneID, len(files), files)
	}

	baselineGoroutines := runtime.NumGoroutine()

	const cycles = 50
	for i := 0; i < cycles; i++ {
		stormCtx, stormCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		ch, err := plane.StreamSession(stormCtx, plane.sessionID)
		if err != nil {
			stormCancel()
			t.Fatalf("StreamSession iter %d: %v", i, err)
		}
		// Drain at most one event (the snapshot). If the deadline
		// fires first that's fine — the storm scenario doesn't
		// require receiving anything; it's about the attach/detach
		// lifecycle.
		select {
		case <-ch:
		case <-stormCtx.Done():
		}
		stormCancel()
	}

	// Settle: readLoop teardown can lag detach by a few ms.
	time.Sleep(500 * time.Millisecond)

	if files := fifoFilesFor(t, plane.paneID); len(files) != 0 {
		t.Fatalf("storm leaked FIFOs for paneID %s: %v", plane.paneID, files)
	}

	// Goroutine tolerance: the hub itself + the test framework can
	// jitter the count by a handful. ±20 is generous enough to
	// avoid flakes while still catching a per-cycle leak (which
	// would push the delta to ~50).
	delta := runtime.NumGoroutine() - baselineGoroutines
	if delta > 20 || delta < -20 {
		t.Errorf("goroutine delta after storm: %d (baseline=%d, now=%d)",
			delta, baselineGoroutines, runtime.NumGoroutine())
	}
}

// TestIntegrationSignalDispatch — Plan B-04 success criterion #4:
// InputSignal "SIGINT" delivers C-c to the pane and the foreground
// process's INT trap fires. Uses a sh trap to print a unique sentinel
// when the signal is received so the test can observe the dispatch
// path through StreamSession.
func TestIntegrationSignalDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationDeadline)
	defer cancel()

	// The pane runs sh with an INT trap. The trap prints
	// INT-RECEIVED so the test has a deterministic sentinel to wait
	// on without timing-sensitive observations of the pane's
	// process table.
	script := `trap 'echo INT-RECEIVED; exit 0' INT; while true; do sleep 0.1; done`
	plane := newIntegrationPlane(t, script)

	// Brief wait for trap installation. Without this the SIGINT can
	// arrive before sh registers the handler and the process exits
	// without printing the sentinel.
	time.Sleep(250 * time.Millisecond)

	attachCtx, attachCancel := context.WithCancel(ctx)
	defer attachCancel()
	events, err := plane.StreamSession(attachCtx, plane.sessionID)
	if err != nil {
		t.Fatalf("StreamSession: %v", err)
	}
	drainSnapshot(t, events, "signal-snapshot")

	in := core.SessionInput{Mode: core.InputSignal, Keys: "SIGINT"}
	if err := plane.SendInput(ctx, plane.sessionID, in); err != nil {
		t.Fatalf("SendInput SIGINT: %v", err)
	}
	waitForBytes(t, events, []byte("INT-RECEIVED"), 3*time.Second, "signal-dispatch")
}

// TestIntegrationKeysMode — Plan B-04 acceptance criterion #5:
// InputKeys named-key "Enter" round-trips correctly. The pane runs a
// read loop that echoes "got:[<line>]" for every newline-terminated
// line; we send the literal "ping" THEN the named key "Enter" and
// assert the formatted echo arrives, which proves both the literal
// path AND the named-key path work end-to-end under real tmux.
func TestIntegrationKeysMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationDeadline)
	defer cancel()

	script := `while read line; do echo "got:[$line]"; done`
	plane := newIntegrationPlane(t, script)

	attachCtx, attachCancel := context.WithCancel(ctx)
	defer attachCancel()
	events, err := plane.StreamSession(attachCtx, plane.sessionID)
	if err != nil {
		t.Fatalf("StreamSession: %v", err)
	}
	drainSnapshot(t, events, "keys-snapshot")

	if err := plane.SendInput(ctx, plane.sessionID, core.SessionInput{
		Mode: core.InputLiteral, Keys: "ping",
	}); err != nil {
		t.Fatalf("SendInput literal: %v", err)
	}
	if err := plane.SendInput(ctx, plane.sessionID, core.SessionInput{
		Mode: core.InputKeys, Keys: "Enter",
	}); err != nil {
		t.Fatalf("SendInput Enter: %v", err)
	}

	waitForBytes(t, events, []byte("got:[ping]"), 3*time.Second, "keys-roundtrip")
}

// TestIntegrationResizeNoOp documents the no-op resize contract from
// CONTEXT.md scope §2 with an executable check: ResizeSession returns
// nil and the pane stays alive (CapturePane succeeds after). Container/
// k8s adaptors override with real resize; under tmux the kernel already
// follows the pane geometry server-side so there's nothing to push.
func TestIntegrationResizeNoOp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), integrationDeadline)
	defer cancel()

	plane := newIntegrationPlane(t, "cat")

	if err := plane.ResizeSession(ctx, plane.sessionID, 120, 40); err != nil {
		t.Fatalf("ResizeSession: %v", err)
	}

	// Confirm pane is still alive by capturing it.
	if _, err := plane.backend.Tmux.CapturePane(ctx, plane.paneID, 10); err != nil {
		t.Fatalf("CapturePane after ResizeSession: %v", err)
	}
}
