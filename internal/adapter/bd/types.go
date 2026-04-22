package bd

import (
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Bead mirrors a row of `bd show --json` / `bd list --json`. Only the
// fields the WorkPlane adaptor projects onto core.WorkItem or
// round-trips through Custom are typed here; bd ships many more
// (ephemeral, wisp_type, external_ref, metadata, ...) that can be added
// as adjacent features (agent federation, evidence synthesis) need
// them. Passing the raw bd JSON through the extension channel keeps
// the DoD round-trip green without committing to every bd field at
// once.
type Bead struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	IssueType   string     `json:"issue_type"`
	Assignee    string     `json:"assignee,omitempty"`
	Owner       string     `json:"owner,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   string     `json:"created_by,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	Labels      []string   `json:"labels,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	Parent      string     `json:"parent,omitempty"`
	// Dependencies / Dependents carry Beads's 7 native edge types in
	// their raw shape here so gm-e6.2 can land the structured mapping
	// without reshaping this struct. Until then they round-trip through
	// Custom untouched so no edge metadata is lost.
	Dependencies []BeadEdge `json:"dependencies,omitempty"`
	Dependents   []BeadEdge `json:"dependents,omitempty"`
}

// BeadEdge is the raw shape of an entry in Bead.Dependencies /
// Bead.Dependents. The fields populated differ by direction: the
// `dependencies` array uses {issue_id, depends_on_id, type}; the
// `dependents` array flattens the linked bead plus a `dependency_type`.
// We accept both shapes and let gm-e6.2 decide on the normalized form.
type BeadEdge struct {
	IssueID        string `json:"issue_id,omitempty"`
	DependsOnID    string `json:"depends_on_id,omitempty"`
	ID             string `json:"id,omitempty"`
	Title          string `json:"title,omitempty"`
	Type           string `json:"type,omitempty"`
	DependencyType string `json:"dependency_type,omitempty"`
}

// beadsStateMap is the declarative mapping from bd status tokens to the
// five core StateCategory buckets (gm-root DD-4). Keys cover every
// status the Beads CLI can emit today per `bd update --status` help +
// observed production values.
//
// Group-A's manifest round-trip probe asserts this map survives JSON;
// missing a bd-native status here would fail CapabilityManifest.Validate
// at adaptor startup.
var beadsStateMap = core.StateMap{
	"open":        core.StateUnstarted,
	"in_progress": core.StateStarted,
	"hooked":      core.StateStarted,
	"pinned":      core.StateStarted,
	"blocked":     core.StateStarted,
	"deferred":    core.StateBacklog,
	"closed":      core.StateCompleted,
}

// toWorkItem projects a Beads record onto the adaptor-agnostic
// core.WorkItem shape. Beads-specific fields that don't have a direct
// core counterpart (issue_type, notes, parent, dependency graph, raw
// timestamps) survive on Custom under the "beads:" namespace so the
// UI extension under web/src/extensions/beads/ can render them without
// a round-trip back to bd.
func (b *Bead) toWorkItem(prefix string) core.WorkItem {
	category, ok := beadsStateMap[b.Status]
	if !ok {
		// Unknown statuses land in backlog so the SPA still has a lane
		// to paint the card. In practice this path should be
		// unreachable: adaptor startup calls StateMap.Validate and new
		// bd statuses need a map entry first.
		category = core.StateBacklog
	}
	priority := b.Priority
	custom := map[string]any{}
	if b.IssueType != "" {
		custom["beads:issue_type"] = b.IssueType
	}
	if b.Notes != "" {
		custom["beads:notes"] = b.Notes
	}
	if b.Parent != "" {
		custom["beads:parent"] = b.Parent
	}
	if b.CreatedBy != "" {
		custom["beads:created_by"] = b.CreatedBy
	}
	if b.StartedAt != nil {
		custom["beads:started_at"] = *b.StartedAt
	}
	if len(b.Dependencies) > 0 {
		custom["beads:dependencies"] = b.Dependencies
	}
	if len(b.Dependents) > 0 {
		custom["beads:dependents"] = b.Dependents
	}
	wi := core.WorkItem{
		ID:            buildWorkItemID(prefix, b.ID),
		Kind:          b.IssueType,
		Title:         b.Title,
		Description:   b.Description,
		Status:        b.Status,
		StateCategory: category,
		Priority:      &priority,
		Labels:        append([]string(nil), b.Labels...),
		CreatedAt:     b.CreatedAt,
		UpdatedAt:     b.UpdatedAt,
		Custom:        custom,
	}
	if b.Owner != "" {
		wi.Owner = &core.AgentRef{
			ID:   core.AgentID(b.Owner),
			Name: b.Owner,
			Kind: core.AgentKindHuman,
		}
	}
	if b.Assignee != "" {
		wi.Assignee = &core.AgentRef{
			ID:   core.AgentID(b.Assignee),
			Name: b.Assignee,
			Kind: core.AgentKindAgent,
		}
	}
	return wi
}

// buildWorkItemID composes a workspace-qualified WorkItemID from the
// adaptor's prefix and a bare Beads id. An empty prefix yields the bare
// id so callers using raw bd ids (gm-abc) keep working.
func buildWorkItemID(prefix, native string) core.WorkItemID {
	if prefix == "" {
		return core.WorkItemID(native)
	}
	return core.WorkItemID(prefix + "/" + native)
}

// nativeID strips the adaptor's workspace prefix from a WorkItemID to
// yield the bare bd id the CLI expects. We accept either the exact
// registered prefix OR any path whose last segment is the bd id — the
// conformance harness mints ids as "gemba/gemba/gm-..." regardless of
// which adaptor is under test, so being permissive on the prefix keeps
// the Group B probes green.
func nativeID(prefix string, id core.WorkItemID) string {
	s := string(id)
	if prefix != "" && strings.HasPrefix(s, prefix+"/") {
		return strings.TrimPrefix(s, prefix+"/")
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
