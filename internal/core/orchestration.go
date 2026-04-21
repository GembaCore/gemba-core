// Package core: see doc.go for the overview.
package core

import (
	"context"
	"time"
)

// Transport, TransportAPI, TransportJSONL, TransportMCP are declared in
// workplane.go (gm-e3.2). The OrchestrationPlane reuses the same enum so
// that an adaptor pair (WorkPlane + OrchestrationPlane) running on the
// same wire shares one canonical transport vocabulary.

// WorkspaceKind enumerates the isolation shapes an OrchestrationPlane
// can acquire for an assignment. Per gm-root DD-5, every workspace MUST
// guarantee `fs_scoped`; richer isolation (network, CPU, memory,
// snapshot/restore) is declared per-kind on the manifest.
type WorkspaceKind string

const (
	// WorkspaceWorktree — a git worktree on the host (Gas Town polecats).
	WorkspaceWorktree WorkspaceKind = "worktree"
	// WorkspaceContainer — an OCI container (Docker/Podman).
	WorkspaceContainer WorkspaceKind = "container"
	// WorkspaceK8sPod — a Kubernetes pod.
	WorkspaceK8sPod WorkspaceKind = "k8s_pod"
	// WorkspaceVM — a full virtual machine.
	WorkspaceVM WorkspaceKind = "vm"
	// WorkspaceExec — unscoped exec; relies on the agent honouring $PWD
	// (LangGraph default; documents its weakness).
	WorkspaceExec WorkspaceKind = "exec"
	// WorkspaceSubprocess — a spawned child process with a scoped cwd.
	WorkspaceSubprocess WorkspaceKind = "subprocess"
)

// GroupMode names the three ways an orchestrator may present a group of
// agents. Per gm-root DD-7, every AgentGroup declares exactly one:
//
//   - static — enumerated members (Gas Town convoy, CrewAI crew).
//   - pool   — elastic members behind an opaque check endpoint.
//   - graph  — topology-defined (LangGraph subgraph, ADK hierarchy).
type GroupMode string

const (
	// GroupStatic — membership enumerated at registration.
	GroupStatic GroupMode = "static"
	// GroupPool — membership derived from a live check endpoint.
	GroupPool GroupMode = "pool"
	// GroupGraph — membership derived from a topology the adaptor owns.
	GroupGraph GroupMode = "graph"
)

// CostAxis names a dimension the orchestration adaptor meters against.
// Gemba aggregates into a canonical CostMeter (gm-root DD-4); adaptors
// MUST declare at least one axis and emit samples against it.
type CostAxis string

const (
	// CostTokens — LLM tokens consumed (sum of prompt+completion).
	CostTokens CostAxis = "tokens"
	// CostWallclock — elapsed wall-clock seconds of session runtime.
	CostWallclock CostAxis = "wallclock"
	// CostDollarsNative — adaptor-native cost unit (e.g. Devin ACUs).
	// The manifest carries a configurable conversion rate.
	CostDollarsNative CostAxis = "dollars_native"
)

// EscalationKind identifies a source an adaptor may raise an
// EscalationRequest from (gm-root DD-6). Adaptors declare the subset
// they emit so the UI can wire the right badges, inbox categories, and
// Kanban overlays.
type EscalationKind string

const (
	// EscalationMCPElicitation — MCP server requested structured input.
	EscalationMCPElicitation EscalationKind = "mcp_elicitation"
	// EscalationA2AInputRequired — A2A task transitioned to input-required.
	EscalationA2AInputRequired EscalationKind = "a2a_input_required"
	// EscalationPermissionPrompt — agent tool-use permission request.
	EscalationPermissionPrompt EscalationKind = "permission_prompt"
	// EscalationHITLApproval — human-in-the-loop approval gate.
	EscalationHITLApproval EscalationKind = "hitl_approval"
	// EscalationOrchestratorPause — orchestrator paused autonomously
	// (e.g. conflict detected, auto-resolve failed).
	EscalationOrchestratorPause EscalationKind = "orchestrator_pause"
)

