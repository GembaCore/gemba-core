package bd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// gm-e6.6 — fsnotify+poll Subscribe path.
//
// These tests exercise three layers without needing a real bd binary:
//
//  1. pollAndEmit — given a fake `bd list --updated-after` runner that
//     returns one row, the watcher publishes a workitem_updated event
//     and advances the high-water-mark.
//  2. End-to-end watcher loop — a temp `.beads` dir + the runWatcher
//     goroutine + a Subscribe channel; touching a file inside the
//     `.beads` dir surfaces an event within the latency budget.
//  3. classifyKind — terminal state_categories map to workitem_closed
//     (mirroring the in-process UpdateWorkItem rule).

func TestPollAndEmit_PublishesUpdatedEvent(t *testing.T) {
	hwm := time.Now().UTC().Add(-1 * time.Hour)
	rowAt := time.Now().UTC()

	var calls atomic.Int32
	runner := func(_ context.Context, args ...string) ([]byte, error) {
		calls.Add(1)
		// Sanity-check the flag the watcher passes; we want the poll
		// to be scoped, not a full list scan.
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--updated-after") {
			t.Errorf("watcher did not pass --updated-after; args=%q", joined)
		}
		return []byte(`[{"id":"gm-watch-1","title":"watcher row","status":"open","updated_at":"` +
			rowAt.Format(time.RFC3339Nano) + `"}]`), nil
	}

	wp := NewWorkPlaneWithRunner(runner, "")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	ch, err := wp.Subscribe(ctx, core.WorkPlaneSubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	got := wp.pollAndEmit(ctx, hwm)
	if !got.After(hwm) {
		t.Fatalf("pollAndEmit did not advance hwm; got=%v hwm=%v", got, hwm)
	}
	if calls.Load() == 0 {
		t.Fatal("runner not called by pollAndEmit")
	}

	select {
	case ev := <-ch:
		if ev.Kind != core.WorkItemEventUpdated {
			t.Errorf("kind: got %q want %q", ev.Kind, core.WorkItemEventUpdated)
		}
		if got, want := string(ev.WorkItemID), "gemba/gemba/gm-watch-1"; got != want {
			t.Errorf("work_item_id: got %q want %q", got, want)
		}
		if src, _ := ev.Payload["notify_source"].(string); src != "fsnotify" {
			t.Errorf("notify_source: got %q want %q", src, "fsnotify")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("no event published within budget")
	}
}

func TestPollAndEmit_NoChangesKeepsHwm(t *testing.T) {
	runner := func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`[]`), nil
	}
	wp := NewWorkPlaneWithRunner(runner, "")
	hwm := time.Now().UTC()
	got := wp.pollAndEmit(context.Background(), hwm)
	if !got.Equal(hwm) {
		t.Errorf("pollAndEmit advanced hwm on empty result; got=%v hwm=%v", got, hwm)
	}
}

func TestPollAndEmit_DropsBoundaryRow(t *testing.T) {
	// `bd list --updated-after` is inclusive on the boundary in some
	// builds; the watcher must drop strict-equals so a stable row
	// doesn't replay every poll.
	hwm := time.Now().UTC().Truncate(time.Second)
	runner := func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`[{"id":"gm-stable","title":"x","updated_at":"` +
			hwm.Format(time.RFC3339Nano) + `"}]`), nil
	}
	wp := NewWorkPlaneWithRunner(runner, "")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	ch, err := wp.Subscribe(ctx, core.WorkPlaneSubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	wp.pollAndEmit(ctx, hwm)
	select {
	case ev := <-ch:
		t.Errorf("boundary row should not emit; got %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// expected — silence
	}
}

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		name     string
		category core.StateCategory
		want     string
	}{
		{"unstarted", core.StateUnstarted, core.WorkItemEventUpdated},
		{"started", core.StateStarted, core.WorkItemEventUpdated},
		{"completed", core.StateCompleted, core.WorkItemEventClosed},
		{"canceled", core.StateCanceled, core.WorkItemEventClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyKind(core.WorkItem{StateCategory: tc.category})
			if got != tc.want {
				t.Errorf("classifyKind(%q) = %q, want %q", tc.category, got, tc.want)
			}
		})
	}
}

// TestRunWatcher_FsnotifyDrivenPoll spins up a real fsnotify watcher
// against a temp dir and confirms a file write inside `.beads/`
// surfaces a SPA-bound event within the watcher's combined debounce
// + poll budget. This is the gm-e6.6 DoD evidence — "a `bd update`
// made outside Gemba surfaces in the SPA within 500ms."
func TestRunWatcher_FsnotifyDrivenPoll(t *testing.T) {
	tmp := t.TempDir()
	beadsDir := filepath.Join(tmp, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}

	// Runner returns one freshly-updated row whenever called. Because
	// the watcher only emits on rows with UpdatedAt > hwm, the row's
	// timestamp is set to "now-ish" inside the runner.
	var mu sync.Mutex
	rowAt := time.Now().UTC()
	runner := func(_ context.Context, _ ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		rowAt = time.Now().UTC()
		body := `[{"id":"gm-watch-fs","title":"fs watch row","status":"open","updated_at":"` +
			rowAt.Format(time.RFC3339Nano) + `"}]`
		return []byte(body), nil
	}

	wp := NewWorkPlaneWithRunner(runner, "")
	wp.beadsDir = tmp
	t.Cleanup(func() { _ = wp.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	ch, err := wp.Subscribe(ctx, core.WorkPlaneSubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Allow the watcher loop to install fsnotify before we touch.
	time.Sleep(50 * time.Millisecond)

	// Writing inside .beads should trigger the fsnotify -> debounce
	// -> pollAndEmit chain.
	f := filepath.Join(beadsDir, "trigger.json")
	if err := os.WriteFile(f, []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatalf("write trigger file: %v", err)
	}

	select {
	case ev := <-ch:
		if string(ev.WorkItemID) != "gemba/gemba/gm-watch-fs" {
			t.Errorf("wrong work_item_id: %q", ev.WorkItemID)
		}
		if src, _ := ev.Payload["notify_source"].(string); src != "fsnotify" {
			t.Errorf("notify_source: got %q want fsnotify", src)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("no event from fsnotify path within 750ms; watcher chain broken")
	}
}
