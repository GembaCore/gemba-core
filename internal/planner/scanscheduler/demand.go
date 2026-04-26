package scanscheduler

import (
	"context"
	"time"
)

// DispatchDemandState is the slice of planner state the synchronous
// pre-dispatch demand check (gm-s47n.9.2) needs. Built once per
// dispatch cycle right before the planner computes its conflict
// graph.
type DispatchDemandState struct {
	Now  time.Time
	Repo string
	// CandidatesHaveSemanticHistory mirrors the spec's "any candidate
	// bead has semantic conflicts in its concept area in past
	// retrospectives" gate. The retrospective layer (gm-s47n.8) feeds
	// it; an honest false here means "we have no history reason to
	// suspect a missed conflict on this dispatch".
	//
	// When false, a stale index is acceptable for *this* dispatch —
	// the planner accepts a degraded semantic-conflict check rather
	// than blocking dispatch on a long scan that may not surface
	// anything. When true, the index MUST be fresh.
	CandidatesHaveSemanticHistory bool
}

// DemandDecision reports whether a synchronous scan must run before
// dispatch. The scheduler uses Reason to populate the activity-log
// entry when it kicks the scan off.
type DemandDecision struct {
	// Required is true when the planner MUST block dispatch and run
	// a scan first. False means dispatch may proceed with the
	// current (possibly stale) index.
	Required bool
	// Reason is the human-readable explanation for the decision.
	// Surfaced in the activity log alongside the scan record so an
	// operator can see why a dispatch stalled (or didn't).
	Reason string
}

// MustScanBeforeDispatch is the gm-s47n.9.2 synchronous demand check.
// Returns Required=true when the planner has to block dispatch on a
// scan first — when (a) the index is stale (per Freshness.IsStale)
// AND (b) the candidate set has a semantic-conflict history that
// makes a stale index dangerous.
//
// The scheduler's Submit / RunNow path can be used by the dispatch
// caller when Required=true to actually run the synchronous scan;
// this function only answers the demand question.
//
// Pure — no side effects, no clock reads (state.Now is authoritative).
func (s *Scheduler) MustScanBeforeDispatch(ctx context.Context, state DispatchDemandState) DemandDecision {
	cfg := s.cfg.resolved()

	// Without semantic history we accept the staleness rather than
	// pay the latency. The planner's degraded-check banner will
	// surface that the index is stale.
	if !state.CandidatesHaveSemanticHistory {
		return DemandDecision{
			Required: false,
			Reason:   "no candidate has prior semantic conflicts; degraded check acceptable",
		}
	}

	// Probe the backend for current freshness. A backend that errors
	// is treated as stale — better to scan than dispatch blind.
	freshness, err := s.rescanner.Freshness(ctx, state.Repo)
	if err != nil {
		return DemandDecision{
			Required: true,
			Reason:   "freshness probe failed; treating index as stale: " + err.Error(),
		}
	}

	if freshness.IsStale(cfg.DriftStaleThreshold, state.Now) {
		reason := "index stale; candidates have semantic-conflict history"
		if !freshness.IndexedAt.IsZero() {
			reason = "index stale (" + state.Now.Sub(freshness.IndexedAt).Round(time.Minute).String() +
				" since last scan); candidates have semantic-conflict history"
		}
		return DemandDecision{Required: true, Reason: reason}
	}

	return DemandDecision{
		Required: false,
		Reason:   "index is fresh enough for the candidate set",
	}
}
