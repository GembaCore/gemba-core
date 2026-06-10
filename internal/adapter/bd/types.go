package bd

import (
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
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

// bdStatusForCategory is the canonical inverse of beadsStateMap used on
// the write path. The forward map is many-to-one (several bd statuses
// fold into StateStarted); the inverse picks the representative bd
// status the adaptor emits when a caller patches by StateCategory
// alone. StateStaged rides on `open` plus stagedLabel because bd has no
// native staging status. StateCanceled has no bd-native status —
// UpdateWorkItem refuses it rather than silently picking a surprising
// mapping (e.g. canceled → closed would merge with completed).
func bdStatusForCategory(cat core.StateCategory) (string, bool) {
	switch cat {
	case core.StateBacklog:
		return "deferred", true
	case core.StateUnstarted:
		return "open", true
	case core.StateStaged:
		return "open", true
	case core.StateStarted:
		return "in_progress", true
	case core.StateCompleted:
		return "closed", true
	}
	return "", false
}

// toWorkItem projects a Beads record onto the adaptor-agnostic
// core.WorkItem shape. Beads-specific fields that don't have a direct
// core counterpart (issue_type, notes, parent, dependency graph, raw
// timestamps) survive on Custom under the "beads:" namespace so the
// UI extension under web/src/extensions/beads/ can render them without
// a round-trip back to bd.
func (b *Bead) toWorkItem(prefix string, repos *core.RepositoryRegistry) core.WorkItem {
	category, ok := beadsStateMap[b.Status]
	if !ok {
		// Unknown statuses land in backlog so the SPA still has a lane
		// to paint the card. In practice this path should be
		// unreachable: adaptor startup calls StateMap.Validate and new
		// bd statuses need a map entry first.
		category = core.StateBacklog
	}
	if category == core.StateUnstarted && hasLabel(b.Labels, stagedLabel) {
		category = core.StateStaged
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
	id := buildWorkItemID(prefix, b.ID)
	kind := b.IssueType
	// Milestone convention (gm-root.3): older Beads stores modelled
	// milestones as epic beads tagged with "type:milestone"; newer
	// stores can carry issue_type=milestone directly. Project either
	// shape onto core.KindMilestone while preserving the native
	// issue_type under Custom["beads:issue_type"].
	if b.IssueType == core.KindMilestone || hasLabel(b.Labels, milestoneLabel) {
		kind = core.KindMilestone
	}
	primary, repoIDs := repositoriesFromLabels(b.Labels)
	// gm-d2ts: when no `repo:*` label is set and a registry is
	// available, fall back to bead-id-prefix routing. Lets operators
	// who organize beads by per-repo prefix skip per-bead labelling.
	if primary == "" && repos != nil {
		if r, ok := repos.GetByPrefix(prefixOf(b.ID)); ok {
			primary = r.ID
			repoIDs = []core.RepositoryID{r.ID}
		}
	}
	branches := branchesFromLabels(b.Labels)
	readPaths := readPathsFromLabels(b.Labels)
	writePaths := writePathsFromLabels(b.Labels)
	// gm-s47n.1.1: Layer 0 enrichment from labels.
	targets := targetsFromLabels(b.Labels)
	concepts := conceptsFromLabels(b.Labels)
	dispatch := dispatchStatusFromLabels(b.Labels)
	size := estimatedSizeFromLabels(b.Labels)
	// gm-e6.4: parse the embedded DoD section out of the description so
	// core.WorkItem.DoD is the canonical surface for acceptance criteria
	// + notes. Anything outside the delimiters survives verbatim on
	// Description; if no block exists, dod is nil and Description is the
	// raw bd value.
	descClean, dod := extractDoD(b.Description)
	wi := core.WorkItem{
		ID:                   id,
		Kind:                 kind,
		Title:                b.Title,
		Description:          descClean,
		Status:               b.Status,
		StateCategory:        category,
		Priority:             &priority,
		PrimaryRepositoryID:  primary,
		RepositoryIDs:        repoIDs,
		Branches:             branches,
		AdditionalReadPaths:  readPaths,
		AdditionalWritePaths: writePaths,
		Labels:               append([]string(nil), b.Labels...),
		Targets:              targets,
		Concepts:             concepts,
		DispatchStatus:       dispatch,
		EstimatedSize:        size,
		// Relationships carries only the three core edges (blocks,
		// parent_child, relates_to); the four extension edges plus the
		// raw bd edge blob ride Custom["beads:dependencies"] /
		// ["beads:dependents"] so the beads/ SPA extension can render
		// them when loaded (gm-e6.2, DD-9).
		Relationships: relationshipsFromBead(b, id, prefix),
		// gm-e6.5: native `evidence:*` labels become non-synthesized
		// Evidence entries. Synthesizer-backed entries are merged in
		// later by GetWorkItem.
		Evidence:  evidenceFromLabels(b.Labels),
		DoD:       dod,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
		Custom:    custom,
	}
	if b.Owner != "" {
		wi.Owner = &core.AgentRef{
			ID:   core.AgentID(b.Owner),
			Name: b.Owner,
			Kind: core.AgentKindHuman,
		}
	}
	// Assignee goes through agentRefFromBead so role + parent_id come
	// off the bead's labels (gm-e6.3 / DD-1). A bare assignee with no
	// agent:role:* / agent:parent:* labels still yields a valid AgentRef
	// — those fields are optional.
	wi.Assignee = agentRefFromBead(b.Assignee, b.Labels)
	return wi
}

// milestoneLabel is the legacy bd label Gemba used to tag a bead as a
// milestone (gm-root.3). Native Beads can now also store
// issue_type=milestone, so read/filter paths accept both forms.
const milestoneLabel = "type:milestone"

// stagedLabel is the bd-side convention for core.StateStaged: bd has no
// native "staged" status, so staged beads remain status=open with this
// label attached.
const stagedLabel = "staged:true"

// hasLabel reports whether needle appears in haystack. Case-sensitive
// by design: bd labels are lowercased by convention and exact match is
// the same rule bd applies on --label filtering.
func hasLabel(haystack []string, needle string) bool {
	for _, l := range haystack {
		if l == needle {
			return true
		}
	}
	return false
}

func setStagedLabel(labels []string, staged bool) []string {
	out := make([]string, 0, len(labels)+1)
	seen := false
	for _, l := range labels {
		if l == stagedLabel {
			continue
		}
		out = append(out, l)
	}
	if staged {
		for _, l := range out {
			if l == stagedLabel {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, stagedLabel)
		}
	}
	return out
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
