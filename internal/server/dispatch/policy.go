// Package dispatch holds the server-side routing policy that decides
// whether a new bead should co-locate in an existing intra-parallel
// session or trigger a fresh pane spawn (gm-root.16.4).
//
// The policy is deliberately separate from the OrchestrationPlane
// adaptor so a future router (auto-dispatcher, scheduling agent) can
// reuse the picker without going through HTTP.
package dispatch

import "time"

// Candidate is a session the policy can consider routing a new bead
// to. Sourced from native.OrchestrationPlane.SessionsByAgentType (or
// any equivalent on a future adaptor).
type Candidate struct {
	SessionID   string
	PaneID      string
	StartedAt   time.Time
	InFlight    int
	MaxParallel int
}

// HasCapacity reports whether the candidate's pane can carry one more
// bead. Hard cap is enforced again at dispatch time inside the
// adaptor — this just spares the round trip on the obviously-full case.
func (c Candidate) HasCapacity() bool {
	return c.InFlight < c.MaxParallel
}

// Pick selects the best reuse candidate for a new bead, or returns
// ("", false) when none qualifies (caller falls back to fresh spawn).
//
// Tiebreak order:
//  1. Prefer the candidate with the lowest InFlight (level the load).
//  2. Among ties, prefer the oldest StartedAt (the longest-running
//     pane is presumably the most stable; a brand-new pane may still
//     be initializing the prompt).
//
// Both rules are deterministic so two identical inputs route the same
// way every time, which keeps the SPA's mental model legible.
func Pick(cands []Candidate) (paneID string, ok bool) {
	var best *Candidate
	for i := range cands {
		c := cands[i]
		if !c.HasCapacity() {
			continue
		}
		if best == nil || better(c, *best) {
			cc := c
			best = &cc
		}
	}
	if best == nil {
		return "", false
	}
	return best.PaneID, true
}

func better(a, b Candidate) bool {
	if a.InFlight != b.InFlight {
		return a.InFlight < b.InFlight
	}
	return a.StartedAt.Before(b.StartedAt)
}
