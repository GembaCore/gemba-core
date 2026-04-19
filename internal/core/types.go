// Package core: see doc.go for the overview.
package core

import "time"

// WorkItemID is the adaptor-qualified identifier for a work item. Format
// is intentionally opaque to Gemba core; adaptors choose whatever scheme
// is stable and unique within their own namespace (e.g. "bd:gm-e3.1",
// "jira:GEMBA-17"). The prefix lets multi-adaptor setups round-trip IDs
// without collision.
type WorkItemID string

// AgentID is the adaptor-qualified identifier for an agent. Same shape
// rules as WorkItemID; the prefix names the orchestration adaptor (e.g.
// "gt:gemba/polecats/jasper", "langgraph:run-42/node-a").
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

// AgentRef is the adaptor-agnostic view of an agent — a human, a polecat,
// a LangGraph node, a Gas Town crew member, or a plain human user. Core
// doesn't care which; capability manifests decide what actions that
// agent can be the subject of.
type AgentRef struct {
	ID        AgentID `json:"id"`
	Name      string  `json:"name"`
	Role      string  `json:"role,omitempty"`
	Workspace string  `json:"workspace,omitempty"`
}

// RelationshipKind enumerates the directed edges the two planes share.
// Adaptor-specific link types must map onto one of these or be exposed
// through Custom; the Kanban UI only renders these.
type RelationshipKind string

const (
	// RelBlocks — From blocks To. Inverse of depends_on.
	RelBlocks RelationshipKind = "blocks"
	// RelDependsOn — From depends on To completing first.
	RelDependsOn RelationshipKind = "depends_on"
	// RelParentOf — hierarchical parent→child (epic→story).
	RelParentOf RelationshipKind = "parent_of"
	// RelChildOf — the other direction of parent_of, exposed explicitly so
	// adaptors that only know "my parent is X" don't have to invert.
	RelChildOf RelationshipKind = "child_of"
	// RelRelated — non-directional association. Use sparingly.
	RelRelated RelationshipKind = "related"
)

// Relationship is a typed edge between two work items. Edges are stored
// on the source item's Relationships slice; adaptors are responsible for
// emitting the inverse if they want both directions walkable without a
// full scan.
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
