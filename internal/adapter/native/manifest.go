package native

import "github.com/MikeBengtson/gemba/core"

// AdaptorID is the stable identifier surfaced in CapabilityManifest.
// Kept short + lowercase so CLI flags (--orchestration=native) and SPA
// strings match.
const AdaptorID = "native"

// AdaptorVersion tracks the adaptor's own semver independently of the
// protocol version. Bumped when a wire-visible contract changes.
const AdaptorVersion = "0.1.0"

// Manifest is the capability manifest the native adaptor publishes.
// Transport is API because the adaptor runs in-process alongside the
// server — no JSONL/MCP transport wrapping.
//
// WorkspaceKinds:
//   - worktree: a git worktree Gemba auto-provisions for a bead.
//   - exec:     an operator-pre-existing pane whose cwd the adaptor
//     trusts ($PWD, no isolation).
//
// GroupModes is static for MVP (every pane is its own "group of one").
// AssignmentStrategy is push because the SPA dispatches to named
// sessions; pull-style claim loops are a Gas Town concept that doesn't
// map onto direct-to-shell dispatch.
//
// EventDelivery is push because the adaptor owns its event channel
// and fans out through the server's existing SSE hub.
//
// EscalationKinds covers the two surfaces Claude Code exposes today
// via its hook system: permission prompts (tool gating) and HITL
// approvals (elicitation / notification). MCP elicitation arrives via
// the same Notification hook, so it falls under PermissionPrompt on
// the wire.
//
// CostAxes is wallclock-only for MVP; token metering is deferred to
// gm-native.17.
var Manifest = core.OrchestrationCapabilityManifest{
	AdaptorID:               AdaptorID,
	AdaptorVersion:          AdaptorVersion,
	OrchestrationAPIVersion: core.ProtocolVersion,
	Transport:               core.TransportAPI,
	WorkspaceKinds: []core.WorkspaceKind{
		core.WorkspaceWorktree,
		core.WorkspaceExec,
	},
	DefaultWorkspaceKind: core.WorkspaceWorktree,
	PerKindIsolation: map[core.WorkspaceKind]core.IsolationCapabilities{
		core.WorkspaceWorktree: {FSScoped: true},
		core.WorkspaceExec:     {},
	},
	GroupModes:           []core.GroupMode{core.GroupStatic},
	AssignmentStrategies: []core.AssignmentStrategy{core.StrategyPush},
	CostAxes:             []core.CostAxis{core.CostWallclock},
	EscalationKinds: []core.EscalationKind{
		core.EscalationPermissionPrompt,
		core.EscalationHITLApproval,
	},
	PeekModes: []core.PeekMode{
		core.PeekTranscript,
		core.PeekStructured,
	},
	EventDelivery: core.EventDeliveryPush,
}
