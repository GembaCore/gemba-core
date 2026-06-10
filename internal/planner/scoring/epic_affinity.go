// Epic-affinity sub-score (gm-v5z2.5, work-planning.md §4 Layer 3.2).
//
// "Finish what you started" bias. Boosts candidates that share
// the parent epic of beads this session has CLOSED this turn,
// decays per turn so the planner doesn't over-fit to one epic
// forever, hard 0 once a different epic has been contiguously
// worked.
//
// Pure function over an EpicStreak snapshot the caller materialises
// from session history.

package scoring

import "github.com/GembaCore/gemba-core/core"

// DefaultEpicAffinityDecayPerTurn is the per-turn decrement applied
// after a streak's first close. With the default 0.1 a session
// shipping 5 in the same epic would still score the next sibling
// at 0.6; the 11th sibling drops to 0. Operators tune via
// EpicStreak.DecayPerTurn.
const DefaultEpicAffinityDecayPerTurn = 0.1

// EpicStreak is the session-level snapshot the scorer reads. The
// caller walks the session's recent close history and produces
// this from whichever store holds it (session_profiles, an
// in-memory log, the assignment table).
//
// CurrentEpicID is the parent epic of the most-recent close.
// Empty means "no streak" — the session has either closed nothing
// yet, or the most recent close had no epic — score is 0 for
// every candidate.
//
// ContiguousCount is the number of consecutive closes in
// CurrentEpicID, counting the most recent. >=1 when CurrentEpicID
// is non-empty. Each per-turn decay multiplies by the count - 1
// (so first sibling pick scores at full strength; later picks
// fade).
//
// DecayPerTurn overrides the default decay. Zero falls through to
// DefaultEpicAffinityDecayPerTurn.
type EpicStreak struct {
	CurrentEpicID   core.WorkItemID
	ContiguousCount int
	DecayPerTurn    float64
}

// EpicAffinity returns the sibling-bias score for a candidate.
// candidateEpicID is the parent epic of the bead being scored
// (caller resolves from the bead's parent_child relationships).
//
// Score in [0, 1]:
//
//	1.0  → first sibling pick after a streak began
//	0.0  → candidate has no epic, OR streak is empty, OR
//	       streak's most recent epic differs from candidate's
//
// "hard 0 once a different epic has been contiguously worked" is
// expressed by the caller resetting ContiguousCount to 1 when
// the streak switches epics — so the candidate is only "in the
// streak" when its epic matches CurrentEpicID.
func EpicAffinity(candidateEpicID core.WorkItemID, streak EpicStreak) float64 {
	if candidateEpicID == "" || streak.CurrentEpicID == "" {
		return 0
	}
	if candidateEpicID != streak.CurrentEpicID {
		return 0
	}
	if streak.ContiguousCount <= 0 {
		return 0
	}
	decay := streak.DecayPerTurn
	if decay <= 0 {
		decay = DefaultEpicAffinityDecayPerTurn
	}
	// First sibling pick (count==1) scores at full strength;
	// subsequent picks fade by decay per turn.
	score := 1 - decay*float64(streak.ContiguousCount-1)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// BuildEpicStreak walks a session's recent-close list (newest first)
// and produces the EpicStreak snapshot EpicAffinity consumes.
// "Newest first" matters: the caller sorted-by-closed-at-desc.
// Closes with empty epic ids are skipped (they neither start nor
// break a streak — treated as "no signal").
//
// The streak ends as soon as a close in a different epic is
// encountered; subsequent closes are not considered.
func BuildEpicStreak(closesNewestFirst []core.WorkItemID) EpicStreak {
	var current core.WorkItemID
	count := 0
	for _, epic := range closesNewestFirst {
		if epic == "" {
			continue
		}
		if current == "" {
			current = epic
			count = 1
			continue
		}
		if epic != current {
			break
		}
		count++
	}
	return EpicStreak{CurrentEpicID: current, ContiguousCount: count}
}
