// Package backend defines the abstraction the native orchestrator
// adaptor calls to enumerate sessions, spawn new ones, inject
// keystrokes, capture output, and kill. Concrete backends — tmux
// (gm-native.4), iTerm2 + Terminal.app (gm-native.5), and Docker
// (gm-root.15.3) — implement this interface. The adaptor
// (internal/adapter/native) is backend-agnostic; it selects a backend
// at construction via Detect or via explicit configuration.
//
// Session kinds. A session may be a tmux pane, a macOS terminal tab,
// a local Docker container, or a remote SSH shell. Callers interact
// with them through the same verbs — the Kind field lets the SPA and
// logs distinguish without branching logic in the adaptor itself.
// See docs/design/containerized-sessions.md §2.
package backend

import (
	"context"

	"github.com/MikeBengtson/gemba/core"
)

// Session is the unit of identity: one tmux pane / iTerm tab /
// Terminal tab / container / ssh host per AgentRef. ID must be stable
// across backend restarts (tmux pane id %N survives the server;
// iTerm session id survives the app; container id survives the docker
// daemon).
//
// Pane is a type alias for Session kept for one release so adaptor
// code that still speaks in tmux vocabulary compiles unchanged while
// the container backend lands (gm-root.15.2). New code should use
// Session.
type Session struct {
	// Kind classifies the execution surface — tmux pane, docker
	// container, remote SSH, etc. Set by the producing backend;
	// consumers (SPA, logs) render a badge keyed off this. The value
	// matches the backend selection Kind (detect.go) one-to-one, so
	// a Docker backend always produces Kind=KindContainer sessions.
	Kind Kind
	// ID is backend-opaque but globally unique across sessions the
	// backend knows about. For tmux this is the %N format; for Docker
	// the full container id; for SSH a <user>@<host>:<remote-pid>
	// triple.
	ID string
	// Cwd is the session's current working directory at the time of
	// enumeration. Sessions can cd out so treat this as a best-effort
	// snapshot. For containers this is the cwd inside the container.
	Cwd string
	// Command is the session's foreground command (zsh, claude, etc.).
	Command string
	// Title is the backend-visible session title.
	Title string
	// Pid is the leaf process id on the host running the session.
	// 0 means the backend doesn't expose one (AppleScript backends
	// often don't; remote-container backends can't).
	Pid int
	// Image is the container image reference. Populated only for
	// KindContainer / KindSSH sessions; empty for tmux/iterm/terminal.
	Image string
	// Labels carries backend-specific metadata (e.g. docker labels
	// including gemba.session, gemba.agent). Empty map is valid.
	Labels map[string]string
}

// Pane is the legacy name for Session. Kept as a type alias so the
// native adaptor and its tests compile unchanged during the backend
// refactor (gm-root.15.2). Remove after the Docker backend and the
// SPA surfacing beads land.
type Pane = Session

// Mount describes a bind-mount or tmpfs injected into a containerized
// session. Ignored by non-container backends.
type Mount struct {
	// Src is the host path for bind-mounts, empty for tmpfs.
	Src string
	// Dst is the destination path inside the container.
	Dst string
	// Mode is "rw", "ro", or "tmpfs". Empty defaults to "rw".
	Mode string
	// Size is the tmpfs size hint (e.g. "128m"). Ignored for
	// non-tmpfs mounts.
	Size string
}

// NetworkPolicy declares how a containerized session reaches the
// network. See docs/design/containerized-sessions.md §5.
type NetworkPolicy struct {
	// Mode: "none" (default), "bridge:<name>", or "host".
	Mode string
}

// Limits caps a containerized session's resource consumption. All
// fields are optional; zero means "no limit" (backends should prefer
// a sensible default rather than actually unlimited — see
// docs/design/containerized-sessions.md §4).
type Limits struct {
	// CPUs is the fractional CPU count (e.g. 2.0 for two cores).
	CPUs float64
	// Memory is the memory cap in bytes. 0 means the backend's default.
	Memory int64
	// PidsLimit caps the number of processes.
	PidsLimit int64
}