// PeekMode names a live-introspection mode an orchestrator supports for
// running sessions. Gas Town's tmux capture-pane is a `transcript`
// peek; a screenshot UI is `screenshot`; a structured event dump is
// `structured`.
type PeekMode string

const (
	// PeekTranscript — plain-text tail of the session transcript.
	PeekTranscript PeekMode = "transcript"
	// PeekScreenshot — rendered snapshot (image bytes or URL).
	PeekScreenshot PeekMode = "screenshot"
	// PeekStructured — structured event/state dump.
	PeekStructured PeekMode = "structured"
)

// IsolationCapabilities declares what a given workspace kind actually
// enforces. fs_scoped is the single MUST from gm-root DD-5; the rest
// are declared per-kind on the manifest and inspected by the
// negotiation layer when an assignment specifies required isolation.
type IsolationCapabilities struct {
	FSScoped        bool `json:"fs_scoped"`
	NetIsolated     bool `json:"net_isolated"`
	CPULimited      bool `json:"cpu_limited"`
	MemLimited      bool `json:"mem_limited"`
	SnapshotRestore bool `json:"snapshot_restore"`
}

// OrchestrationCapabilityManifest is the describe() payload every
// OrchestrationPlaneAdaptor returns at registration. The capability-
// negotiation UI (gm-e11.4) reads this to hide controls the adaptor
// cannot service; the registry (gm-e3.4) uses it for version matching.
//
// The six bead-required axes — transport, workspace_kinds, group_modes,
// cost_axes, escalation_kinds, peek_modes — are all non-omitempty so
// that a manifest serialised by one adaptor and parsed by the UI can
// never silently default to "all off".
type OrchestrationCapabilityManifest struct {
	AdaptorID               string                                  `json:"adaptor_id"`
	AdaptorVersion          string                                  `json:"adaptor_version"`
	OrchestrationAPIVersion string                                  `json:"orchestration_api_version"`
	Transport               Transport                               `json:"transport"`
	WorkspaceKinds          []WorkspaceKind                         `json:"workspace_kinds"`
	DefaultWorkspaceKind    WorkspaceKind                           `json:"default_workspace_kind,omitempty"`
	PerKindIsolation        map[WorkspaceKind]IsolationCapabilities `json:"per_kind_isolation,omitempty"`
	GroupModes              []GroupMode                             `json:"group_modes"`
	AssignmentStrategies    []AssignmentStrategy                    `json:"assignment_strategies,omitempty"`
	CostAxes                []CostAxis                              `json:"cost_axes"`
	NativeCostUnit          string                                  `json:"native_cost_unit,omitempty"`
	NativeCostToDollars     *float64                                `json:"native_cost_to_dollars,omitempty"`
	EscalationKinds         []EscalationKind                        `json:"escalation_kinds"`
	PeekModes               []PeekMode                              `json:"peek_modes"`
	EventDelivery           EventDelivery                           `json:"event_delivery,omitempty"`
	Extension               map[string]any                          `json:"extension,omitempty"`
}

// AssignmentStrategy names one of the three observed ways an orchestrator
// hands work to an agent (domain.md §3.4). Adaptors declare which they
// support; Gemba's canonical strategy is `pull`, with push and hook as
// adaptor-supported alternatives.
type AssignmentStrategy string

const (
	// StrategyPush — caller pushes work to a named agent.
	StrategyPush AssignmentStrategy = "push"
	// StrategyPull — agent claims from a shared queue (canonical).
	StrategyPull AssignmentStrategy = "pull"
	// StrategyHook — adaptor fires a side-effect on state transition
	// (Gas Town's hook model, GitHub webhook).
	StrategyHook AssignmentStrategy = "hook"
)

// EventDelivery names how an adaptor pushes OrchestrationEvents.
type EventDelivery string

