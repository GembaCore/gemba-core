// Conflict scorer + batching. See doc.go for context.

package conflicts

import (
	"sort"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner/targets"
)

// Bead is the planner's input shape: a work item id plus the targets
// it touches. The conflicts package deliberately does NOT import
// core.WorkItem — projection from WorkItem to Bead lives wherever
// the planner is wired in (today: a future helper that reads the
// WorkItem.targets field gm-s47n.1 will land). Decoupling keeps
// this scorer testable without dragging the whole core package
// into the test surface.
type Bead struct {
	ID      core.WorkItemID
	Targets []targets.Pattern
}

// ReasonKind tags one source of conflict between two beads. A single
// Edge can carry multiple reasons — two beads might both touch the
// same files (target-overlap) AND be routed to the same workspace
// (workspace-collision); both are surfaced so the planner can show
// the operator the full picture.
type ReasonKind int

const (
	// ReasonTargetOverlap — the beads' target globs touch the same
	// concrete file path (gm-s47n.4.1). Detected by targets.CompareSets.
	ReasonTargetOverlap ReasonKind = iota
	// ReasonTargetOverlapMaybe — the pure target analysis returned
	// Maybe (mid-segment wildcards on both sides). Surfaced as a
	// distinct reason so the operator can see "we couldn't decide
	// without enumerating; treat as risky" vs a confirmed overlap.
	ReasonTargetOverlapMaybe
	// ReasonSemantic — gm-s47n.4.2: dependents of one bead's likely
	// public symbols overlap the other bead's targets. Detected by
	// the optional SemanticDetector.
	ReasonSemantic
	// ReasonWorkspaceCollision — gm-s47n.4.6: the two beads route to
	// the same (repo, branch) or worktree path, so they can't run
	// in parallel even if their files are disjoint. Detected by the
	// optional WorkspaceCollisionDetector.
	ReasonWorkspaceCollision
)

func (k ReasonKind) String() string {
	switch k {
	case ReasonTargetOverlap:
		return "target_overlap"
	case ReasonTargetOverlapMaybe:
		return "target_overlap_maybe"
	case ReasonSemantic:
		return "semantic"
	case ReasonWorkspaceCollision:
		return "workspace_collision"
	default:
		return "unknown"
	}
}

// Reason is one piece of evidence on an Edge.
type Reason struct {
	Kind   ReasonKind
	Detail string // optional human-readable context
}

// Edge is one conflict edge in the Graph. From < To lexicographically;
// Conflicts canonicalises pair order so adjacency lookups don't need
// to test both directions.
type Edge struct {
	From    core.WorkItemID
	To      core.WorkItemID
	Reasons []Reason
}

// Graph is the conflict graph for a bead set. Implementation note:
// the data structure is small (number of beads × pairwise edges) so
// adjacency is computed on demand rather than precomputed. The
// planner's typical input is dozens of beads, not thousands.
type Graph struct {
	Beads []core.WorkItemID
	Edges []Edge
}

