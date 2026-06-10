// Package backend: streamable.go declares the optional Streamable
// extension every backend that can server-side fan-out pane bytes to a
// named pipe satisfies. Mirrors the Pausable pattern in backend.go —
// callers (the adapter's sessionIOHub, plan B-03) type-assert to
// Streamable at the point of use rather than extending the base
// Backend interface. gm-v01.3.4.
//
// Today only the tmux backend implements Streamable (see
// tmux_stream.go). Docker / SSH / AppleScript backends will satisfy it
// in later phases via their own runtime-appropriate fan-out (docker
// attach -> stdout pipe, ssh ControlMaster channel, etc.). Until then
// callers branch on the type-assertion and surface KindUnsupported.
package backend

import "context"

// Streamable is the per-backend hook the adapter's sessionIOHub
// invokes to (a) open a one-way server-side fan-out from a pane to a
// named pipe, (b) tear it down + remove the pipe file, and (c) grab a
// first-subscriber backscroll snapshot.
//
// Lifecycle contract:
//
//   - StartPipe MUST be idempotent. Calling twice with the same
//     sessionID returns the same fifoPath and does NOT spawn a second
//     server-side writer. The caller (sessionIOHub) owns refcount
//     semantics; the backend owns the "this pane is already piping"
//     state.
//
//   - StopPipe MUST be safe to call when StartPipe was never invoked
//     (no-op, returns nil). The backend MUST remove the FIFO file on
//     successful stop; ENOENT during removal is not an error.
//
//   - SnapshotPane is called by the hub on every Attach (every
//     subscriber gets the snapshot as its first event so the SPA can
//     paint the terminal immediately). MUST preserve ANSI escapes so
//     xterm.js can render colors — tmux backend uses capture-pane -e.
//
// Sessions identifiers passed here are backend-opaque (typically tmux
// pane ids like "%5"); the hub translates the public sessionID -> the
// backend's internal id via a resolver injected by the adapter at
// construction (CONTEXT.md C-21 opacity guard).
type Streamable interface {
	// StartPipe opens a server-side fan-out from the backing pane to
	// a named pipe under $TMPDIR and returns the path. Idempotent.
	// The caller is responsible for opening the FIFO for read; this
	// method only arranges that data starts flowing into it.
	StartPipe(ctx context.Context, sessionID string) (fifoPath string, err error)

	// StopPipe tears the server-side fan-out down and removes the
	// FIFO file. Safe to call when StartPipe was never invoked.
	StopPipe(ctx context.Context, sessionID string) error

	// SnapshotPane returns up to `lines` of backscroll for a first-
	// subscriber snapshot event. Implementations preserve ANSI escape
	// sequences so the SPA can render colors. lines <= 0 means use a
	// backend-defined default.
	SnapshotPane(ctx context.Context, sessionID string, lines int) ([]byte, error)
}