const (
	// EventDeliverySSE — server-sent events stream.
	EventDeliverySSE EventDelivery = "sse"
	// EventDeliveryPush — adaptor POSTs events back to Gemba.
	EventDeliveryPush EventDelivery = "push"
	// EventDeliveryPoll — Gemba polls; adaptor returns deltas.
	EventDeliveryPoll EventDelivery = "poll"
)

// Workspace is Gemba's adaptor-agnostic view of a scoped execution
// environment — a worktree, a container, a k8s pod. Per gm-root DD-5
// the one invariant is that writes within the workspace do not escape
// to corrupt another concurrent assignment's workspace.
type Workspace struct {
	ID               string                `json:"id"`
	Kind             WorkspaceKind         `json:"kind"`
	Repository       string                `json:"repository,omitempty"`
	Branch           string                `json:"branch,omitempty"`
	BaseSHA          string                `json:"base_sha,omitempty"`
	Status           WorkspaceStatus       `json:"status"`
	Isolation        IsolationCapabilities `json:"isolation"`
	ProviderMetadata map[string]any        `json:"provider_metadata,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	ReleasedAt       *time.Time            `json:"released_at,omitempty"`
}

// WorkspaceStatus is the lifecycle tag on a Workspace. "provisioning"
// and "ready" are pre-assignment; "in_use" covers the live session;
// "released" and "error" are terminal.
type WorkspaceStatus string

const (
	WorkspaceProvisioning WorkspaceStatus = "provisioning"
	WorkspaceReady        WorkspaceStatus = "ready"
	WorkspaceInUse        WorkspaceStatus = "in_use"
	WorkspaceReleased     WorkspaceStatus = "released"
	WorkspaceError        WorkspaceStatus = "error"
)

// AgentGroup is the adaptor-agnostic view of a collection of agents
// (gm-root DD-7). The Mode field picks the union arm carried by Members.
type AgentGroup struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"display_name"`
	Mode        GroupMode      `json:"mode"`
	Members     GroupMembers   `json:"members"`
	Repository  []string       `json:"repository,omitempty"`
	Extension   map[string]any `json:"extension,omitempty"`
}

// GroupMembers is the mode-tagged union of member descriptors. Only the
// subset matching Mode is meaningful; the rest MUST be zero.
type GroupMembers struct {
	Static          []AgentID `json:"static,omitempty"`
	PoolCheckURL    string    `json:"pool_check_url,omitempty"`
	PoolMin         *int      `json:"pool_min,omitempty"`
	PoolMax         *int      `json:"pool_max,omitempty"`
	GraphTopologyID string    `json:"graph_topology_id,omitempty"`
	GraphResolved   []AgentID `json:"graph_resolved,omitempty"`
}

// WorkspaceTopology is the payload both DeclaredState and ObservedState
// return. It generalises Gas City's declared (`city.toml`) vs observed
// (`.gc/agents/`) split into a single reusable shape (gm-root §Novel
// §8). The Kanban, Agents dashboard, and capability-negotiation UI all
// diff these two to surface drift.
type WorkspaceTopology struct {
	Agents      []AgentRef   `json:"agents,omitempty"`
	Groups      []AgentGroup `json:"groups,omitempty"`
	Workspaces  []Workspace  `json:"workspaces,omitempty"`
	Assignments []Assignment `json:"assignments,omitempty"`
	Sessions    []Session    `json:"sessions,omitempty"`
	CapturedAt  time.Time    `json:"captured_at"`
}

// Assignment binds an agent to a work item via an optional workspace.
// Cost accumulates here across the assignment's session lifetime; open
// escalations hang off it so the UI can badge cards without walking
// sessions.
type Assignment struct {
	ID          string              `json:"id"`
	WorkItemID  WorkItemID          `json:"work_item_id"`
	AgentID     AgentID             `json:"agent_id"`
	WorkspaceID string              `json:"workspace_id,omitempty"`
	SessionID   string              `json:"session_id,omitempty"`
	Status      AssignmentStatus    `json:"status"`
	StartedAt   *time.Time          `json:"started_at,omitempty"`
	EndedAt     *time.Time          `json:"ended_at,omitempty"`
	Cost        *CostMeter          `json:"cost,omitempty"`
	Escalations []EscalationRequest `json:"escalations,omitempty"`
}

