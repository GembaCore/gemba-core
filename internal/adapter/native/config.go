package native

import (
	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
	"github.com/GembaCore/gemba-core/internal/adapter/native/bridge"
)

// Config holds the dependencies the native OrchestrationPlane needs
// to function. Zero values are fine in unit tests; serve.go wires
// real backends, registries, and workplanes via NewWithConfig.
//
// Fields are kept value-typed (not pointer) for most deps because
// they wrap things that don't need lifecycle management. The slice
// types (Backend, WorkPlane) are interfaces so tests can inject
// fakes.
type Config struct {
	// Backend is the terminal multiplexer. Required for StartSession;
	// ListAgents works without it (returns empty).
	Backend backend.Backend
	// Registry is the .gemba/agents.toml parsed registry. Required
	// for StartSession (to resolve binary + hooks profile).
	Registry agents.Registry
	// WorkPlane is optional; when non-nil, the correlator attaches
	// evidence for bd-subcommand invocations detected via bridge
	// PreToolUse frames (gm-native.14).
	WorkPlane core.WorkPlane
	// Fanout is the per-session bridge tail multiplexer. When nil,
	// a new one is created in NewWithConfig.
	Fanout *bridge.Fanout
	// RepoRoot is the repository the worktree provisioner uses; empty
	// means "discover from cwd."
	RepoRoot string
	// WorktreesDir overrides the default "../worktrees" sibling.
	WorktreesDir string

	// Reaper tunes the idle-pane reaper goroutine (gm-s47n.11,
	// session-pool.md §4.4). Zero values get spec defaults
	// (30-minute idle ceiling, 1-minute tick). DisableReaper turns
	// the goroutine off entirely — used by unit tests that want
	// deterministic lifecycles without a background ticker.
	Reaper        ReaperConfig
	DisableReaper bool

	// Reconcile tunes the §9 reconcile loop. Off by default unless
	// WorkPlane is non-nil (the loop reads bead status to detect
	// the "agent crashed mid-bead-done" failure mode).
	Reconcile        ReconcileConfig
	DisableReconcile bool
}
