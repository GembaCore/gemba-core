// Package reconcile is the pure planner for the ASDD interception layer.
//
// Given a parsed spec.md, the current set of bd beads labeled spec:<slug>,
// and the lockfile that maps anchors to bead IDs, Plan returns the create /
// update / orphan operations that an apply step (gm-v0sp.6) will execute.
//
// This package has NO side effects: it never invokes bd mutating commands,
// never writes to disk, and never touches the network. Reading bead state
// goes through a BeadLister (default impl shells out to `bd list --json`).
//
// Field ownership:
//
//	spec owns: Title, Description, Type, Parent, Priority, edges-within-spec
//	bead owns: Status, Assignee, Notes, Comments, edges-outside-spec
//
// Updates only propose changes on spec-owned fields. Bead-owned fields are
// surfaced as informational context (FieldChange.Informational = true) so
// the apply UI can show drift without acting on it.
//
// Determinism: identical inputs produce byte-identical JSON. All Op slices
// are sorted by Anchor.
package reconcile

import (
	"fmt"
	"sort"

	"github.com/GembaCore/gemba-core/internal/spec/lockfile"
	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

// OpKind enumerates the three operation classes.
type OpKind string

const (
	OpCreate OpKind = "create"
	OpUpdate OpKind = "update"
	OpOrphan OpKind = "orphan"
)

// FieldChange describes a proposed change to one bead field.
type FieldChange struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Informational bool   `json:"informational,omitempty"`
}

// Op is a single create / update / orphan operation.
type Op struct {
	Kind        OpKind                 `json:"kind"`
	Anchor      string                 `json:"anchor"`
	BeadID      string                 `json:"bead_id,omitempty"` // empty for create
	Title       string                 `json:"title"`
	Type        string                 `json:"type"` // milestone | epic | story
	Parent      string                 `json:"parent,omitempty"`
	Priority    string                 `json:"priority,omitempty"`
	ContentHash string                 `json:"content_hash,omitempty"`
	FieldDiffs  map[string]FieldChange `json:"field_diffs,omitempty"`
	Reason      string                 `json:"reason"`
	NonceStale  bool                   `json:"nonce_stale,omitempty"`
	Rationale   string                 `json:"rationale,omitempty"` // e.g. "--prune" for orphans
}

// Plan is the full diff between spec and beads.
type Plan struct {
	SpecID     string `json:"spec_id"`
	Nonce      string `json:"nonce"`
	LockNonce  string `json:"lock_nonce"`
	NonceStale bool   `json:"nonce_stale"`
	Creates    []Op   `json:"creates"`
	Updates    []Op   `json:"updates"`
	Orphans    []Op   `json:"orphans"`
}

// Empty reports whether the plan proposes no mutations.
func (p *Plan) Empty() bool {
	return len(p.Creates) == 0 && len(p.Updates) == 0 && len(p.Orphans) == 0
}

// Diff is the canonical planner entry point. It diffs spec against
// existing beads and the lockfile and returns a *Plan whose Op slices are
// sorted by anchor.
//
// existing may be nil (treated as empty). lock may be nil (treated as a
// fresh lockfile with no mappings). When lock.Nonce != spec.Nonce every Op
// carries NonceStale = true and Plan.NonceStale = true; the apply command
// will refuse to act under those conditions.
//
// Diff is named to avoid a clash with the *Plan struct type — callers can
// read the API as "compute a Plan by Diffing spec against beads + lock".
func Diff(spec *parser.Spec, existing []Bead, lock *lockfile.Lock) (*Plan, error) {
	return runPlan(spec, existing, lock)
}