// AssignmentStatus is the lifecycle tag on an Assignment.
type AssignmentStatus string

const (
	AssignmentPending  AssignmentStatus = "pending"
	AssignmentActive   AssignmentStatus = "active"
	AssignmentPaused   AssignmentStatus = "paused"
	AssignmentFinished AssignmentStatus = "finished"
	AssignmentFailed   AssignmentStatus = "failed"
	AssignmentCanceled AssignmentStatus = "canceled"
)

// Session is an ephemeral run of an agent against an assignment (C10 —
// sessions are ephemeral, identities persist). Adaptors append cost
// samples and heartbeats; Gemba aggregates on read.
type Session struct {
	ID               string         `json:"id"`
	AssignmentID     string         `json:"assignment_id"`
	AgentID          AgentID        `json:"agent_id"`
	Status           SessionStatus  `json:"status"`
	StartedAt        time.Time      `json:"started_at"`
	EndedAt          *time.Time     `json:"ended_at,omitempty"`
	LastHeartbeat    *time.Time     `json:"last_heartbeat,omitempty"`
	TranscriptRef    string         `json:"transcript_ref,omitempty"`
	CostSamples      []CostSample   `json:"cost_samples,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// SessionStatus is the lifecycle tag on a Session.
type SessionStatus string

const (
	SessionRunning       SessionStatus = "running"
	SessionInputRequired SessionStatus = "input_required"
	SessionSuspended     SessionStatus = "suspended"
	SessionCompleted     SessionStatus = "completed"
	SessionFailed        SessionStatus = "failed"
)

// SessionEndMode names the terminal intent when EndSession is called.
type SessionEndMode string

const (
	SessionEndCompleted SessionEndMode = "completed"
	SessionEndFailed    SessionEndMode = "failed"
	SessionEndCanceled  SessionEndMode = "canceled"
)

// CostSample is a single metered tick from an adaptor. One of Tokens,
// WallclockSeconds, DollarsNative may be set (or all three); the axis
// key in the manifest's cost_axes[] tells the UI which are authoritative.
type CostSample struct {
	At               time.Time `json:"at"`
	Tokens           *int64    `json:"tokens,omitempty"`
	WallclockSeconds *float64  `json:"wallclock_seconds,omitempty"`
	DollarsNative    *float64  `json:"dollars_native,omitempty"`
	DollarsEst       *float64  `json:"dollars_est,omitempty"`
}

// CostMeter is the aggregated three-axis cost on an Assignment. Gemba
// computes this from CostSamples; adaptors SHOULD NOT populate it
// directly.
type CostMeter struct {
	TokensTotal      int64   `json:"tokens_total"`
	WallclockSeconds float64 `json:"wallclock_seconds"`
	DollarsEst       float64 `json:"dollars_est"`
}

// SessionPeek is the snapshot shape returned by peek_session. The set
// of populated fields is gated by the manifest's peek_modes[]; callers
// MUST tolerate any subset.
type SessionPeek struct {
	SessionID  string         `json:"session_id"`
	Status     SessionStatus  `json:"status"`
	CapturedAt time.Time      `json:"captured_at"`
	Transcript string         `json:"transcript,omitempty"`
	Screenshot []byte         `json:"screenshot,omitempty"`
	Structured map[string]any `json:"structured,omitempty"`
}

// SessionPrompt carries the seed input to start a session. Free-form
// text covers the common case; structured payloads (for MCP-style
// initialisation) flow through Extension.
type SessionPrompt struct {
	Text      string         `json:"text,omitempty"`
	Extension map[string]any `json:"extension,omitempty"`
}

// Reservation is the short-lived claim returned by ClaimNextReady. A
// reservation is converted to an Assignment by the matching WorkPlane
// claim; if the adaptor's TTL expires first, the reservation releases
// itself (domain.md §3.4 step 5 + conformance B.2).
type Reservation struct {
	ID         string     `json:"id"`
	WorkItemID WorkItemID `json:"work_item_id"`
	AgentID    AgentID    `json:"agent_id"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Nonce      string     `json:"nonce"`
}