// Neighbors returns every bead id connected to `id` by an edge.
// Order is deterministic (sorted by id) so callers diffing graphs
// across runs see stable output.
func (g Graph) Neighbors(id core.WorkItemID) []core.WorkItemID {
	out := []core.WorkItemID{}
	for _, e := range g.Edges {
		switch {
		case e.From == id:
			out = append(out, e.To)
		case e.To == id:
			out = append(out, e.From)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// HasEdge reports whether two beads are in conflict, returning the
// edge's reasons when true. Lookup is order-insensitive.
func (g Graph) HasEdge(a, b core.WorkItemID) (bool, []Reason) {
	for _, e := range g.Edges {
		if (e.From == a && e.To == b) || (e.From == b && e.To == a) {
			return true, e.Reasons
		}
	}
	return false, nil
}

// Batch is a set of beads with no internal conflicts — safe to
// dispatch in parallel. Order within a batch is deterministic
// (sorted by id).
type Batch struct {
	Beads []core.WorkItemID
}

// Batches returns a parallel-safe partition of g.Beads. Greedy
// first-fit: walk the sorted bead list once, drop each bead into
// the first existing batch where it conflicts with no member; if no
// such batch exists, open a new batch. Stable: the first batch
// holds the lowest-id "anchor" beads, the second batch picks up
// what wouldn't fit, etc.
//
// Why first-fit and not optimal coloring: the planner UX cares about
// "give me a parallel-safe handful to dispatch right now"; the
// first-fit greedy answers that with a deterministic, predictable
// shape. Operators can iterate (close one batch, re-Conflicts the
// remaining) without thrashing the dispatch order. Min-coloring
// would be optimal for theoretical max-parallelism but unstable
// across small input changes.
func (g Graph) Batches() []Batch {
	beads := append([]core.WorkItemID(nil), g.Beads...)
	sort.Slice(beads, func(i, j int) bool { return beads[i] < beads[j] })
	batches := []Batch{}
	for _, id := range beads {
		placed := false
		for bi := range batches {
			if !batchConflictsWith(batches[bi], id, g) {
				batches[bi].Beads = append(batches[bi].Beads, id)
				placed = true
				break
			}
		}
		if !placed {
			batches = append(batches, Batch{Beads: []core.WorkItemID{id}})
		}
	}
	return batches
}

func batchConflictsWith(b Batch, id core.WorkItemID, g Graph) bool {
	for _, member := range b.Beads {
		if has, _ := g.HasEdge(id, member); has {
			return true
		}
	}
	return false
}

// SemanticDetector is the optional gm-s47n.4.2 hook. Return
// (overlap=true, evidence) when the dependents of one bead's likely
// public symbols overlap the other bead's targets. Implementations
// own the source-analysis cache; the conflicts package is
// orchestration-only.
type SemanticDetector interface {
	Detect(a, b Bead) (overlap bool, evidence string, err error)
}

// WorkspaceCollisionDetector is the optional gm-s47n.4.6 hook.
// Return true when both beads route to the same (repo, branch) or
// worktree path. Cross-references live OperationalContexts that the
// detector implementation owns.
type WorkspaceCollisionDetector interface {
	Detect(a, b core.WorkItemID) (overlap bool, evidence string, err error)
}

// Options bundle the configuration knobs for Conflicts. All fields
// are optional; the zero value runs target-overlap analysis only,
// without an FS safety net (Maybe results land as
// ReasonTargetOverlapMaybe).
type Options struct {
	// FS resolves Maybe results from the target overlap analysis by
	// enumerating glob expansions on a real filesystem. nil → leave
	// Maybe results as ReasonTargetOverlapMaybe.
	FS targets.Filesystem
	// Semantic is the gm-s47n.4.2 detector. nil → skip.
	Semantic SemanticDetector
	// WorkspaceCollision is the gm-s47n.4.6 detector. nil → skip.
	WorkspaceCollision WorkspaceCollisionDetector
	// MaybeIsConflict, when true, escalates ReasonTargetOverlapMaybe
	// to ReasonTargetOverlap — i.e. callers that can't run the FS
	// safety net can opt into the conservative interpretation
	// ("treat 'might overlap' as a hard conflict") without losing
	// the Maybe distinction in the audit detail.
	MaybeIsConflict bool
}

// Conflicts is the bead-set scorer: returns the conflict graph for
// the given beads. The graph contains every input bead in g.Beads
// (so a bead with no conflicts is still represented) and one Edge
// per conflicting pair, canonicalised From < To.
//
// O(N²) over beads in the worst case; for the planner's typical
// input (dozens of beads) this is fine. A future bead may grow a
// trie-backed prefix index (the bead's title mentions it) to bring
// target-overlap detection sublinear; the API here doesn't change.
func Conflicts(beads []Bead, opts Options) (Graph, error) {
	ids := make([]core.WorkItemID, len(beads))
	for i, b := range beads {
		ids[i] = b.ID
	}
	g := Graph{Beads: ids}

	for i := 0; i < len(beads); i++ {
		for j := i + 1; j < len(beads); j++ {
			edge, err := classifyPair(beads[i], beads[j], opts)
			if err != nil {
				return Graph{}, err
			}
			if len(edge.Reasons) > 0 {
				g.Edges = append(g.Edges, edge)
			}
		}
	}
	// Canonicalise edge order: sort by (From, To). Graph diffs across
	// runs become comparable by-eye and via tests.
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		return g.Edges[i].To < g.Edges[j].To
	})
	return g, nil
}

// classifyPair runs every enabled detector against (a, b) and
// returns the resulting Edge (with no Reasons if the pair is
// conflict-free). Edge.From / Edge.To are canonicalised so the
// caller doesn't have to.
func classifyPair(a, b Bead, opts Options) (Edge, error) {
	from, to := a.ID, b.ID
	if to < from {
		from, to = to, from
	}
	edge := Edge{From: from, To: to}

	// Target overlap (always runs — gm-s47n.4.1).
	if len(a.Targets) > 0 && len(b.Targets) > 0 {
		var (
			result targets.Result
			err    error
		)
		if opts.FS != nil {
			result, err = targets.CompareSetsWith(a.Targets, b.Targets, opts.FS)
		} else {
			result = targets.CompareSets(a.Targets, b.Targets)
		}
		if err != nil {
			return Edge{}, err
		}
		switch result {
		case targets.Overlap:
			edge.Reasons = append(edge.Reasons, Reason{
				Kind:   ReasonTargetOverlap,
				Detail: "targets share at least one concrete file path",
			})
		case targets.Maybe:
			kind := ReasonTargetOverlapMaybe
			detail := "targets MAY share a path; FS enumeration would resolve"
			if opts.MaybeIsConflict {
				kind = ReasonTargetOverlap
				detail = "targets MAY share a path; treating as conflict (MaybeIsConflict=true)"
			}
			edge.Reasons = append(edge.Reasons, Reason{Kind: kind, Detail: detail})
		}
	}

	if opts.Semantic != nil {
		ok, evidence, err := opts.Semantic.Detect(a, b)
		if err != nil {
			return Edge{}, err
		}
		if ok {
			edge.Reasons = append(edge.Reasons, Reason{
				Kind:   ReasonSemantic,
				Detail: evidence,
			})
		}
	}

	if opts.WorkspaceCollision != nil {
		ok, evidence, err := opts.WorkspaceCollision.Detect(a.ID, b.ID)
		if err != nil {
			return Edge{}, err
		}
		if ok {
			edge.Reasons = append(edge.Reasons, Reason{
				Kind:   ReasonWorkspaceCollision,
				Detail: evidence,
			})
		}
	}

	return edge, nil
}
