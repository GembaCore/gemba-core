// Leverage scorer (gm-v5z2.5, work-planning.md §4 Layer 3.3).
//
// "How many other beads does picking THIS bead unblock?"
// Selection (gm-v5z2.7) composes Leverage with Affinity so the
// planner doesn't get stuck on cheap concept-matched leaves while
// a high-leverage bead waits.

package scoring

import (
	"math"
	"sort"

	"github.com/MikeBengtson/gemba/internal/core"
)

// DefaultLeverageK is the saturation knob: score = 1 - exp(-k * weight).
// k=1.0 maps single-blocker beads to ~0.63 and 5+-blocker beads
// asymptote toward 1; tuned so leaf beads stay at 0 and one
// downstream is already a meaningful bump.
const DefaultLeverageK = 1.0

// DependencyView is the narrow read surface Leverage walks. Two
// methods kept separate so an in-memory test fake stays trivial
// AND a future per-rig dolt-backed view can wire in cleanly.
//
// Direct (non-transitive) blocks are enough for v1: Selection
// re-runs every dispatch tick, so a chain "A blocks B blocks C"
// surfaces as A's leverage growing as the planner clears B.
type DependencyView interface {
	// Blocks returns the bead ids the given bead directly blocks
	// (i.e. for each Relationship{Kind: blocks, From: beadID, To: x},
	// emit x). Closed/done beads are filtered upstream so the
	// caller hands Leverage only candidates that still matter.
	Blocks(beadID core.WorkItemID) []core.WorkItemID
	// IsOpen reports whether the given bead still consumes
	// leverage. Closed / canceled / done beads return false so
	// Leverage doesn't credit a bead for clearing already-done
	// work.
	IsOpen(beadID core.WorkItemID) bool
}

// LeverageResult is the score + the justification list. The
// blocked beads are surfaced so coach-mode UI / audit logs can
// render "picking gm-7 unblocks gm-8, gm-12, gm-19" instead of
// just a scalar.
type LeverageResult struct {
	Score   float64           `json:"score"`
	Weight  int               `json:"weight"`
	Blocked []core.WorkItemID `json:"blocked,omitempty"`
}

// Leverage computes the score for the given bead. Walks transitive
// blocks (BFS) so a bead at the head of a chain gets credit for
// the full downstream span, not just the immediate child. Counts
// only OPEN beads — clearing already-closed downstreams is not
// leverage. k is the saturation knob; pass 0 for DefaultLeverageK.
//
// Output is deterministic: Blocked is sorted by bead id so equal
// inputs always produce equal outputs (the audit log relies on
// it).
func Leverage(beadID core.WorkItemID, view DependencyView, k float64) LeverageResult {
	if k <= 0 {
		k = DefaultLeverageK
	}
	if view == nil || beadID == "" {
		return LeverageResult{}
	}

	visited := map[core.WorkItemID]bool{beadID: true}
	var blocked []core.WorkItemID
	queue := []core.WorkItemID{beadID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range view.Blocks(cur) {
			if child == "" || visited[child] {
				continue
			}
			visited[child] = true
			if !view.IsOpen(child) {
				continue
			}
			blocked = append(blocked, child)
			queue = append(queue, child)
		}
	}
	sort.Slice(blocked, func(i, j int) bool { return blocked[i] < blocked[j] })

	weight := len(blocked)
	score := 1 - math.Exp(-k*float64(weight))
	return LeverageResult{Score: score, Weight: weight, Blocked: blocked}
}