// WorkspaceRequest is the input to AcquireWorkspace. The adaptor picks
// the weakest supported kind that satisfies RequiredIsolation (gm-root
// DD-5); failing to meet the ask must error rather than silently
// downgrade.
type WorkspaceRequest struct {
	AssignmentID      string                `json:"assignment_id"`
	Repository        string                `json:"repository"`
	Branch            string                `json:"branch,omitempty"`
	PreferredKind     WorkspaceKind         `json:"preferred_kind,omitempty"`
	RequiredIsolation IsolationCapabilities `json:"required_isolation"`
}

// EscalationRequest is the unified shape MCP elicitation, A2A
// input-required, permission prompts, HITL approvals and orchestrator
// pauses all map onto (gm-root DD-6).
type EscalationRequest struct {
	ID           string                `json:"id"`
	Source       EscalationKind        `json:"source"`
	AssignmentID string                `json:"assignment_id,omitempty"`
	WorkItemID   WorkItemID            `json:"work_item_id,omitempty"`
	AgentID      AgentID               `json:"agent_id,omitempty"`
	Urgency      EscalationUrgency     `json:"urgency"`
	Title        string                `json:"title"`
	Prompt       string                `json:"prompt"`
	Options      []EscalationOption    `json:"options,omitempty"`
	Deadline     *time.Time            `json:"deadline,omitempty"`
	State        EscalationState       `json:"state"`
	Resolution   *EscalationResolution `json:"resolution,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
}

// EscalationOption is a single choice in a multiple-choice escalation.
type EscalationOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// EscalationUrgency names whether an escalation suspends the session
// until it's answered.
type EscalationUrgency string

const (
	UrgencyBlocking EscalationUrgency = "blocking"
	UrgencyAdvisory EscalationUrgency = "advisory"
)

// EscalationState is the lifecycle tag on an EscalationRequest.
type EscalationState string

const (
	EscalationOpen     EscalationState = "open"
	EscalationResolved EscalationState = "resolved"
	EscalationCanceled EscalationState = "canceled"
	EscalationExpired  EscalationState = "expired"
)

// EscalationResolution is the outcome recorded when a human answers an
// escalation.
type EscalationResolution struct {
	Kind       EscalationResolutionKind `json:"kind"`
	Value      any                      `json:"value,omitempty"`
	ResolvedBy AgentID                  `json:"resolved_by"`
	ResolvedAt time.Time                `json:"resolved_at"`
}

// EscalationResolutionKind names the four canonical resolutions.
type EscalationResolutionKind string

const (
	ResolutionApprove EscalationResolutionKind = "approve"
	ResolutionDeny    EscalationResolutionKind = "deny"
	ResolutionModify  EscalationResolutionKind = "modify"
	ResolutionDefer   EscalationResolutionKind = "defer"
)

// AgentFilter narrows ListAgents. All fields are optional; zero value
// returns everything the adaptor knows about.
type AgentFilter struct {
	ParentID  *AgentID  `json:"parent_id,omitempty"`
	Kind      AgentKind `json:"agent_kind,omitempty"`
	GroupID   string    `json:"group_id,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
}

