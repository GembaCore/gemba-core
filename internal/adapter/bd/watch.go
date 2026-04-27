package bd

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/MikeBengtson/gemba/core"
	"github.com/fsnotify/fsnotify"
)

// gm-e6.6 — out-of-process change propagation. The bd adaptor's
// Subscribe surface (workplane.go) historically only fanned events
// from in-process mutations; a `bd update` from another terminal,
// from CI, or from a polecat's own shell never reached the SPA. This
// file wires fsnotify on `<beadsDir>/.beads` to a debounced poll of
// `bd list --json --updated-after <high-water-mark>`. Each changed
// bead re-flows through the same emitter the in-process path uses,
// so SSE subscribers can't tell the two origins apart (gm-e4.3.1 +
// gm-e4.3.2 + this slice form the full picture).
//
// Coalescing: any number of fsnotify events arriving within a 100ms
// window collapse to one `bd list` poll. The list endpoint itself
// dedupes per WorkItem (one row per bead in the result), so the
// downstream emitter sees at most one event per bead per window.

const (
	// watchDebounce is the burst-collapsing window. Multiple writes to
	// .beads/ within this window combine into one poll. Bead's DoD
	// asks for "one update per WorkItem per 100ms" — the list call's
	// per-bead dedup gives us that for free; the timer keeps us from
	// hammering bd during a checkpoint or merge.
	watchDebounce = 100 * time.Millisecond

	// watchPollFloor is the maximum delay between polls when fsnotify
	// is missing or unsupported (file moves on macOS sometimes lose
	// events; non-local filesystems don't propagate at all). The
	// fallback path ticks at this rate so the SPA's <500ms freshness
	// budget still holds even when fsnotify lies.
	watchPollFloor = 250 * time.Millisecond
)

