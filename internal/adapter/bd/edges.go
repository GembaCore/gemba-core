package bd

import (
	"strings"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Edge mapping for gm-e6.2: Beads's 7 native edge types split into the
// three core RelationshipKinds that every adaptor must support (blocks,
// parent_child, relates_to — DD-9) and four Beads-native extensions that
// only render when the SPA has loaded the beads/ extension bundle.
//
// The bd CLI spells edge types with hyphens ("parent-child",
// "relates-to", "discovered-from") while the rest of the Gemba surface
// prefers underscores. Every lookup here normalizes the token first so a
// caller with either spelling lands on the same mapping entry.

// normalizeBeadsEdgeType folds a raw bd edge token onto the canonical
// underscore form used by this package. Empty in, empty out — callers
// treat the empty string as "no edge kind specified" and fall back to
// blocks (bd's own default, per `bd dep add --help`).
func normalizeBeadsEdgeType(t string) string {
	return strings.ReplaceAll(strings.TrimSpace(strings.ToLower(t)), "-", "_")
}

// coreEdgeByBeadsType maps Beads's native edge types to the three core
// RelationshipKinds. The table keys are the normalized (underscore) form;
// coreEdgeForBeadsType runs normalization before lookup so hyphen inputs
// from bd's CLI round-trip transparently.
//
// "related" is accepted as an alias for relates_to because bd's own CLI
// accepted that spelling in earlier versions and real fixtures still
// carry it.
var coreEdgeByBeadsType = map[string]core.RelationshipKind{
	"blocks":       core.RelBlocks,
	"parent_child": core.RelParentChild,
	"relates_to":   core.RelRelatesTo,
	"related":      core.RelRelatesTo,
}

// beadsEdgeExtensions declares the Beads-native edges that have NO core
// counterpart. The capability-negotiation UI (gm-e11.4) reads this list
// and only renders the matching edge when the beads/ extension bundle is
// loaded; other adaptors just see "relates_to" on the wire per DD-9.
//
// Names are prefixed with "beads:" so the manifest stays self-describing
// — a UI that doesn't know the prefix still won't collide with a core
// edge kind because the core kinds are unprefixed strings ("blocks",
// "parent_child", "relates_to").
var beadsEdgeExtensions = []core.EdgeExtension{
	{
		Name:        "beads:discovered_from",
		Directed:    true,
		Inverse:     "beads:discovered_by",
		Description: "Target bead was discovered while working on the source bead (bd's discovered-from edge).",
	},
	{
		Name:        "beads:waits_for",
		Directed:    true,
		Inverse:     "beads:waited_on_by",
		Description: "Source bead pauses until the target resolves (soft async dependency distinct from hard blocks).",
	},
	{
		Name:        "beads:replies_to",
		Directed:    true,
		Inverse:     "beads:replied_by",
		Description: "Source bead is a reply / follow-up message to the target (mail, review, comment thread).",
	},
	{
		Name:        "beads:conditional_blocks",
		Directed:    true,
		Inverse:     "beads:conditional_blocked_by",
		Description: "Source bead blocks the target only under a stated condition; record the predicate on the relationship metadata.",
	},
}

// coreEdgeForBeadsType resolves a bd edge token onto a core
// RelationshipKind. Empty bd types default to blocks because that is
// bd's own CLI default (see `bd dep add --help`). Unknown tokens return
// ("", false) so the caller can decide whether to surface them as an
// extension or fall through to relates_to.
func coreEdgeForBeadsType(t string) (core.RelationshipKind, bool) {
	n := normalizeBeadsEdgeType(t)
	if n == "" {
		return core.RelBlocks, true
	}
	k, ok := coreEdgeByBeadsType[n]
	return k, ok
}

// relationshipsFromBead projects the bd dependencies/dependents arrays
// on one bead onto the subset of core.Relationships that the 3 core edge
// kinds can express. Extension edges (discovered_from, waits_for, …) do
// NOT appear here — they survive on Custom["beads:dependencies"] /
// ["beads:dependents"] so the beads/ UI extension can render them
// without core taking a dependency on adaptor vocabulary.
//
// Direction semantics: a `dependencies` entry means "this bead depends
// on X" via the given type. For RelBlocks that's "X blocks this bead"
// → From=X, To=self. For RelParentChild it's "X is the parent of this
// bead" → From=X, To=self. For RelRelatesTo the direction is advisory
// only; we still emit From=X, To=self so walking the graph from the
// dependency side matches walking it from the dependent side. The
// `dependents` array flips both: "Y depends on this bead" → From=self,
// To=Y.
func relationshipsFromBead(b *Bead, self core.WorkItemID, prefix string) []core.Relationship {
	if len(b.Dependencies) == 0 && len(b.Dependents) == 0 {
		return nil
	}
	rels := make([]core.Relationship, 0, len(b.Dependencies)+len(b.Dependents))
	for _, e := range b.Dependencies {
		kind, ok := coreEdgeForBeadsType(e.edgeType())
		if !ok {
			continue
		}
		other := buildWorkItemID(prefix, e.linkedID())
		if other == "" {
			continue
		}
		rels = append(rels, core.Relationship{Kind: kind, From: other, To: self})
	}
	for _, e := range b.Dependents {
		kind, ok := coreEdgeForBeadsType(e.edgeType())
		if !ok {
			continue
		}
		other := buildWorkItemID(prefix, e.linkedID())
		if other == "" {
			continue
		}
		rels = append(rels, core.Relationship{Kind: kind, From: self, To: other})
	}
	if len(rels) == 0 {
		return nil
	}
	return rels
}

// edgeType returns the edge type the BeadEdge carries, tolerating both
// shapes bd emits: `dependencies` rows spell it `type`, `dependents`
// rows spell it `dependency_type`. One of the two is always populated
// for a well-formed row.
func (e BeadEdge) edgeType() string {
	if e.Type != "" {
		return e.Type
	}
	return e.DependencyType
}

// linkedID returns the id of the OTHER bead the edge points at. For a
// `dependencies` row that is DependsOnID; for a `dependents` row bd
// flattens the linked bead and the id lands on the ID field.
func (e BeadEdge) linkedID() string {
	if e.DependsOnID != "" {
		return e.DependsOnID
	}
	return e.ID
}
