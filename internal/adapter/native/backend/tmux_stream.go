// Package backend: tmux_stream.go is the tmux satisfaction of the
// Streamable optional extension (gm-v01.3.4). Three primitives:
//
//   - StartPipe(paneID)        -> open `tmux pipe-pane -o` writing to a
//                                 FIFO under $TMPDIR; mkfifo first.
//   - StopPipe(paneID)         -> toggle the pipe off (`pipe-pane` with
//                                 no shell command) + os.Remove the
//                                 FIFO.
//   - SnapshotPane(paneID, N)  -> `capture-pane -p -e -J -t paneID
//                                 -S -N` returning the raw bytes with
//                                 ANSI escapes preserved.
//
// The hub (internal/adapter/native/iohub.go) owns refcount + FIFO open-
// for-read; this file owns the tmux-side argv shape and the in-process
// activePipes table that makes StartPipe idempotent.
package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/GembaCore/gemba-core/core"
)

// Compile-time assertion: *Tmux satisfies Streamable. Removing this
// line breaks tmux_stream_test.go::TestStreamable_TmuxSatisfiesInterface.
var _ Streamable = (*Tmux)(nil)

// activePipes lives on *Tmux (declared in tmux.go's struct extension
// below) — make sure to extend the struct via the shadow declaration
// below when refactoring; Go does not allow a struct definition to be
// spread across multiple files for the same type, so we instead expose
// helpers that lazily initialize the map.

// pipePaneState lazily initializes the activePipes map under pipeMu so
// older NewTmux constructors that pre-date this file still work.
func (t *Tmux) pipePaneState() (map[string]struct{}, *sync.Mutex) {
	t.pipeMu.Lock()
	if t.activePipes == nil {
		t.activePipes = make(map[string]struct{})
	}
	t.pipeMu.Unlock()
	return t.activePipes, &t.pipeMu
}

// fifoPath returns the canonical FIFO path for a given pane id.
func fifoPath(paneID string) string {
	return filepath.Join(os.TempDir(), "gemba-stream-"+safeFIFOName(paneID)+".fifo")
}

// safeFIFOName strips the tmux `%` prefix from pane ids and replaces
// any character outside [a-zA-Z0-9_-] with underscore so the resulting
// filename is safe to interpolate into a shell command line. Empty
// result falls back to "pane" so a missing or all-prohibited id still
// produces a usable filename.
func safeFIFOName(id string) string {
	// Strip a single leading % (tmux pane id convention).
	id = strings.TrimPrefix(id, "%")
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "pane"
	}
	return b.String()
}

// StartPipe implements Streamable. Idempotent: a second invocation for
// the same paneID returns the existing FIFO path and does NOT shell to
// tmux a second time. ENOTSUP / unsupported FS from mkfifo wraps to
// KindAdaptorDegraded — callers (the hub) surface that to the SPA as a
// typed banner rather than panicking.
func (t *Tmux) StartPipe(ctx context.Context, paneID string) (string, error) {
	if paneID == "" {
		return "", core.NewAdaptorError(core.KindValidation,
			"native/backend/tmux: StartPipe requires pane id")
	}
	path := fifoPath(paneID)

	active, mu := t.pipePaneState()
	mu.Lock()
	if _, ok := active[paneID]; ok {
		mu.Unlock()
		return path, nil
	}
	// Hold the lock across mkfifo + tmux shell so concurrent callers
	// see exactly one open. Releases before returning.
	defer mu.Unlock()

	// Create the FIFO. ENOENT on the parent is improbable ($TMPDIR
	// always exists) but treat it as a degraded condition rather than a
	// hard error so the hub can surface it.
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		// EEXIST: a stale FIFO from a prior process. Remove it and
		// retry once; if THAT fails we surface degraded.
		if errors.Is(err, syscall.EEXIST) {
			if rmErr := os.Remove(path); rmErr != nil {
				return "", core.WrapAdaptorError(core.KindAdaptorDegraded, rmErr,
					"native/backend/tmux: removing stale FIFO at %s", path)
			}
			if err2 := syscall.Mkfifo(path, 0o600); err2 != nil {
				return "", core.WrapAdaptorError(core.KindAdaptorDegraded, err2,
					"native/backend/tmux: mkfifo retry at %s", path)
			}
		} else {
			return "", core.WrapAdaptorError(core.KindAdaptorDegraded, err,
				"native/backend/tmux: mkfifo at %s", path)
		}
	}

	// `tmux pipe-pane -o -t <paneID> 'cat >> <path>'`. -o toggles the
	// pipe ON. The shell command is a single argv element; safeFIFOName
	// guarantees no shell metacharacters can appear in the path.
	shell := "cat >> " + path
	if _, err := t.run(ctx, "pipe-pane", "-o", "-t", paneID, shell); err != nil {
		// Best-effort: remove the FIFO we just created so we don't
		// leak the file. Ignore remove errors here — the original tmux
		// error is the meaningful one.
		_ = os.Remove(path)
		return "", core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"native/backend/tmux: pipe-pane open on %s", paneID)
	}
	active[paneID] = struct{}{}
	return path, nil
}

// StopPipe implements Streamable. Safe to call when StartPipe was
// never invoked (no-op, returns nil). On successful stop the FIFO file
// is removed; ENOENT during removal is silently tolerated.
func (t *Tmux) StopPipe(ctx context.Context, paneID string) error {
	if paneID == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/tmux: StopPipe requires pane id")
	}
	active, mu := t.pipePaneState()
	mu.Lock()
	if _, ok := active[paneID]; !ok {
		mu.Unlock()
		return nil
	}
	delete(active, paneID)
	mu.Unlock()

	// `tmux pipe-pane -t <paneID>` with no command argument toggles
	// the pipe OFF (tmux's documented semantic).
	if _, err := t.run(ctx, "pipe-pane", "-t", paneID); err != nil {
		// Even if tmux returns an error (pane already gone, server
		// dead), still remove the FIFO so we don't leak.
		_ = os.Remove(fifoPath(paneID))
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"native/backend/tmux: pipe-pane close on %s", paneID)
	}
	if err := os.Remove(fifoPath(paneID)); err != nil && !os.IsNotExist(err) {
		return core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"native/backend/tmux: removing FIFO for %s", paneID)
	}
	return nil
}

// SnapshotPane implements Streamable. Wraps tmux's capture-pane with
// -e (preserve escape sequences for xterm.js color) and -J (join
// wrapped lines). lines <= 0 defaults to 200 to match the existing
// CapturePane peek default.
func (t *Tmux) SnapshotPane(ctx context.Context, paneID string, lines int) ([]byte, error) {
	if paneID == "" {
		return nil, core.NewAdaptorError(core.KindValidation,
			"native/backend/tmux: SnapshotPane requires pane id")
	}
	if lines <= 0 {
		lines = 200
	}
	out, err := t.run(ctx, "capture-pane", "-p", "-e", "-J", "-t", paneID, "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"native/backend/tmux: capture-pane on %s", paneID)
	}
	return out, nil
}