// ReadyFilter narrows ClaimNextReady to the subset of ready work an
// agent is willing to pick up.
type ReadyFilter struct {
	Labels      []string `json:"labels,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	MaxPriority *int     `json:"max_priority,omitempty"`
	SprintID    string   `json:"sprint_id,omitempty"`
	Repository  string   `json:"repository,omitempty"`
}

// EscalationFilter narrows ListOpenEscalations.
type EscalationFilter struct {
	AssignmentID string            `json:"assignment_id,omitempty"`
	WorkItemID   WorkItemID        `json:"work_item_id,omitempty"`
	AgentID      AgentID           `json:"agent_id,omitempty"`
	Source       EscalationKind    `json:"source,omitempty"`
	Urgency      EscalationUrgency `json:"urgency,omitempty"`
}

// SubscribeFilter narrows Subscribe to a subset of OrchestrationEvents.
type SubscribeFilter struct {
	AssignmentID string     `json:"assignment_id,omitempty"`
	SessionID    string     `json:"session_id,omitempty"`
	Kinds        []string   `json:"kinds,omitempty"`
	Since        *time.Time `json:"since,omitempty"`
}

// OrchestrationEvent is the streamed envelope covering session
// lifecycle, cost samples, escalation state transitions, and
// potential_conflict signals (domain.md §3.5).
type OrchestrationEvent struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	At           time.Time      `json:"at"`
	AssignmentID string         `json:"assignment_id,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	AgentID      AgentID        `json:"agent_id,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
}

// ConfirmNonce is the opaque token a caller echoes to confirm a
// mutating call that Gemba wants to be idempotent (pause, resume, end).
type ConfirmNonce string