// startBeadsWatcher launches the fsnotify+poll loop for one WorkPlane.
// Idempotent — multiple calls reuse the same watcher (sync.Once on the
// caller side). Returns a stop closure that the WorkPlane invokes from
// its own shutdown path. When dir is empty (test-mode WorkPlanes built
// via NewWorkPlaneWithRunner) the watcher is a no-op so test suites
// don't hit a missing directory.
func (w *WorkPlane) startBeadsWatcher() {
	w.watcherOnce.Do(func() {
		dir := w.beadsDir
		if dir == "" {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		w.watcherStop = cancel
		go w.runWatcher(ctx, filepath.Join(dir, ".beads"))
	})
}

// runWatcher is the goroutine body. It hydrates a high-water-mark to
// time.Now (so historical mutations don't replay), spins up fsnotify
// on the .beads dir, and on each debounced tick polls `bd list
// --json --updated-after <hwm>` to find changes since the last poll.
//
// Errors during fsnotify setup degrade to a plain ticker so the
// adaptor still surfaces out-of-process changes, just with the
// floor-rate latency rather than the snappier event-driven path.
func (w *WorkPlane) runWatcher(ctx context.Context, beadsPath string) {
	hwm := time.Now().UTC()
	tick := make(chan struct{}, 1)

	// Best-effort fsnotify. Filesystems that don't support inotify
	// (NFS, fuse mounts) bounce back the same path silently — we keep
	// the watcher alive but rely on the floor ticker.
	fs, err := fsnotify.NewWatcher()
	if err == nil {
		defer func() { _ = fs.Close() }()
		if err := fs.Add(beadsPath); err == nil {
			go w.runFsnotifyLoop(ctx, fs, tick)
		}
	}

	// Floor ticker: even if fsnotify works, we still poll at this
	// cadence as belt-and-braces. Cheap — a single SQL-backed
	// `bd list --updated-after` call. Dropped events from fsnotify
	// can't strand the SPA more than watchPollFloor seconds.
	ticker := time.NewTicker(watchPollFloor)
	defer ticker.Stop()

	// Coalesce timer: nil when no poll is pending; non-nil while a
	// 100ms window is in flight. Re-using a single timer instead of
	// creating one per event keeps allocation off the hot path.
	var debounce *time.Timer
	deboundCh := make(chan struct{}, 1)
	scheduleDebounce := func() {
		if debounce != nil {
			return
		}
		debounce = time.AfterFunc(watchDebounce, func() {
			select {
			case deboundCh <- struct{}{}:
			default:
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case <-tick:
			scheduleDebounce()
		case <-deboundCh:
			debounce = nil
			hwm = w.pollAndEmit(ctx, hwm)
		case <-ticker.C:
			hwm = w.pollAndEmit(ctx, hwm)
		}
	}
}

// runFsnotifyLoop translates fsnotify events into ticks on the shared
// channel. Filters out chmod-only events (which fsnotify emits on file
// stat changes that don't carry data) so we don't spam the debounce
// timer while another process scans .beads/.
func (w *WorkPlane) runFsnotifyLoop(ctx context.Context, fs *fsnotify.Watcher, tick chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-fs.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			select {
			case tick <- struct{}{}:
			default:
				// debounce is already scheduled; drop the duplicate
			}
		case <-fs.Errors:
			// Best-effort. The floor ticker covers us.
		}
	}
}

// pollAndEmit lists beads updated since hwm, re-reads each through
// `bd show`, and publishes the result through the same emitter the
// in-process path uses. Returns the new high-water-mark — max of the
// observed UpdatedAt values, or hwm unchanged when nothing moved.
func (w *WorkPlane) pollAndEmit(ctx context.Context, hwm time.Time) time.Time {
	args := []string{
		"list", "--json",
		"--updated-after", hwm.Format(time.RFC3339),
	}
	out, err := w.run(ctx, args...)
	if err != nil {
		// Transient bd errors (Dolt server hiccup, lock contention)
		// keep us at the same hwm so the next poll retries the same
		// window. Worth logging at higher verbosity once we have a
		// logger handle on the adaptor; for now silent retry matches
		// the pattern Subscribe already establishes (gm-e4.3.1).
		return hwm
	}
	var beads []Bead
	if err := json.Unmarshal(out, &beads); err != nil {
		return hwm
	}
	if len(beads) == 0 {
		return hwm
	}
	maxAt := hwm
	for i := range beads {
		wi := beads[i].toWorkItem(w.prefix, w.repos)
		// `bd list --updated-after` is inclusive on the boundary in
		// some bd builds, so a hwm-equal row would replay every poll.
		// Drop strict-equals to avoid the spam; the fallback ticker
		// guarantees we'll catch the row on its next mutation.
		if !wi.UpdatedAt.After(hwm) {
			continue
		}
		kind := classifyKind(wi)
		w.emitWithSource(kind, wi, "fsnotify")
		if wi.UpdatedAt.After(maxAt) {
			maxAt = wi.UpdatedAt
		}
	}
	return maxAt
}

// classifyKind picks the canonical WorkPlaneEvent kind for a re-read.
// Mirrors the rule UpdateWorkItem and NotifyExternal already use:
// terminal state_category → workitem_closed; everything else →
// workitem_updated. workitem_created is reserved for the in-process
// path that knows the row is brand-new; the watcher cannot
// distinguish a fresh insert from an update without persisting a
// per-id "first-seen" marker, and the SPA's invalidation routes both
// kinds to the same query key, so the conservative choice is updated
// over created.
func classifyKind(wi core.WorkItem) string {
	if wi.StateCategory == core.StateCompleted || wi.StateCategory == core.StateCanceled {
		return core.WorkItemEventClosed
	}
	return core.WorkItemEventUpdated
}

// stopBeadsWatcher tears the watcher down. Safe to call multiple
// times; safe to call when the watcher never started.
func (w *WorkPlane) stopBeadsWatcher() {
	if w.watcherStop != nil {
		w.watcherStop()
	}
}
