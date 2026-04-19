// Package core: see doc.go for the overview.
package core

import "time"

// WorkItemID is the workspace-qualified identifier for a work item.
// Format is "<workspace>/<repo>/<native-id>" (e.g. "gemba/gemba/gm-e3.1",
// "myorg/atl/GEMBA-17"). The workspace/repo prefix disambiguates
// multi-workspace deployments (gm-root DD-6); two Beads workspaces on
// the same Gemba instance are distinguishable by this prefix where a
// bare adaptor-kind prefix would collide.
type WorkItemID string

// AgentID is the workspace-qualified identifier for an agent. Same shape
// rules as WorkItemID: "<workspace>/<repo-or-scope>/<native-id>" (e.g.
// "gemba/polecats/jasper", "langgraph/run-42/node-a").
type AgentID string

// WorkItem is the adaptor-agnostic view of a unit of work. Every
// WorkPlaneAdaptor must be able to project its native record (Beads
// issue, Jira issue, GitHub issue, LangGraph task) onto this shape.
//
// Design notes:
//
//   - Status holds the adaptor's own word for the current state so the
//     UI can show "In Review" instead of "started" when it matters.
//     StateCategory carries the normalized bucket used for lane placement.
//   - DoD is informational-only; core never blocks transitions on it.
//     That rule is locked by gm-root DD #? / DD-10.
//   - Custom is an escape hatch for adaptor-specific fields that don't
//     map onto any cross-cutting primitive. The UI only renders them
//     inside `web/src/extensions/<adaptor-id>/` (gm-root DD-4).
type WorkItem struct {
	ID            WorkItemID        `json:"id"`
	Kind          string            `json:"kind"`
	Title         string            `json:"title"`
	Description   string            `json:"description,omitempty"`
	Status        string            `json:"status"`
	StateCategory StateCategory     `json:"state_category"`
	Priority      *int              `json:"priority,omitempty"`
	Owner         *AgentRef         `json:"owner,omitempty"`
	Assignee      *AgentRef         `json:"assignee,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Relationships []Relationship    `json:"relationships,omitempty"`
	Evidence      []Evidence        `json:"evidence,omitempty"`
	DoD           *DefinitionOfDone `json:"dod,omitempty"`
	SprintID      *string           `json:"sprint_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Custom        map[string]any    `json:"custom,omitempty"`
}

// AgentKind distinguishes an automated agent from a human operator.
// The UI renders the two with distinct visual treatment (gm-e12.4
// Agents dashboard depends on this); capability manifests may also
// gate action types on Kind.
type AgentKind string

const (
	// AgentKindAgent — an automated actor (polecat, LangGraph node,
	// Gas Town crew member, etc.).
	AgentKindAgent AgentKind = "agent"
	// AgentKindHuman — a human operator.
	AgentKindHuman AgentKind = "human"
)

// AgentRef is the adaptor-agnostic view of an agent — a polecat, a
// LangGraph node, a Gas Town crew member, or a human user. Core
// doesn't care which; capability manifests decide what actions that
// agent can be the subject of.
//
// ParentID carries parent-agent federation (gm-root DD-1): orchestrators
// with hierarchical structures (Gas Town Mayor → Polecats; LangGraph
// supervisor → subgraph nodes) populate it so the UI can render agent
// hierarchies without adaptor-specific hacks. Nil means the agent is
// top-level or standalone.
type AgentRef struct {
	ID        AgentID   `json:"id"`
	Name      string    `json:"name"`
	Kind      AgentKind `json:"agent_kind"`
	ParentID  *AgentID  `json:"parent_id,omitempty"`
	Role      string    `json:"role,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
}

// RelationshipKind enumerates the directed edges the two planes share.
// Per gm-root DD-9, core recognises exactly three kinds; any richer
// adaptor-native edge type (Beads's 7 edges, Jira's link catalogue,
// LangGraph's control/data flow) either maps onto one of these at the
// adaptor boundary or is declared through the adaptor's
// CapabilityManifest and rendered as "relates_to" in the core Kanban UI.
type RelationshipKind string

const (
	// RelBlocks — directed: From blocks To (From must complete before
	// To can start). Adaptors whose native edge is "depends_on" map the
	// inverse to this edge at their boundary.
	RelBlocks RelationshipKind = "blocks"
	// RelParentChild — directed: From is the parent of To (epic→story,
	// supervisor→subtask, orchestrator→polecat work).
	RelParentChild RelationshipKind = "parent_child"
	// RelRelatesTo — advisory cross-reference. No ordering semantics.
	// This is the edge the capability-negotiation UI falls back to when
	// an adaptor declares a non-core edge type that core should still
	// render as "associated with".
	RelRelatesTo RelationshipKind = "relates_to"
)

// Relationship is a typed edge between two work items. Edges are stored
// on the source item's Relationships slice; adaptors are responsible for
// emitting the inverse if they want both directions walkable without a
// full scan. Only the three kinds above are valid core edges; adaptor
// extensions flow through CapabilityManifest (see gm-e3.2).
type Relationship struct {
	Kind RelationshipKind `json:"kind"`
	From WorkItemID       `json:"from"`
	To   WorkItemID       `json:"to"`
}

// EvidenceKind enumerates the categories of artifact Gemba understands.
// Adaptors MAY attach opaque payloads by setting Kind = EvidenceCustom
// and populating Payload.
type EvidenceKind string

const (
	// EvidenceCommit — a VCS commit (SHA in Ref, repo URL in Source).
	EvidenceCommit EvidenceKind = "commit"
	// EvidenceLog — a log line, transcript excerpt, or streamed output.
	EvidenceLog EvidenceKind = "log"
	// EvidenceTestResult — output of a test run (pass/fail/duration).
	EvidenceTestResult EvidenceKind = "test_result"
	// EvidenceURL — an external link (PR, dashboard, dashboard screenshot).
	EvidenceURL EvidenceKind = "url"
	// EvidenceFile — a file artifact (path in Ref, adaptor knows how to read).
	EvidenceFile EvidenceKind = "file"
	// EvidenceCustom — adaptor-defined shape. Inspect Source + Payload.
	EvidenceCustom EvidenceKind = "custom"
)

// Evidence is a single artifact captured against a work item: a commit
// that implements it, a test run that verified it, a URL that documents
// it. Evidence is append-only from the UI's perspective; adaptors may
// reconstruct the list on every read.
type Evidence struct {
	ID         string         `json:"id"`
	Kind       EvidenceKind   `json:"kind"`
	Source     string         `json:"source"`
	Ref        string         `json:"ref,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	CapturedAt time.Time      `json:"captured_at"`
	Payload    map[string]any `json:"payload,omitempty"`
}
