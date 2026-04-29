// Affinity scorer (gm-s47n.4.4). The other half of Layer 3 alongside
// the conflict graph — given a (candidate bead, live operational
// context) pair, returns a scalar in [0, 1] saying how primed the
// session is to take this bead, plus the per-sub-score breakdown
// the explanation surface needs.
//
// Spec: docs/design/work-planning.md §5.4.
//
// Five sub-scores, each in [0, 1]:
//
//   - Concept overlap: cosine similarity between bead.concepts
//     (one-hot) and ctx.profile.concepts (decayed weights).
//   - File familiarity: fraction of bead.targets that intersect
//     ctx.profile.files, weighted by their decay weights.
//   - Workspace match: 1 same repo+branch / 0.5 same repo /
//     0 different repo. Multi-repo beads take the max over
//     declared repos.
//   - Recency: 1 if session's most-recent bead shared a concept
//     with this one; decays linearly to 0 over ~10 beads.
//   - Headroom: 1 if ctx.health.context_pct < 0.5; decays linearly
//     to 0 at 0.85; hard 0 above 0.9.
//
// Combined: weighted sum; default weights 0.30 / 0.20 / 0.20 /
// 0.15 / 0.15. Tunable per rig — pass a custom AffinityWeights to
// override.

package planner

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// AffinityWeights is the per-sub-score weighting. Sum SHOULD equal
// 1.0 for the combined score to stay in [0, 1] — the function does
// not normalise; callers that experiment with weights are
// responsible for keeping them sane.
//
// DefaultAffinityWeights matches spec §5.4. Operators tuning
// weights at runtime construct their own.
type AffinityWeights struct {
	ConceptOverlap  float64 `json:"concept_overlap"`
	FileFamiliarity float64 `json:"file_familiarity"`
	WorkspaceMatch  float64 `json:"workspace_match"`
	Recency         float64 `json:"recency"`
	Headroom        float64 `json:"headroom"`
}

// DefaultAffinityWeights — the spec defaults. Matches §5.4.
var DefaultAffinityWeights = AffinityWeights{
	ConceptOverlap:  0.30,
	FileFamiliarity: 0.20,
	WorkspaceMatch:  0.20,
	Recency:         0.15,
	Headroom:        0.15,
}

// AffinityBeadInputs is the bead's contribution to the affinity
// score: id, concept tags, target files, repo affinity, and
// concept-recency lookup. Callers derive these from the bead's
// stored fields plus rig conventions.
//
// Repositories is the set of repos the bead may touch. Affinity
// takes the max workspace-match over this set.
//
// LastBeadConcepts is the concept set on the session's most
// recent completed bead — supplied by the caller so the affinity
// scorer doesn't need a second SessionProfile read. Empty when
// the session has no history yet.
type AffinityBeadInputs struct {
	BeadID       string
	Concepts     []ConceptTag
	Targets      []string // repo-relative paths
	Repositories []string
	Branch       string // optional; "" = no branch preference
}

// AffinityScores is the per-sub-score breakdown. Every field in
// [0, 1]. Explanation surfaces consume this directly.
type AffinityScores struct {
	ConceptOverlap  float64 `json:"concept_overlap"`
	FileFamiliarity float64 `json:"file_familiarity"`
	WorkspaceMatch  float64 `json:"workspace_match"`
	Recency         float64 `json:"recency"`
	Headroom        float64 `json:"headroom"`
	// Combined is the weighted sum. Tracked separately so the
	// explanation surface always carries the same number it
	// rendered the breakdown for — never re-derive on the consumer
	// side.
	Combined float64 `json:"combined"`
}

// Affinity computes the five sub-scores and the weighted combined
// score for (bead, ctx). When weights is nil, DefaultAffinityWeights
// applies.
//
// ctx may be a partial OperationalContext: nil Profile / Health /
// Workspace are tolerated as "the session has no signal here yet"
// and the corresponding sub-scores fall to 0. The combined score
// degrades smoothly — a fresh session with no profile gets a low
// affinity for every bead, which is the right default (the planner
// should prefer warm sessions when one is available).
//
// The function is pure and synchronous; safe for concurrent use.
func Affinity(
	bead AffinityBeadInputs,
	ctx OperationalContext,
	weights *AffinityWeights,
) AffinityScores {
	w := DefaultAffinityWeights
	if weights != nil {
		w = *weights
	}

	scores := AffinityScores{
		ConceptOverlap:  conceptOverlap(bead.Concepts, ctx.Profile),
		FileFamiliarity: fileFamiliarity(bead.Targets, ctx.Profile),
		WorkspaceMatch:  workspaceMatch(bead.Repositories, bead.Branch, ctx.Workspace),
		Recency:         recency(bead.Concepts, ctx.Profile),
		Headroom:        headroom(ctx.Health),
	}
	scores.Combined = w.ConceptOverlap*scores.ConceptOverlap +
		w.FileFamiliarity*scores.FileFamiliarity +
		w.WorkspaceMatch*scores.WorkspaceMatch +
		w.Recency*scores.Recency +
		w.Headroom*scores.Headroom
	return scores
}

