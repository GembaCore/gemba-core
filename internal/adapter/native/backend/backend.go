// Package backend defines the abstraction the native orchestrator
// adaptor calls to enumerate terminal panes/tabs, spawn new ones,
// inject keystrokes, capture output, and kill. Concrete backends —
// tmux (gm-native.4), iTerm2 + Terminal.app (gm-native.5) — implement
// this interface. The adaptor (internal/adapter/native) is backend-
// agnostic; it selects a backend at construction via Detect or via
// explicit configuration.
package backend

import (
	"context"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Pane is the unit of identity: one tmux pane / iTerm tab / Terminal
// tab per AgentRef. ID must be stable across backend restarts (tmux
// pane id %N survives the server; iTerm session id survives the app).
type Pane struct {
	// ID is backend-opaque but globally unique across panes the
	// backend knows about. For tmux this is the %N format.
	ID string
	// Cwd is the pane's current working directory at the time of
	// enumeration. Panes can cd out so treat this as a best-effort
	// snapshot.
	Cwd string
	// Command is the pane's foreground command (zsh, claude, etc.).
	Command string
	// Title is the backend-visible pane title (user-set via the
	// terminal's title escape or backend-default).
	Title string
	// Pid is the leaf process id. 0 means the backend doesn't expose
	// one (AppleScript backends often don't).
	Pid int
}

// SpawnSpec carries the arguments Backend.SpawnPane needs to open a
// new pane. Env is applied before the shell starts (not send-keys
// export), so `gemba-bridge` can read GEMBA_SESSION_ID / GEMBA_AGENT_TYPE
// without racing the first command.
type SpawnSpec struct {
	// Cwd is the directory to launch in. Required (relative paths are
	// an adaptor-side bug; backends treat empty as $HOME as a safety
	// net but log a warning).
	Cwd string
	// Env is the environment prepended to the spawned shell. Backends
	// that can't pass env directly (Terminal.app AppleScript) simulate
	// it by writing "export K=V" lines before Command.
	Env map[string]string
	// Command is the program + args to exec in the new pane. Examples:
	// []string{"claude"}, []string{"bash"}, []string{"zsh", "-l"}.
	Command []string
	// Title is an optional initial pane title.
	Title string
}

// Backend is the shell multiplexer facade. Every method takes a
// context so the adaptor can cancel a slow command (osascript can
// take seconds under load).
type Backend interface {
	// Name returns the backend identifier ("tmux", "iterm", "terminal")
	// so the adaptor can surface it in logs and manifest provider
	// metadata.
	Name() string

	// ListPanes returns every pane/tab the backend can see. Order is
	// not guaranteed; callers that need determinism sort by Pane.ID.
	ListPanes(ctx context.Context) ([]Pane, error)

	// SpawnPane creates a new pane per spec and returns its Pane. The
	// returned ID is what SendKeys / CapturePane / Kill consume later.
	SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error)

	// SendKeys writes keys into the pane as if the user typed them.
	// The literal string "Enter" at end-of-input is translated by the
	// backend into a real newline (tmux convention); callers that want
	// to send the literal 4-char sequence "Enter" must split the send.
	SendKeys(ctx context.Context, paneID string, keys string) error

	// CapturePane returns the last `lines` rendered lines of pane
	// output. Implementations strip ANSI if they can cheaply; callers
	// should not rely on that.
	CapturePane(ctx context.Context, paneID string, lines int) (string, error)

	// Kill force-closes the pane. Graceful shutdown is the adaptor's
	// responsibility (send /exit, wait, then Kill) — this is the
	// backstop.
	Kill(ctx context.Context, paneID string) error
}

// ErrUnsupported is the canonical error for "this backend can't do X
// at all" (e.g. Pid is always 0 on Terminal.app; capturing transcript
// on a bare Terminal.app tab requires logging). Callers branch on
// core.KindUnsupported.
func ErrUnsupported(backend, method string) error {
	return core.NewAdaptorError(core.KindUnsupported,
		"native/backend/%s: %s unsupported", backend, method)
}
