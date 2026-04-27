package native

import (
	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
	"github.com/MikeBengtson/gemba/internal/adapter/native/bridge"
	"github.com/MikeBengtson/gemba/core"
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
}