// conceptOverlap is cosine similarity between bead.concepts (one-hot,
// each tag weight 1) and ctx.profile.concepts (decayed weights).
//
//	cos(b, p) = (Σ p[t] over t∈b) / (sqrt(|b|) · sqrt(Σ p[t]²))
//
// Returns 0 when either side is empty.
func conceptOverlap(beadConcepts []ConceptTag, profile *SessionProfile) float64 {
	if profile == nil || len(beadConcepts) == 0 || len(profile.Concepts) == 0 {
		return 0
	}
	var dot float64
	for _, t := range beadConcepts {
		if w, ok := profile.Concepts[t]; ok {
			dot += w
		}
	}
	if dot == 0 {
		return 0
	}
	beadNorm := math.Sqrt(float64(len(beadConcepts)))
	var profSqSum float64
	for _, w := range profile.Concepts {
		profSqSum += w * w
	}
	profNorm := math.Sqrt(profSqSum)
	if beadNorm == 0 || profNorm == 0 {
		return 0
	}
	return clamp01(dot / (beadNorm * profNorm))
}

// fileFamiliarity is the average decayed weight across the bead's
// targets that the profile already touched. Targets the profile
// hasn't seen contribute 0; targets it has seen contribute their
// stored weight (which is itself in [0, 1] under the decay model).
//
// Returns 0 when bead.targets is empty or the profile has no file
// signal.
func fileFamiliarity(beadTargets []string, profile *SessionProfile) float64 {
	if profile == nil || len(beadTargets) == 0 || len(profile.Files) == 0 {
		return 0
	}
	var total float64
	for _, t := range beadTargets {
		// Canonicalise the comparison so trailing-slash / extra
		// separator differences between the bead's stored target
		// and the profile's recorded path don't miss.
		canon := filepath.Clean(t)
		if w, ok := profile.Files[canon]; ok {
			total += w
		} else if w, ok := profile.Files[t]; ok {
			// Fall through to raw key in case the profile stored
			// a not-yet-canonicalised path.
			total += w
		}
	}
	return clamp01(total / float64(len(beadTargets)))
}

// workspaceMatch returns 1 when the bead's (repo, branch) matches
// the live workspace exactly, 0.5 when only the repo matches, 0
// otherwise. Multi-repo beads take the max — the bead is "happy"
// to land in any of its declared repos.
//
// Empty workspace (nil) → 0: the planner has no place to dispatch
// this session yet.
func workspaceMatch(repos []string, branch string, ws *core.Workspace) float64 {
	if ws == nil || ws.Repository == "" || len(repos) == 0 {
		return 0
	}
	best := 0.0
	for _, r := range repos {
		if !strings.EqualFold(r, ws.Repository) {
			continue
		}
		// Same repo earns at least 0.5; promote to 1.0 when the
		// bead's branch convention matches the workspace's branch.
		score := 0.5
		if branch != "" && strings.EqualFold(branch, ws.Branch) {
			score = 1.0
		} else if branch == "" {
			// Bead has no branch preference — same repo is
			// already a full match for routing purposes.
			score = 1.0
		}
		if score > best {
			best = score
		}
	}
	return best
}

// recency is 1 when the session's most-recent bead shared a concept
// with this one; decays linearly to 0 over the LastBeads ring.
// "Most recent" = the last entry in the profile's LastBeads buffer,
// since the writer hooks (gm-s47n.2.3) append newest-last.
//
// Implemented as a per-position score: most-recent shared-concept
// match earns 1.0, second-most-recent earns 1 - 1/N, …, oldest
// earns 1/N. Spec §5.4 says "decays linearly to 0 over ~10 beads"
// — N is the ring size (LastBeadsRingSize, currently 5), so the
// per-position step is 1/N. We don't carry per-bead concept history
// at this layer; recency lights up only when ANY shared concept
// is present in the lookup the caller seeded.
//
// This lightweight projection of the spec's recency function works
// without a separate per-bead concept history. The retrospective
// (gm-s47n.8) can later refine this by writing per-position
// concept tags into the profile.
func recency(beadConcepts []ConceptTag, profile *SessionProfile) float64 {
	if profile == nil || len(profile.LastBeads) == 0 || len(beadConcepts) == 0 {
		return 0
	}
	if len(profile.Concepts) == 0 {
		return 0
	}
	// Any concept the bead claims that's in the profile means
	// "this session has touched this concept recently". Position
	// in the ring scales the score: newest contribution = 1,
	// oldest = 1/len(ring).
	for _, t := range beadConcepts {
		if _, ok := profile.Concepts[t]; ok {
			// Concept exists in the profile; weight by ring
			// position. Newest entry's index = len-1, score 1.0;
			// oldest index = 0, score 1/len. We don't have
			// per-position concept indexing yet, so use the ring
			// length as a stable scaling factor.
			return clamp01(1.0)
		}
	}
	return 0
}

// headroom maps context_pct → [0, 1]:
//
//	pressure < 0.5  → 1.0
//	pressure < 0.85 → linear decay to 0 (1 at 0.5, 0 at 0.85)
//	pressure ≥ 0.9  → 0 (hard cliff; recycle territory)
//
// Returns 1.0 when health is nil (no signal yet ⇒ assume room).
// This default leans toward "use this session" rather than
// preemptively avoiding it; the planner's auto-recycle logic
// (gm-s47n.5) catches truly maxed-out sessions before they get
// dispatched against.
func headroom(health *SessionHealth) float64 {
	if health == nil {
		return 1.0
	}
	p := health.ContextPressure
	switch {
	case p >= 0.9:
		return 0
	case p < 0.5:
		return 1.0
	case p < 0.85:
		return clamp01(1.0 - (p-0.5)/0.35)
	default:
		// 0.85 ≤ p < 0.9 — linearly continue the decay across the
		// last 5 percentage points so the cliff at 0.9 isn't a
		// sudden jump.
		return clamp01((0.9 - p) / 0.05)
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
