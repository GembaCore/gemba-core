// Package core: see doc.go for the overview.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is the sentinel error WorkPlane implementations return
// when a lookup id does not exist in the backend. Adaptors MAY wrap it
// with context (errors.Wrap / fmt.Errorf with %w) so long as
// errors.Is(err, ErrNotFound) still holds.
var ErrNotFound = errors.New("core: not found")

// ErrUnsupported is the sentinel error WorkPlane implementations return
// when the caller requests a feature group the manifest opts out of
// (for example ReadBudgetRollup on a non-budget-enforced adaptor). The
// UI uses errors.Is to decide whether to hide the control versus
// surface a fatal error.
var ErrUnsupported = errors.New("core: unsupported by adaptor")

// Transport names the wire protocol an adaptor uses to talk to the core
// (gm-root DD-12). Exactly one of these three values is valid per adaptor
// in v1; multi-transport adaptors are explicitly out of scope.
type Transport string

const (
	// TransportAPI — HTTP + JSON request/response.
	TransportAPI Transport = "api"
	// TransportJSONL — newline-delimited JSON over stdio (in-process or
	// subprocess).
	TransportJSONL Transport = "jsonl"
	// TransportMCP — Model Context Protocol. Recommended-not-required.
	TransportMCP Transport = "mcp"
)

// validTransports is the authoritative set used by Valid / UnmarshalJSON.
var validTransports = map[Transport]struct{}{
	TransportAPI:   {},
	TransportJSONL: {},
	TransportMCP:   {},
}

// Valid reports whether t is one of the three canonical transports.
func (t Transport) Valid() bool {
	_, ok := validTransports[t]
	return ok
}

// String satisfies fmt.Stringer and always returns the lowercase token.
func (t Transport) String() string { return string(t) }

// UnmarshalJSON rejects unknown transports at decode time so that a bad
// manifest fails at the adaptor boundary, not later when we try to route.
func (t *Transport) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	candidate := Transport(raw)
	if !candidate.Valid() {
		return fmt.Errorf("core: unknown Transport %q", raw)
	}
	*t = candidate
	return nil
}

// FieldExtension declares an adaptor-native field that core does not know
// about. The UI will only render it inside
// `web/src/extensions/<adaptor-id>/` (gm-root DD-4).
//
// Name is the JSON key the adaptor emits on WorkItem.Custom. Type is a
// free-form hint for the UI renderer ("string", "number", "duration",
// "url", "markdown", ...); the UI is responsible for mapping unknown
// types to a safe default widget.
type FieldExtension struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// EdgeExtension declares an adaptor-native relationship kind that is not
// one of the three core edges (gm-root DD-9). The capability-negotiation
// UI renders the extension's Name when the adaptor's own extension widget
// is loaded, and falls back to "relates_to" semantics otherwise.
//
// Inverse is the name of the extension edge that the adaptor emits when
// the same logical link is walked backwards ("blocks" ↔ "blocked_by").
// Leave empty when the edge is symmetric or has no defined inverse.
type EdgeExtension struct {
	Name        string `json:"name"`
	Directed    bool   `json:"directed"`
	Inverse     string `json:"inverse,omitempty"`
	Description string `json:"description,omitempty"`
}

// RelationshipExtension declares an adaptor-native field on the
// Relationship record itself (not a new edge kind). Use this when the
// adaptor carries metadata on every edge — Jira link categories, beads
// edge confidence scores, LangGraph data-flow contracts — that the UI
// extension may want to render alongside the core edge.
type RelationshipExtension struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// StateMap is the adaptor's declared translation from its native status
// tokens to the five core StateCategory buckets. Every native status the
// adaptor can emit must appear as a key; missing keys surface as
// conformance failures and force the UI onto an unknown-state fallback.
//
// The map is declarative: core does not attempt to infer categories from
// status names. This keeps lane placement deterministic and keeps the
// UI free of adaptor-specific vocabulary (gm-root DD-4).
type StateMap map[string]StateCategory

// Validate reports the first native status whose target bucket is not a
// valid StateCategory. Adaptors should run this inside their init path
// so a malformed map fails at startup rather than at first query.
func (m StateMap) Validate() error {
	for native, bucket := range m {
		if !bucket.Valid() {
			return fmt.Errorf(
				"core: StateMap[%q] = %q is not a valid StateCategory",
				native, bucket)
		}
	}
	return nil
}

