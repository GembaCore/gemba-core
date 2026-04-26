// Package core: see doc.go for the overview.
package core

import (
	"fmt"
	"time"
)

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

// RepositoryID is the slug naming a [Repository] inside a workspace
// (gm-26n4). One workspace may hold many repositories; every WorkItem
// is associated with exactly one. The slug appears as the second
// segment of [WorkItemID] and as the filename stem of the
// `.gemba/repositories/<id>.toml` file the Repository is loaded from.
type RepositoryID string

// RepositoryUnspecified is the sentinel value
// [WorkItem.PrimaryRepositoryID] takes when a bead was filed before
// repository tracking landed (gm-26n4) or when an adaptor cannot
// derive a repository for a native record. Spawn paths reject
// "unspecified" beads with a caller-visible error so the operator
// backfills before working.
const RepositoryUnspecified RepositoryID = "unspecified"

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
//
// Canonical cross-adaptor tokens for WorkItem.Kind (and
// WorkItemFilter.Kinds). Adaptors MAY emit additional kinds beyond
// these constants; the set here exists so core-layer callers (filters,
// the SPA's lane chrome) don't hard-code string literals for cross-
// cutting concepts.
//
// KindMilestone is Gemba-native: there is no native "milestone" type in
// bd. The Beads adaptor encodes a milestone as `-t epic` + label
// "type:milestone" and projects that convention onto KindMilestone on
// read (gm-root.3 / gm-root.3.5). Filtering WorkItemFilter.Kinds to
// {KindMilestone} returns only the label-flagged beads.
const KindMilestone = "milestone"