// SpawnSpec carries the arguments Backend.SpawnPane needs to open a
// new session. Env is applied before the shell / entrypoint starts,
// so `gemba-bridge` can read GEMBA_SESSION_ID / GEMBA_AGENT_TYPE
// without racing the first command.
//
// Container-specific fields (Image, Mounts, Network, Secrets, Limits,
// ReadOnlyRootfs, UserNS, Labels) are silently ignored by terminal
// backends. Config validation upstream rejects combinations that
// don't match the selected backend.
type SpawnSpec struct {
	// Cwd is the directory to launch in. Required (relative paths are
	// an adaptor-side bug; backends treat empty as $HOME as a safety
	// net but log a warning). For containers this is the cwd inside
	// the container.
	Cwd string
	// Env is the environment prepended to the spawned shell. Backends
	// that can't pass env directly (Terminal.app AppleScript) simulate
	// it by writing "export K=V" lines before Command. Never carry
	// secrets here — use Secrets instead.
	Env map[string]string
	// Command is the program + args to exec in the new session.
	// Examples: []string{"claude"}, []string{"bash"}, []string{"zsh",
	// "-l"}. For containers this overrides the image entrypoint /
	// cmd.
	Command []string
	// Title is an optional initial session title.
	Title string

	// --- container-specific fields (gm-root.15.3) --------------------

	// Image is the container image reference for container backends.
	// Ignored by terminal backends; required by the Docker backend.
	Image string
	// Mounts declares bind-mounts and tmpfs into the container.
	Mounts []Mount
	// Network declares the session's egress policy. Container
	// backends default to Mode="none" when the field is zero-value.
	Network NetworkPolicy
	// Secrets is the list of named secrets the backend must resolve
	// and mount into the session at /run/secrets/<name>. Never pass
	// secret material via Env.
	Secrets []string
	// Limits caps CPU, memory, and PID count.
	Limits Limits
	// ReadOnlyRootfs requests a read-only root filesystem with a
	// writable tmpfs at /tmp. Container backends default to true;
	// terminal backends ignore it.
	ReadOnlyRootfs bool
	// UserNS requests the backend drop into a non-root user matching
	// the workspace owner on the host. Leave empty for the backend
	// default.
	UserNS string
	// Labels are backend-specific key/value tags applied to the
	// session (docker labels, ssh session metadata). The orchestration
	// plane adaptor stamps gemba.session and gemba.agent here.
	Labels map[string]string
}

// Backend is the shell multiplexer facade. Every method takes a
// context so the adaptor can cancel a slow command (osascript can
// take seconds under load; docker can block on daemon I/O).
//
// ListPanes / SpawnPane / SendKeys / CapturePane / Kill form the
// minimum required surface. Backends that support pause/resume
// additionally satisfy Pausable (gm-root.15.12); callers type-assert
// to that interface at the point of use rather than extending the
// base.
type Backend interface {
	// Name returns the backend identifier ("tmux", "iterm",
	// "terminal", "docker", "ssh") so the adaptor can surface it in
	// logs and manifest provider metadata.
	Name() string

	// ListPanes returns every session the backend can see. Order is
	// not guaranteed; callers that need determinism sort by
	// Session.ID. (Name preserved for backward compat; the returned
	// type is Session since Pane = Session.)
	ListPanes(ctx context.Context) ([]Pane, error)

	// SpawnPane creates a new session per spec and returns its
	// Session. The returned ID is what SendKeys / CapturePane / Kill
	// consume later.
	SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error)

	// SendKeys writes keys into the session as if the user typed
	// them. The literal string "Enter" at end-of-input is translated
	// by the backend into a real newline (tmux convention); callers
	// that want to send the literal 4-char sequence "Enter" must
	// split the send.
	SendKeys(ctx context.Context, sessionID string, keys string) error

	// CapturePane returns the last `lines` rendered lines of output.
	// Implementations strip ANSI if they can cheaply; callers should
	// not rely on that. For container backends this tails docker
	// logs.
	CapturePane(ctx context.Context, sessionID string, lines int) (string, error)

	// Kill force-closes the session. Graceful shutdown is the
	// adaptor's responsibility (send /exit, wait, then Kill) — this
	// is the backstop.
	Kill(ctx context.Context, sessionID string) error
}

// Pausable is an optional extension satisfied by backends whose
// underlying surface supports pause/unpause semantics (containers).
// Tmux / iTerm / Terminal backends do not satisfy it. The
// OrchestrationPlane's PauseSession / ResumeSession methods
// type-assert to Pausable and return core.KindUnsupported when the
// active backend is terminal-only. See
// docs/design/containerized-sessions.md §8.
type Pausable interface {
	// Pause suspends the session's processes. For Docker this is
	// `docker pause`; the container keeps its state in memory.
	Pause(ctx context.Context, sessionID string) error
	// Unpause resumes a paused session.
	Unpause(ctx context.Context, sessionID string) error
}

// ErrUnsupported is the canonical error for "this backend can't do X
// at all" (e.g. Pid is always 0 on Terminal.app; Pause is unsupported
// on tmux). Callers branch on core.KindUnsupported.
func ErrUnsupported(backend, method string) error {
	return core.NewAdaptorError(core.KindUnsupported,
		"native/backend/%s: %s unsupported", backend, method)
}