// CapabilityManifest is the declarative description every WorkPlane
// adaptor returns from Describe. It tells core (and the UI) which
// transport the adaptor speaks, how to normalize its native statuses,
// what extensions it carries beyond the core primitives, and which
// optional feature groups it opts into.
//
// The manifest is the single source of truth the capability-negotiation
// UI consults before rendering adaptor-specific controls (gm-e11.4 /
// gm-root DD-15): controls for unsupported capabilities are hidden, not
// disabled, so the operator never sees a button they can't use.
type CapabilityManifest struct {
	AdaptorName    string `json:"adaptor_name"`
	AdaptorVersion string `json:"adaptor_version"`
	// ProtocolVersion is the core contract version the adaptor was built
	// against. Version negotiation (gm-e3.4) compares this to the core's
	// advertised core_version and fails startup on mismatch.
	ProtocolVersion string `json:"protocol_version"`

	// Transport is the wire protocol the adaptor speaks. Exactly one of
	// api|jsonl|mcp per adaptor in v1.
	Transport Transport `json:"transport"`

	// StateMap translates the adaptor's native statuses to the five core
	// StateCategory buckets. Required; an adaptor with no states is not a
	// valid WorkPlane.
	StateMap StateMap `json:"state_map"`

	// EdgeExtensions declares non-core relationship kinds (anything
	// beyond blocks / parent_child / relates_to).
	EdgeExtensions []EdgeExtension `json:"edge_extensions,omitempty"`

	// FieldExtensions declares non-core fields the adaptor emits on
	// WorkItem.Custom.
	FieldExtensions []FieldExtension `json:"field_extensions,omitempty"`

	// RelationshipExtensions declares per-edge metadata the adaptor
	// attaches to every Relationship record.
	RelationshipExtensions []RelationshipExtension `json:"relationship_extensions,omitempty"`

	// SprintNative — adaptor emits first-class Sprint records. When
	// false, core treats ListSprints as "may return empty" and hides the
	// sprint lane chrome in the UI.
	SprintNative bool `json:"sprint_native"`

	// TokenBudgetEnforced — adaptor carries a real TokenBudget with
	// three-tier enforcement (gm-root DD-14). When false, the UI may
	// still render a budget if one is configured, but the "stop" tier
	// has no runtime effect.
	TokenBudgetEnforced bool `json:"token_budget_enforced"`

	// EvidenceSynthesisRequired — adaptor expects the core to synthesize
	// Evidence records from transport-level artifacts (commits, test
	// runs) rather than receive them pre-built (gm-root DD-13). When
	// false, the adaptor provides its own Evidence in GetWorkItem.
	EvidenceSynthesisRequired bool `json:"evidence_synthesis_required"`

	// ReadOnly — adaptor cannot service mutations. CreateWorkItem,
	// UpdateWorkItem, and AttachEvidence all fail with KindReadOnly;
	// the UI MUST hide write controls rather than disable them. Used
	// by the --dolt-url direct SQL connector (gm-0fd); the bd CLI
	// adapter leaves this false because it mediates mutations through
	// bd's public API.
	ReadOnly bool `json:"read_only"`
}

// Validate applies structural checks that every manifest must satisfy.
// Adaptors should call this in the startup path so doctor reports a
// clean failure before the transport is bound.
func (m CapabilityManifest) Validate() error {
	if m.AdaptorName == "" {
		return fmt.Errorf("core: CapabilityManifest.AdaptorName is required")
	}
	if m.ProtocolVersion == "" {
		return fmt.Errorf("core: CapabilityManifest.ProtocolVersion is required")
	}
	if !m.Transport.Valid() {
		return fmt.Errorf("core: CapabilityManifest.Transport %q is not valid", m.Transport)
	}
	if len(m.StateMap) == 0 {
		return fmt.Errorf("core: CapabilityManifest.StateMap must not be empty")
	}
	return m.StateMap.Validate()
}

// WorkItemFilter narrows a ListWorkItems query. Zero values mean "no
// filter on that field"; the intersection of non-zero fields applies.
// Adaptors are expected to push as many filters down to their native
// store as possible and only fall back to in-process filtering for
// predicates their backend can't express.
type WorkItemFilter struct {
	IDs           []WorkItemID    `json:"ids,omitempty"`
	Kinds         []string        `json:"kinds,omitempty"`
	Statuses      []string        `json:"statuses,omitempty"`
	StateCategory []StateCategory `json:"state_category,omitempty"`
	AssigneeID    *AgentID        `json:"assignee_id,omitempty"`
	SprintID      *string         `json:"sprint_id,omitempty"`
	Labels        []string        `json:"labels,omitempty"`
	UpdatedSince  *time.Time      `json:"updated_since,omitempty"`
	// Limit caps the returned set. 0 means "adaptor default".
	Limit int `json:"limit,omitempty"`
}