type WorkItem struct {
	ID WorkItemID `json:"id"`
	// PrimaryRepositoryID names the [Repository] this work item lives
	// in primarily (gm-kdh3). The spawn path reads this to pick the
	// working directory and worktree pool for an agent session.
	// Empty (or [RepositoryUnspecified]) means the bead has not been
	// backfilled since repository tracking landed; spawn rejects with
	// a clear error so the operator sets it explicitly. Use the
	// [WorkItem.RepositoryID] method (no arg) when you only need the
	// primary id and don't care about plurality.
	PrimaryRepositoryID RepositoryID `json:"primary_repository_id,omitempty"`

	// RepositoryIDs lists every [Repository] this work item touches
	// (gm-kdh3). A multi-repo bead — common for cross-cutting work
	// like a backend API change with a frontend client update — has
	// more than one entry. Validation rules:
	//   - PrimaryRepositoryID, when set, MUST appear in this slice
	//   - this slice non-empty + PrimaryRepositoryID empty is invalid
	// [WorkItem.NormalizeRepositories] auto-promotes a sole
	// PrimaryRepositoryID into a single-element slice for back-
	// compat with the gm-26n4 single-repo shape.
	RepositoryIDs []RepositoryID `json:"repository_ids,omitempty"`

	// Branches names the git branch this bead's work happens on, per
	// repository (gm-ou02). When empty, the spawn path derives a
	// branch as `<bead-id>-<slugified-title>` from each repo's
	// default branch. When non-empty, the spawn path uses the
	// explicit value for the named repo and errors on a missing entry
	// for a repo in RepositoryIDs.
	Branches []BeadBranch `json:"branches,omitempty"`

	// AdditionalReadPaths extends the default read surface a session
	// gets when working this bead (gm-v8vr). Glob-style patterns;
	// resolved as additional read-only allowances on top of cwd +
	// sibling Repository.Path entries + workspace .gemba/ + standard
	// tooling whitelist. Used when a bead legitimately needs to peek
	// outside the workspace surface — e.g. a polecat that has to
	// reference vendored docs at `~/notes/` or a config file under
	// `~/.aws/credentials` (which is sensitive: justify in bead
	// notes). Surfaces in bd via labels of the form `read:<glob>`.
	AdditionalReadPaths []string `json:"additional_read_paths,omitempty"`

	// AdditionalWritePaths grants writes outside cwd. Very rare —
	// the operator must justify in bead notes. Surfaces via labels
	// `write:<glob>`.
	AdditionalWritePaths []string          `json:"additional_write_paths,omitempty"`
	Kind                 string            `json:"kind"`
	Title                string            `json:"title"`
	Description          string            `json:"description,omitempty"`
	Status               string            `json:"status"`
	StateCategory        StateCategory     `json:"state_category"`
	Priority             *int              `json:"priority,omitempty"`
	Owner                *AgentRef         `json:"owner,omitempty"`
	Assignee             *AgentRef         `json:"assignee,omitempty"`
	Labels               []string          `json:"labels,omitempty"`
	Relationships        []Relationship    `json:"relationships,omitempty"`
	Evidence             []Evidence        `json:"evidence,omitempty"`
	DoD                  *DefinitionOfDone `json:"dod,omitempty"`
	SprintID             *string           `json:"sprint_id,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
	Custom               map[string]any    `json:"custom,omitempty"`
	// Derived carries UI-facing booleans computed by Derive from this
	// item plus its open escalations (gm-gsh). Servers SHOULD populate
	// it on every read so the UI does not have to rebuild the predicate
	// per-card; it is omitempty because the pure derivation also lets
	// the UI recompute locally if a transport omits it.
	Derived *DerivedSignals `json:"derived,omitempty"`
}

// BeadBranch maps a repository to the git branch this bead's work
// happens on inside it (gm-ou02). A multi-repo bead may carry one
// entry per touched repository; a single-repo bead typically carries
// at most one. When the spawn path needs a branch for a polecat and
// no entry matches, it derives one as `<bead-id>-<slugified-title>`
// from the corresponding [Repository.DefaultBranch].
type BeadBranch struct {
	// RepositoryID names the repo this branch lives in. MUST appear
	// in [WorkItem.RepositoryIDs] — entries for unknown repos are
	// rejected by [WorkItem.ValidateBranches] to catch typos that
	// would otherwise silently miss the spawn lookup.
	RepositoryID RepositoryID `json:"repository_id"`

	// Branch is the git branch name. Non-empty; whitespace is not
	// trimmed (the operator is responsible for naming hygiene; the
	// loader doesn't reformat).
	Branch string `json:"branch"`
}

// ValidateBranches checks the per-repo branch mapping. Caller-safe
// errors. Rules:
//
//   - Each entry's RepositoryID must be non-empty and present in
//     [WorkItem.RepositoryIDs] (catches typos).
//   - Branch must be non-empty.
//   - At most one entry per RepositoryID — duplicates are rejected.
//
// Empty Branches is valid (the spawn path falls back to derivation).
func (w WorkItem) ValidateBranches() error {
	if len(w.Branches) == 0 {
		return nil
	}
	repoIDs := make(map[RepositoryID]bool, len(w.RepositoryIDs))
	for _, id := range w.RepositoryIDs {
		repoIDs[id] = true
	}
	seen := make(map[RepositoryID]bool, len(w.Branches))
	for _, br := range w.Branches {
		if br.RepositoryID == "" {
			return fmt.Errorf(
				"work item %q: branches contains an entry with empty repository_id",
				w.ID)
		}
		if br.Branch == "" {
			return fmt.Errorf(
				"work item %q: branches[%q].branch must not be empty",
				w.ID, br.RepositoryID)
		}
		if seen[br.RepositoryID] {
			return fmt.Errorf(
				"work item %q: branches contains duplicate repository_id %q",
				w.ID, br.RepositoryID)
		}
		seen[br.RepositoryID] = true
		if !repoIDs[br.RepositoryID] {
			return fmt.Errorf(
				"work item %q: branches[%q] references repository %q not in repository_ids %v",
				w.ID, br.RepositoryID, br.RepositoryID, w.RepositoryIDs)
		}
	}
	return nil
}

// BranchFor returns the recorded branch for the given repository, if
// any. The bool is false when no entry maps to id; the caller derives
// a default in that case. Provided as a method so call sites in the
// spawn driver read clearly.
func (w WorkItem) BranchFor(id RepositoryID) (string, bool) {
	for _, br := range w.Branches {
		if br.RepositoryID == id {
			return br.Branch, true
		}
	}
	return "", false
}

// RepositoryID returns the primary repository id this work item is
// bound to (gm-kdh3). Convenience accessor for callers that don't
// care about multi-repo plurality — the spawn path's cwd selection
// always uses the primary, never a non-primary entry. Equivalent to
// [WorkItem.PrimaryRepositoryID]; provided as a method so call sites
// read clearly when intent is "the repository", not "the field".
func (w WorkItem) RepositoryID() RepositoryID { return w.PrimaryRepositoryID }

// NormalizeRepositories enforces invariants that the JSON wire shape
// alone cannot. Called by every adaptor projecting a native record
// onto WorkItem so callers downstream (spawn, registry, validation)
// see a coherent state.
//
// Rules:
//
//   - When RepositoryIDs is empty AND PrimaryRepositoryID is set,
//     auto-promote PrimaryRepositoryID into RepositoryIDs as a
//     single-element slice. Preserves back-compat with the gm-26n4
//     single-repo shape (callers that only set the primary still
//     produce a valid multi-repo bead).
//   - Otherwise, fields are left untouched. ValidateRepositories
//     surfaces any remaining inconsistencies as errors.
func (w *WorkItem) NormalizeRepositories() {
	if len(w.RepositoryIDs) == 0 && w.PrimaryRepositoryID != "" {
		w.RepositoryIDs = []RepositoryID{w.PrimaryRepositoryID}
	}
}

// ValidateRepositories checks that the multi-repo fields are
// internally consistent. Caller-safe error messages — surface
// directly via HTTP 400 / bd CLI errors. Does NOT check that the
// referenced repositories exist in any registry; that's the spawn
// path's job and the right place to fail when a checkout is missing.
//
// Returns nil for the zero case (legacy beads with no repository
// information yet) — those are valid at the schema level; the spawn
// path enforces a non-empty repository at consult time.
func (w WorkItem) ValidateRepositories() error {
	if len(w.RepositoryIDs) == 0 && w.PrimaryRepositoryID == "" {
		// Legacy / unbackfilled bead. Schema-valid; spawn will
		// reject when actually trying to launch a polecat.
		return nil
	}
	if len(w.RepositoryIDs) > 0 && w.PrimaryRepositoryID == "" {
		return fmt.Errorf(
			"work item %q: repository_ids is non-empty but primary_repository_id is unset",
			w.ID)
	}
	if len(w.RepositoryIDs) > 0 {
		seen := make(map[RepositoryID]bool, len(w.RepositoryIDs))
		var found bool
		for _, id := range w.RepositoryIDs {
			if id == "" {
				return fmt.Errorf(
					"work item %q: repository_ids contains an empty entry",
					w.ID)
			}
			if seen[id] {
				return fmt.Errorf(
					"work item %q: repository_ids contains duplicate %q",
					w.ID, id)
			}
			seen[id] = true
			if id == w.PrimaryRepositoryID {
				found = true
			}
		}
		if !found {
			return fmt.Errorf(
				"work item %q: primary_repository_id %q not present in repository_ids %v",
				w.ID, w.PrimaryRepositoryID, w.RepositoryIDs)
		}
	}
	return nil
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
	// Dialect is the optional adaptor-declared agent-runtime dialect
	// ("claude", "codex", "copilot", "opencode", "gemini", …) that
	// drives per-dialect arg builders and session UI. Free-form on
	// purpose — core does NOT enum this (gm-gsh / Foolery lesson):
	// adaptors invent new dialects faster than a core enum can keep
	// up, and an unknown dialect must degrade gracefully to "generic
	// agent" rendering rather than fail a JSON decode.
	Dialect *string `json:"dialect,omitempty"`
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
