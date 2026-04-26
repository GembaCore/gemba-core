// Semantic-conflict detection (gm-s47n.4.2). The third input to the
// .4.3 Conflicts() composer alongside file-overlap (gm-s47n.4.1)
// and workspace-collision (gm-s47n.4.6).
//
// The algorithm catches the case the other two miss: two beads
// whose target sets DON'T overlap but where one's likely changes
// modify a public contract the other reads. Without this, the
// planner would dispatch them in parallel and discover the
// invalidation only on integration.
//
// Algorithm (spec §5.3, file-level projection):
//
//   1. For each bead, enumerate its target files. Call
//      SourceAnalysis.Dependents on each — the result is the set
//      of files that import / call / otherwise depend on the bead's
//      changes.
//   2. For each unordered pair (a, b): if any of a's dependent
//      files appears in b's target set, a's changes break things b
//      modifies. Symmetric: if any of b's dependent files appears
//      in a's target set, vice versa. Either direction is enough
//      to emit a SemanticConflict edge.
//   3. When SourceAnalysis returns ErrUnavailable, this entire
//      step is skipped: SemanticConflicts returns (nil,
//      ErrUnavailable) wrapped via errors.Is. The .4.3 composer
//      logs the skip and proceeds with file + workspace conflict
//      only — degraded but functional.

package planner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/MikeBengtson/gemba/internal/sourceanalysis"
)

// SemanticBeadInputs is one bead's contribution to the semantic
// graph: its id + the file targets the planner derived. Repository
// is required so cross-repo dependents don't accidentally collide
// (an "auth.go" in repo A is a different file from "auth.go" in
// repo B; the SourceAnalysis backend respects this via
// sourceanalysis.Target.Repository).
//
// Callers build this from WorkItem.targets[] after expanding any
// glob patterns to concrete files. Glob expansion lives in the
// .4.3 composer; this function expects already-resolved paths.
type SemanticBeadInputs struct {
	BeadID  string
	Targets []sourceanalysis.Target
}

// SemanticConflict is a single edge in the semantic-conflict graph.
// A and B are bead ids in stable lexicographic order so two equal
// pairs always produce the same edge (the .4.3 composer uses this
// for de-dup against the file-overlap edge set).
//
// Reason names which bead's changes touch which file the other
// bead targets. Surface text only — never parsed.
type SemanticConflict struct {
	A      string `json:"a"`
	B      string `json:"b"`
	Reason string `json:"reason"`
}

