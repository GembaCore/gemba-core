package scanscheduler

import (
	"fmt"
	"time"
)

// MissedConflict is one signal the retrospective layer (gm-s47n.8)
// hands to the grading function: a semantic conflict that was
// discovered AFTER dispatch (typically when integration broke and a
// retro pass attributed the cause to a missing dependent edge the
// stale index didn't surface).
//
// The fields name everything Grade needs to answer "could the
// scheduler have prevented this by firing a trigger that didn't
// fire?".
type MissedConflict struct {
	// Repo is the repository the missed conflict was attributed to.
	Repo string
	// DiscoveredAt is when the retro flagged the miss; ObservedAt
	// is the older "this is when the missed conflict actually
	// caused the integration break" timestamp. Grade compares
	// ObservedAt against the activity log to find the dispatch
	// that should have caught it.
	DiscoveredAt time.Time
	ObservedAt   time.Time
	// IndexedAtObservation is the freshness snapshot AT the time
	// of the missed dispatch (the retro layer captures this from
	// the dispatch record). Allows Grade to ask "was the index
	// stale right then?" without re-querying the backend.
	IndexedAtObservation Freshness
}

// GradeFinding is the verdict Grade returns for one missed conflict.
// The scheduling-rule auto-tuner (future bead) consumes Suggestion
// to nudge thresholds; the operator-facing UI surfaces Reason
// verbatim in the retrospective drawer.
type GradeFinding struct {
	// IndexWasStale reports whether the index was stale at
	// ObservedAt according to the captured Freshness.
	IndexWasStale bool
	// SuppressedTrigger names the trigger kind the scheduler
	// suppressed (or never received) that, had it fired, would
	// have caught the conflict. TriggerUnknown means "no
	// retroactively-fixable cause found" — the miss is
	// attributable to something other than scheduling.
	SuppressedTrigger TriggerKind
	// Reason is the human-readable explanation the operator-facing
	// retrospective drawer renders.
	Reason string
	// Suggestion is the concrete rule-tweak the auto-tuner could
	// apply (e.g. "lower PostMergeMinCount from 5 to 4 for repo X").
	// Empty when no actionable change is suggested.
	Suggestion string
}

// Grade evaluates a missed semantic conflict against the recent
// activity log + the scheduler's current config and returns the
// finding (gm-s47n.9.5). Pure — no side effects, no clock reads,
// no scheduler-state mutation.
//
// The function answers two questions in order:
//
//  1. Was the index stale at ObservedAt? If not, the miss has
//     nothing to do with scheduling.
//  2. If yes, was a trigger that would have prevented the miss
//     suppressed by debouncing or never fired? Walks the recent
//     activity log + the trigger battery in reverse.
//
// Returns one finding per missed conflict; an aggregator on top can
// fold multiple findings to spot patterns across the retrospective
// stream (a separate bead — this function only inspects one miss).
func (s *Scheduler) Grade(miss MissedConflict, recentActivities []ScanActivity) GradeFinding {
	cfg := s.cfg.resolved()

	// Question 1: was the index stale at the missed dispatch?
	if !miss.IndexedAtObservation.IsStale(cfg.DriftStaleThreshold, miss.ObservedAt) {
		return GradeFinding{
			IndexWasStale:     false,
			SuppressedTrigger: TriggerUnknown,
			Reason:            "index was fresh at observation; miss not attributable to scheduling",
		}
	}

	// Question 2: which trigger would have caught it?
	//
	// Walk recentActivities (newest-first if Activities() format)
	// looking for the most-recent suppression on this repo. The
	// scheduler's Submit returns suppression decisions through its
	// caller; today we don't persist those — a follow-up bead
	// (gm-s47n.9.6) will add a suppressed-trigger log so Grade can
	// pinpoint exactly which firing the cooldown ate. For now the
	// inference is structural: which trigger COULD have fired.
	suppressed := suspectSuppressedTrigger(miss, recentActivities, cfg)
	switch suppressed {
	case TriggerWallClockFloor:
		gap := miss.ObservedAt.Sub(miss.IndexedAtObservation.IndexedAt)
		return GradeFinding{
			IndexWasStale:     true,
			SuppressedTrigger: TriggerWallClockFloor,
			Reason: fmt.Sprintf("index was %s old at observation; wall-clock-floor (%s) should have fired",
				gap.Round(time.Minute), cfg.WallClockFloor),
			Suggestion: fmt.Sprintf("lower WallClockFloor from %s toward %s",
				cfg.WallClockFloor, gap.Round(time.Minute)/2),
		}
	case TriggerDriftSignal:
		return GradeFinding{
			IndexWasStale:     true,
			SuppressedTrigger: TriggerDriftSignal,
			Reason: fmt.Sprintf("backend reported drift (%d commits ahead) but threshold (%s) hadn't been crossed",
				miss.IndexedAtObservation.CommitsAhead, cfg.DriftStaleThreshold),
			Suggestion: fmt.Sprintf("lower DriftStaleThreshold from %s",
				cfg.DriftStaleThreshold),
		}
	case TriggerPreDispatchDemand:
		return GradeFinding{
			IndexWasStale:     true,
			SuppressedTrigger: TriggerPreDispatchDemand,
			Reason:            "candidates had no semantic-conflict history at dispatch but the conflict materialised post-hoc; consider extending the history window",
			Suggestion:        "broaden the CandidatesHaveSemanticHistory lookback to include adjacent concept areas",
		}
	default:
		return GradeFinding{
			IndexWasStale:     true,
			SuppressedTrigger: TriggerUnknown,
			Reason: fmt.Sprintf("index was stale (%d commits ahead) at observation; no specific trigger can be attributed",
				miss.IndexedAtObservation.CommitsAhead),
		}
	}
}

// suspectSuppressedTrigger picks the most-likely trigger to blame
// for the miss. The hierarchy:
//
//  1. Drift signal — the backend itself reported staleness; if its
//     threshold hadn't been crossed but the gap was meaningful,
//     that's the first thing to tune.
//  2. Wall-clock floor — index was older than the floor at
//     observation.
//  3. Pre-dispatch demand — index was stale and a scan WOULD have
//     fired if the demand check had recognised the candidate set
//     as semantically risky.
//
// Returns TriggerUnknown when no rule cleanly applies.
func suspectSuppressedTrigger(miss MissedConflict, _ []ScanActivity, cfg Config) TriggerKind {
	f := miss.IndexedAtObservation
	gap := miss.ObservedAt.Sub(f.IndexedAt)

	// Drift signal: backend had visible drift but the threshold
	// hadn't been crossed. Tightest-fitting attribution.
	if f.CommitsAhead > 0 && gap < cfg.DriftStaleThreshold {
		return TriggerDriftSignal
	}

	// Wall-clock floor: index older than the floor.
	if !f.IndexedAt.IsZero() && gap >= cfg.WallClockFloor {
		return TriggerWallClockFloor
	}

	// Pre-dispatch demand: stale + observation suggests we should
	// have caught it via the demand check.
	if f.IsStale(cfg.DriftStaleThreshold, miss.ObservedAt) {
		return TriggerPreDispatchDemand
	}

	return TriggerUnknown
}