func runPlan(spec *parser.Spec, existing []Bead, lock *lockfile.Lock) (*Plan, error) {
	if spec == nil {
		return nil, fmt.Errorf("reconcile: nil spec")
	}

	plan := &Plan{
		SpecID: spec.Frontmatter.SpecID,
		Nonce:  spec.Frontmatter.Nonce,
	}
	if lock != nil {
		plan.LockNonce = lock.Nonce
		plan.NonceStale = lock.Nonce != spec.Frontmatter.Nonce
	} else {
		plan.NonceStale = false
	}

	// Index existing beads by anchor and by ID.
	beadByID := map[string]*Bead{}
	for i := range existing {
		b := &existing[i]
		beadByID[b.ID] = b
	}

	// Index lockfile mappings by anchor.
	mappingByAnchor := map[string]lockfile.Mapping{}
	if lock != nil {
		for _, m := range lock.Mappings {
			mappingByAnchor[m.Anchor] = m
		}
	}

	// Walk every spec node in a stable order.
	type node struct {
		anchor   string
		title    string
		typ      string
		parent   string
		priority string
		hash     string
	}
	var nodes []node
	for _, m := range spec.Milestones {
		nodes = append(nodes, node{m.Anchor, m.Title, "milestone", "", "", lockfile.HashMilestone(m)})
	}
	for _, e := range spec.Epics {
		nodes = append(nodes, node{e.Anchor, e.Title, "epic", "", "", lockfile.HashEpic(e)})
	}
	for _, s := range spec.Stories {
		nodes = append(nodes, node{s.Anchor, s.Title, "story", s.Parent, s.Priority, lockfile.HashStory(s)})
	}

	specAnchors := map[string]struct{}{}
	for _, n := range nodes {
		specAnchors[n.anchor] = struct{}{}
	}

	for _, n := range nodes {
		mapping, mapped := mappingByAnchor[n.anchor]
		if !mapped {
			plan.Creates = append(plan.Creates, Op{
				Kind:        OpCreate,
				Anchor:      n.anchor,
				Title:       n.title,
				Type:        n.typ,
				Parent:      n.parent,
				Priority:    n.priority,
				ContentHash: n.hash,
				Reason:      "anchor missing from lockfile",
				NonceStale:  plan.NonceStale,
			})
			continue
		}

		// Mapping exists. Compare content hash.
		if mapping.ContentHash == n.hash {
			continue
		}

		op := Op{
			Kind:        OpUpdate,
			Anchor:      n.anchor,
			BeadID:      mapping.BeadID,
			Title:       n.title,
			Type:        n.typ,
			Parent:      n.parent,
			Priority:    n.priority,
			ContentHash: n.hash,
			Reason:      "content hash drift",
			NonceStale:  plan.NonceStale,
		}

		// Compute field-level diffs against the existing bead, if available.
		if bead := beadByID[mapping.BeadID]; bead != nil {
			op.FieldDiffs = computeFieldDiffs(n.title, n.typ, n.parent, n.priority, bead)
		} else {
			// Bead missing from the listing — surface as informational
			// rationale; apply will recreate (still an update from the
			// lockfile's POV).
			op.Reason = "content hash drift; bead not found in listing"
		}

		plan.Updates = append(plan.Updates, op)
	}

	// Orphans: anchors in lockfile but not in spec.
	for anchor, mapping := range mappingByAnchor {
		if _, ok := specAnchors[anchor]; ok {
			continue
		}
		op := Op{
			Kind:        OpOrphan,
			Anchor:      anchor,
			BeadID:      mapping.BeadID,
			ContentHash: mapping.ContentHash,
			Reason:      "anchor missing from spec",
			Rationale:   "--prune",
			NonceStale:  plan.NonceStale,
		}
		if bead := beadByID[mapping.BeadID]; bead != nil {
			op.Title = bead.Title
			op.Type = bead.Type
		}
		plan.Orphans = append(plan.Orphans, op)
	}

	sortOps(plan.Creates)
	sortOps(plan.Updates)
	sortOps(plan.Orphans)
	return plan, nil
}

// computeFieldDiffs compares spec-owned fields and surfaces bead-owned
// fields as informational entries.
func computeFieldDiffs(title, typ, parent, priority string, bead *Bead) map[string]FieldChange {
	out := map[string]FieldChange{}
	if bead.Title != title {
		out["title"] = FieldChange{From: bead.Title, To: title}
	}
	if bead.Type != "" && bead.Type != typ {
		out["type"] = FieldChange{From: bead.Type, To: typ}
	}
	if bead.Parent != parent {
		out["parent"] = FieldChange{From: bead.Parent, To: parent}
	}
	if bead.Priority != priority {
		out["priority"] = FieldChange{From: bead.Priority, To: priority}
	}
	// Bead-owned fields surface as informational context (never as
	// proposed changes). The apply step ignores informational entries.
	if bead.Status != "" {
		out["status"] = FieldChange{From: bead.Status, To: bead.Status, Informational: true}
	}
	if bead.Assignee != "" {
		out["assignee"] = FieldChange{From: bead.Assignee, To: bead.Assignee, Informational: true}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortOps(ops []Op) {
	sort.SliceStable(ops, func(i, j int) bool {
		return ops[i].Anchor < ops[j].Anchor
	})
}