// SemanticConflicts returns every semantic-conflict edge induced
// by the bead set. Output is sorted by (A, B) so equal inputs
// always produce equal outputs — important for change-detection
// up the stack and for de-dup against the other conflict graphs.
//
// When SourceAnalysis is unavailable (errors.Is(err,
// sourceanalysis.ErrUnavailable)), returns (nil, ErrUnavailable).
// The .4.3 composer treats this as a soft skip: log + proceed
// with file + workspace conflict only.
//
// Other errors propagate as-is — they indicate a real failure in
// the SourceAnalysis backend (process crash, malformed query)
// that the operator needs to see.
//
// Implementation note: each unique (Repository, Path) pair across
// the bead set is queried at most once; results are cached for
// the duration of the call. Beads that share target files (a
// common case when multiple beads are scoped to the same module)
// pay the SA round-trip just once.
func SemanticConflicts(
	ctx context.Context,
	beads []SemanticBeadInputs,
	sa sourceanalysis.SourceAnalysis,
) ([]SemanticConflict, error) {
	if sa == nil {
		// Treat a nil binding the same as an unavailable one — the
		// .4.3 composer handles both via the same ErrUnavailable
		// branch, so we don't need a second sentinel for "no
		// backend wired up".
		return nil, sourceanalysis.ErrUnavailable
	}

	// Pre-compute each bead's target set as a quick-lookup map
	// keyed on (repo, path). Same key shape as sourceanalysis.Target
	// so the dependent-set membership check is one map read.
	beadTargets := make([]map[targetKey]struct{}, len(beads))
	for i, b := range beads {
		set := make(map[targetKey]struct{}, len(b.Targets))
		for _, t := range b.Targets {
			set[targetKey{repo: t.Repository, path: t.Path}] = struct{}{}
		}
		beadTargets[i] = set
	}

	// Cache Dependents results so a target file shared by two beads
	// is queried once. Map of seed-target → dependent-target set.
	depCache := make(map[targetKey]map[targetKey]struct{}, 32)
	dependentsOf := func(t sourceanalysis.Target) (map[targetKey]struct{}, error) {
		k := targetKey{repo: t.Repository, path: t.Path}
		if cached, ok := depCache[k]; ok {
			return cached, nil
		}
		out, err := sa.Dependents(ctx, t)
		if err != nil {
			return nil, err
		}
		set := make(map[targetKey]struct{}, len(out))
		for _, d := range out {
			set[targetKey{repo: d.Repository, path: d.Path}] = struct{}{}
		}
		depCache[k] = set
		return set, nil
	}

	// Compute each bead's "blast set" — the union of Dependents()
	// over every one of its targets. Bead a "reaches" bead b when
	// a's blast set intersects b's targets.
	blastSets := make([]map[targetKey]struct{}, len(beads))
	for i, b := range beads {
		blast := make(map[targetKey]struct{}, 0)
		for _, t := range b.Targets {
			deps, err := dependentsOf(t)
			if err != nil {
				if errors.Is(err, sourceanalysis.ErrUnavailable) {
					return nil, sourceanalysis.ErrUnavailable
				}
				return nil, fmt.Errorf("sourceanalysis.Dependents(%s/%s): %w",
					t.Repository, t.Path, err)
			}
			for k := range deps {
				blast[k] = struct{}{}
			}
		}
		blastSets[i] = blast
	}

	// Pair-up: emit one edge per pair where either direction
	// reaches. Both directions checked so the graph is undirected
	// (file overlap is undirected too — keeping the kinds aligned
	// makes the composer's union step trivial).
	out := make([]SemanticConflict, 0)
	for i := 0; i < len(beads); i++ {
		for j := i + 1; j < len(beads); j++ {
			aReachesB := intersect(blastSets[i], beadTargets[j])
			bReachesA := intersect(blastSets[j], beadTargets[i])
			if len(aReachesB) == 0 && len(bReachesA) == 0 {
				continue
			}
			// Canonicalise (A, B) so A < B lexicographically. The
			// reason is rendered against the original (i, j) order
			// so the operator-facing text reads naturally
			// regardless of input order.
			a, b := beads[i].BeadID, beads[j].BeadID
			reason := formatReason(a, b, aReachesB, bReachesA)
			if a > b {
				a, b = b, a
			}
			out = append(out, SemanticConflict{
				A:      a,
				B:      b,
				Reason: reason,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].A != out[j].A {
			return out[i].A < out[j].A
		}
		return out[i].B < out[j].B
	})
	return out, nil
}

// intersect returns the keys present in both sets, sorted by
// (repo, path) so reasons render deterministically. Returns nil
// when there's no overlap (cheap allocation-free path).
func intersect(left, right map[targetKey]struct{}) []targetKey {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	// Iterate the smaller side to minimise lookups.
	small, big := left, right
	if len(right) < len(left) {
		small, big = right, left
	}
	out := make([]targetKey, 0)
	for k := range small {
		if _, ok := big[k]; ok {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].repo != out[j].repo {
			return out[i].repo < out[j].repo
		}
		return out[i].path < out[j].path
	})
	return out
}

// formatReason renders the operator-facing explanation. Caps the
// hit list at three files per direction so a wide fan-out (a
// header file with 100 dependents) doesn't produce an unreadable
// string; the .4.3 composer surfaces the first few and lets the
// coach UI expand on demand.
func formatReason(idA, idB string, aReachesB, bReachesA []targetKey) string {
	parts := make([]string, 0, 2)
	if len(aReachesB) > 0 {
		parts = append(parts, fmt.Sprintf("%s changes touch %s targets: %s",
			idA, idB, summariseHits(aReachesB)))
	}
	if len(bReachesA) > 0 {
		parts = append(parts, fmt.Sprintf("%s changes touch %s targets: %s",
			idB, idA, summariseHits(bReachesA)))
	}
	return strings.Join(parts, "; ")
}

func summariseHits(hits []targetKey) string {
	const max = 3
	out := make([]string, 0, len(hits))
	for i, k := range hits {
		if i >= max {
			out = append(out, fmt.Sprintf("(+%d more)", len(hits)-max))
			break
		}
		out = append(out, fmt.Sprintf("%s/%s", k.repo, k.path))
	}
	return strings.Join(out, ", ")
}

// targetKey is the unexported normal form of (repo, path). Defined
// at package scope so dependentsOf and the per-bead set both
// reference the same type without playing struct-field-tag games.
type targetKey struct {
	repo string
	path string
}