// OrchestrationPlaneAdaptor is the interface every agent-runtime
// adaptor (Gas Town, LangGraph, CrewAI, OpenHands, Devin, Factory, …)
// implements. It mirrors domain.md §3.7 plus gm-root §Novel-Mechanism §8
// (declared_state / observed_state) for desired-vs-actual reconciliation.
//
// Method contract summary:
//
//   - Describe returns the capability manifest. Callers MAY cache it
//     for the lifetime of the adaptor registration.
//   - DeclaredState reports the topology the adaptor's configuration
//     asks for (Gas City's `city.toml`, Gas Town's `gastown.toml`,
//     LangGraph's static graph). ObservedState reports what the adaptor
//     actually sees running. Gemba diffs the two to surface drift.
//   - Claim/Start/Pause/Resume/End model the assignment lifecycle;
//     mutating calls take a ConfirmNonce for idempotency.
//   - Subscribe returns an async iterator of OrchestrationEvents; the
//     adaptor closes the channel on ctx cancellation.
//   - Every state-changing method (ClaimNextReady, ReleaseReservation,
//     StartSession, PauseSession, ResumeSession, EndSession,
//     AcquireWorkspace, ReleaseWorkspace, ResolveEscalation) MUST emit a
//     matching OrchestrationEvent on Subscribe within the adaptor's
//     declared latency budget (default 250ms for SSE/push, 5s for poll).
//     A successful mutation with no matching event is a conformance
//     failure (domain.md §3.8 Group E). See docs/adaptors/orchestration.md
//     and the Foolery-spike lesson (docs/prior-art/foolery.md) — the UI's
//     500ms freshness bar is unmeetable when state updates require client
//     polling.
//
// Implementations MUST be safe for concurrent use.
type OrchestrationPlaneAdaptor interface {
	// Describe returns the capability manifest.
	Describe() OrchestrationCapabilityManifest

	// DeclaredState returns the topology the adaptor's configuration
	// requests. For config-driven orchestrators this is cheap; for
	// pure-runtime adaptors without a declared form it MAY return an
	// empty topology with only CapturedAt set.
	DeclaredState(ctx context.Context) (WorkspaceTopology, error)

	// ObservedState returns the topology currently running. Callers
	// diff against DeclaredState to surface drift.
	ObservedState(ctx context.Context) (WorkspaceTopology, error)

	// ListAgents returns the agents the adaptor currently knows about,
	// filtered by f.
	ListAgents(ctx context.Context, f AgentFilter) ([]AgentRef, error)
	// ReadAgent returns a single agent, or nil if unknown.
	ReadAgent(ctx context.Context, id AgentID) (*AgentRef, error)

	// ListGroups returns every declared AgentGroup.
	ListGroups(ctx context.Context) ([]AgentGroup, error)
	// ResolveGroupMembers returns the current members of a group. For
	// pool groups this may call the check_endpoint; for graph groups
	// the adaptor resolves the topology.
	ResolveGroupMembers(ctx context.Context, groupID string) ([]AgentRef, error)

	// ClaimNextReady atomically reserves the next ready work item for
	// claimant. Returns (nil, nil) when no work is available (the
	// caller distinguishes this from an error). On a non-nil reservation,
	// the adaptor MUST emit a `reservation_claimed` OrchestrationEvent.
	ClaimNextReady(ctx context.Context, f ReadyFilter, claimant AgentRef) (*Reservation, error)
	// ReleaseReservation releases a reservation without converting it.
	// MUST emit a `reservation_released` OrchestrationEvent on success.
	ReleaseReservation(ctx context.Context, reservationID string) error

	// StartSession starts a session against an existing assignment and
	// returns the live Session. The adaptor MUST reject calls whose
	// assignmentID is unknown (conformance B.3) and MUST emit a
	// `session_transition` event with the new session's state on success.
	StartSession(ctx context.Context, assignmentID string, prompt SessionPrompt) (Session, error)
	// PauseSession moves a running session to suspended. Idempotent
	// under the same nonce. MUST emit a `session_transition` event
	// (first call only; replays under the same nonce emit nothing).
	PauseSession(ctx context.Context, sessionID string, nonce ConfirmNonce) (Session, error)
	// ResumeSession moves a suspended/input_required session back to
	// running. MUST emit a `session_transition` event on the first call.
	ResumeSession(ctx context.Context, sessionID string, nonce ConfirmNonce) (Session, error)
	// EndSession terminates a session with the given mode. Idempotent
	// under the same nonce (conformance B.4). MUST emit a
	// `session_transition` event on the first call.
	EndSession(ctx context.Context, sessionID string, mode SessionEndMode, nonce ConfirmNonce) (Session, error)
	// PeekSession returns a snapshot of a live session. The populated
	// fields are gated by manifest.peek_modes.
	PeekSession(ctx context.Context, sessionID string) (SessionPeek, error)

	// AcquireWorkspace provisions a workspace matching req. Errors
	// (rather than silently downgrading) if no supported kind satisfies
	// required_isolation. MUST emit a `workspace_acquired` event on
	// success.
	AcquireWorkspace(ctx context.Context, req WorkspaceRequest) (Workspace, error)
	// ReleaseWorkspace releases a workspace; idempotent. MUST emit a
	// `workspace_released` event on the first call.
	ReleaseWorkspace(ctx context.Context, workspaceID string) error
	// InspectWorkspace returns the current status of a workspace.
	InspectWorkspace(ctx context.Context, workspaceID string) (Workspace, error)

	// ListOpenEscalations returns escalations in state=open matching f.
	ListOpenEscalations(ctx context.Context, f EscalationFilter) ([]EscalationRequest, error)
	// ResolveEscalation records a resolution and, for blocking
	// escalations, unblocks the associated session. MUST emit an
	// `escalation_resolved` event on the first call under a given nonce;
	// for blocking escalations that resume the session, MUST also emit a
	// `session_transition` event.
	ResolveEscalation(ctx context.Context, escalationID string, r EscalationResolution, nonce ConfirmNonce) (EscalationRequest, error)

	// Subscribe streams OrchestrationEvents matching f. The adaptor
	// closes the channel when ctx is cancelled or the underlying
	// transport disconnects.
	Subscribe(ctx context.Context, f SubscribeFilter) (<-chan OrchestrationEvent, error)
}