// WorkItemPatch carries an update intended for an existing WorkItem.
// Every field is optional; nil pointers and empty slices mean "do not
// touch". Adaptors translate the patch to their backend's public API
// (gm-root DD-9) — core never writes private storage.
//
// Status and StateCategory move together at the adaptor boundary: the
// adaptor may accept either (its own native token or the normalized
// bucket) and must reject the case where both are set and inconsistent
// with its StateMap.
type WorkItemPatch struct {
	Title         *string           `json:"title,omitempty"`
	Description   *string           `json:"description,omitempty"`
	Status        *string           `json:"status,omitempty"`
	StateCategory *StateCategory    `json:"state_category,omitempty"`
	Priority      *int              `json:"priority,omitempty"`
	Owner         *AgentRef         `json:"owner,omitempty"`
	Assignee      *AgentRef         `json:"assignee,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	DoD           *DefinitionOfDone `json:"dod,omitempty"`
	SprintID      *string           `json:"sprint_id,omitempty"`
	Custom        map[string]any    `json:"custom,omitempty"`
}

// BudgetRollup is the aggregated token consumption for a sprint,
// suitable for the UI's budget dashboard (gm-e12.3 and friends).
//
// Used/Limit/Tier are convenience projections of the underlying
// TokenBudget at read time; ByWorkItem lets the UI break the total
// down without a second query.
type BudgetRollup struct {
	SprintID   string               `json:"sprint_id"`
	Budget     TokenBudget          `json:"budget"`
	Tier       BudgetTier           `json:"tier"`
	ByWorkItem map[WorkItemID]int64 `json:"by_work_item,omitempty"`
	CapturedAt time.Time            `json:"captured_at"`
}

// WorkPlane is the adaptor-agnostic surface every work-tracker
// implementation must satisfy. One WorkPlane is bound per gemba process
// (gm-root DD-1) and paired with one OrchestrationPlane (see
// orchestration.go, gm-e3.3).
//
// Methods are divided into three groups:
//
//  1. Describe — declarative capability advertisement. Called at
//     startup by doctor and on every reconnect by the UI so it always
//     renders against a fresh manifest.
//  2. Work-item queries and mutations — the main CRUD surface.
//     Mutations MUST be implemented by calling the backend's public
//     CLI or API; direct writes to private storage are forbidden (gm-
//     root DD-9).
//  3. Sprint + budget — optional feature group. Adaptors that set
//     SprintNative=false may return empty slices and zero rollups; the
//     UI hides sprint chrome when the manifest says so.
//
// Contexts carry the request deadline and tracing span (gm-e3.6);
// implementations should propagate them verbatim to their transport
// and return a wrapped error on cancellation.
type WorkPlane interface {
	// Describe returns the adaptor's declared capabilities. MUST be
	// idempotent and side-effect-free; called repeatedly by the UI and
	// the doctor command.
	Describe(ctx context.Context) (CapabilityManifest, error)

	// ListWorkItems returns the work items matching filter, sorted by
	// the adaptor's natural order (typically UpdatedAt desc). Adaptors
	// should respect filter.Limit; 0 means "adaptor default".
	ListWorkItems(ctx context.Context, filter WorkItemFilter) ([]WorkItem, error)

	// GetWorkItem returns a single work item by ID, with relationships
	// and evidence populated when the adaptor supports them.
	// Returns ErrNotFound (or a wrapper thereof) when id is unknown.
	GetWorkItem(ctx context.Context, id WorkItemID) (WorkItem, error)

	// CreateWorkItem creates a new work item through the backend's
	// public API and returns the materialized record (including the
	// backend-assigned id and timestamps). Implementations MUST NOT
	// write private storage directly (gm-root DD-9).
	CreateWorkItem(ctx context.Context, wi WorkItem) (WorkItem, error)

	// UpdateWorkItem applies patch to the work item with the given id
	// and returns the resulting record. Adaptors reject patches that
	// violate their own invariants (e.g. illegal status transitions)
	// with a structured error the UI can surface verbatim.
	UpdateWorkItem(ctx context.Context, id WorkItemID, patch WorkItemPatch) (WorkItem, error)

	// AttachEvidence appends an evidence record to the work item. When
	// the manifest sets EvidenceSynthesisRequired=true the core may
	// call this from its own synthesis pipeline; otherwise the adaptor
	// is expected to manage its own evidence and this method may
	// return an ErrUnsupported.
	AttachEvidence(ctx context.Context, id WorkItemID, ev Evidence) error

	// ListSprints returns the sprints currently declared by the
	// backend. Adaptors with SprintNative=false MAY return an empty
	// slice without error.
	ListSprints(ctx context.Context) ([]Sprint, error)

	// ReadBudgetRollup returns the token consumption aggregated against
	// the named sprint. Adaptors with SprintNative=false or
	// TokenBudgetEnforced=false SHOULD return ErrUnsupported so the UI
	// can hide the widget.
	ReadBudgetRollup(ctx context.Context, sprintID string) (BudgetRollup, error)
}
