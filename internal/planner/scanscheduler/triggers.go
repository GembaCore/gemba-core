package scanscheduler

import (
	"context"
	"fmt"
	"time"
)

// TriggerKind identifies why the scheduler is being asked to consider
// a scan. The five non-Unknown values map 1:1 onto work-planning.md
// §8.1's bullets so an operator reading the activity log sees the
// same vocabulary the design doc speaks.
type TriggerKind int

const (
	// TriggerUnknown is the zero value; rejected by Submit.
	TriggerUnknown TriggerKind = iota
	// TriggerPostMergeWave fires when ≥ N beads merged in a sliding
	// window. The cumulative diff is large; the index is now
	// systematically stale across many areas.
	TriggerPostMergeWave
	// TriggerParallelCompletionBarrier fires when the last bead in a
	// parallel-safe batch finished. The batch's diffs are integrated
	// but the index reflects none of them — re-scan before the next
	// batch's conflict graph is computed.
	TriggerParallelCompletionBarrier
	// TriggerWallClockFloor fires when too long has elapsed since
	// the last successful scan with any merges in the interval. The
	// floor stops the index from drifting indefinitely on a slow day.
	TriggerWallClockFloor
	// TriggerDriftSignal fires when the source analysis backend
	// itself reports staleness via [Rescanner.Freshness]. High-priority
	// — the backend has direct visibility into its own index state.
	TriggerDriftSignal
	// TriggerPreDispatchDemand fires when the planner is about to
	// compute a conflict graph and the index is stale (gm-s47n.9.2).
	// Synchronous — the planner blocks on the scan.
	TriggerPreDispatchDemand
	// TriggerManualOverride fires from `gemba scan --now`. Bypasses
	// cooldown but still respects the in-flight + paused gates.
	TriggerManualOverride
)

// String renders TriggerKind as the operator-visible string used in
// activity log entries. Values map onto the §8.1 phrasing.
func (k TriggerKind) String() string {
	switch k {
	case TriggerPostMergeWave:
		return "post-merge-wave"
	case TriggerParallelCompletionBarrier:
		return "parallel-completion-barrier"
	case TriggerWallClockFloor:
		return "wall-clock-floor"
	case TriggerDriftSignal:
		return "drift-signal"
	case TriggerPreDispatchDemand:
		return "pre-dispatch-demand"
	case TriggerManualOverride:
		return "manual-override"
	default:
		return "unknown"
	}
}

// Trigger is one fired scheduling reason. The scheduler accepts a
// stream of these and decides whether each one warrants a scan.
type Trigger struct {
	Kind    TriggerKind
	Repo    string
	FiredAt time.Time
	// Reason carries the human-readable detail that lands in the
	// activity log ("7 merges in the last 12m"). Free-form; the
	// planner's UI surfaces it verbatim.
	Reason string
}

// Rescanner is the narrow source-analysis capability the scheduler
// needs. Decoupled from sourceanalysis.SourceAnalysis so the
// scheduler stays unit-testable with a fake and so a non-gitnexus
// backend can plug in without implementing the full analysis API.
//
// All methods are safe for concurrent use; the scheduler may invoke
// Rescan on its own goroutine while a separate caller polls Freshness
// from the trigger evaluator.
type Rescanner interface {
	// Rescan kicks off a re-index of the named repository and blocks
	// until it completes. Implementations may be slow (gitnexus
	// analyze takes minutes); the scheduler runs them on a worker
	// goroutine when async, or inline when serving a synchronous
	// pre-dispatch demand check.
	Rescan(ctx context.Context, repo string) error
	// Freshness reports the backend's current view of how stale its
	// index is for the named repository. Used by the drift trigger
	// (.9.1) and the pre-dispatch demand check (.9.2).
	Freshness(ctx context.Context, repo string) (Freshness, error)
}

// Freshness reports the source analysis backend's index state for
// one repository. The scheduler treats large IndexedAtCommit ↔ HEAD
// gaps as drift signals.
type Freshness struct {
	// Repo is the repository this freshness report covers.
	Repo string
	// IndexedAt is the wall-clock when the index last finished a
	// successful scan. Zero when no scan has ever completed.
	IndexedAt time.Time
	// HeadCommit is the repository's current HEAD as the backend
	// observed it.
	HeadCommit string
	// IndexedCommit is the commit the index was built against. When
	// HeadCommit != IndexedCommit the backend has fallen behind;
	// gap-from-HEAD is the operator-visible staleness signal.
	IndexedCommit string
	// CommitsAhead is HeadCommit's distance ahead of IndexedCommit
	// in the linearised git log. Zero when up-to-date.
	CommitsAhead int
}

// IsStale reports whether the freshness report indicates the index
// has drifted past the configured staleness threshold. A backend that
// can't measure (e.g. noop) returns IndexedAt == zero; treat as stale.
func (f Freshness) IsStale(threshold time.Duration, now time.Time) bool {
	if f.IndexedAt.IsZero() {
		return true
	}
	if f.IndexedCommit != "" && f.HeadCommit != "" && f.IndexedCommit != f.HeadCommit {
		// HEAD has moved past the indexed commit; structural
		// staleness regardless of wall-clock.
		return true
	}
	return now.Sub(f.IndexedAt) >= threshold
}

// MergeEvent is one bead merge the trigger evaluator considers when
// counting wave thresholds. Callers feed the recent-merge slice from
// the planner's activity log; out-of-window entries are filtered
// inside EvaluateTriggers so callers don't have to.
type MergeEvent struct {
	Repo string
	At   time.Time
}

// BatchCompletion records the moment the last bead in a parallel-
// safe batch finished. Triggers a scan via TriggerParallelCompletion
// Barrier — the batch's diffs are now integrated but the index
// reflects none of them.
type BatchCompletion struct {
	Repo        string
	BatchID     string
	CompletedAt time.Time
	// AllBeadsDone is the AND of every bead's status in the batch.
	// False entries are ignored — partial batches don't trigger.
	AllBeadsDone bool
}

// PlannerState is the read-only snapshot the trigger evaluator
// inspects. The planner builds it once per evaluation cycle and
// never mutates it after passing in.
type PlannerState struct {
	Now                time.Time
	Repo               string
	RecentMerges       []MergeEvent
	ParallelBatches    []BatchCompletion
	LastSuccessfulScan time.Time
	Freshness          *Freshness
}

// Config tunes the trigger thresholds. Zero values resolve to the
// design defaults (work-planning.md §8.1 / §8.3).
type Config struct {
	// PostMergeWindow is the sliding window for the post-merge-wave
	// trigger. Default 15m.
	PostMergeWindow time.Duration
	// PostMergeMinCount is the minimum merge count inside
	// PostMergeWindow that fires the wave trigger. Default 5.
	PostMergeMinCount int
	// WallClockFloor is the maximum gap between successful scans
	// when any merges have happened in the interval. Default 4h.
	WallClockFloor time.Duration
	// DriftStaleThreshold is how stale Freshness.IndexedAt must be
	// before the drift trigger fires. Default 30m.
	DriftStaleThreshold time.Duration
	// MinScanInterval is the cooldown between scans of the same
	// repo, regardless of trigger. Default 10m.
	MinScanInterval time.Duration
}

// DefaultConfig returns the design's stated defaults.
func DefaultConfig() Config {
	return Config{
		PostMergeWindow:     15 * time.Minute,
		PostMergeMinCount:   5,
		WallClockFloor:      4 * time.Hour,
		DriftStaleThreshold: 30 * time.Minute,
		MinScanInterval:     10 * time.Minute,
	}
}

// resolved fills in the design defaults for any zero field. Pure —
// returns a fresh value, doesn't mutate c.
func (c Config) resolved() Config {
	d := DefaultConfig()
	if c.PostMergeWindow > 0 {
		d.PostMergeWindow = c.PostMergeWindow
	}
	if c.PostMergeMinCount > 0 {
		d.PostMergeMinCount = c.PostMergeMinCount
	}
	if c.WallClockFloor > 0 {
		d.WallClockFloor = c.WallClockFloor
	}
	if c.DriftStaleThreshold > 0 {
		d.DriftStaleThreshold = c.DriftStaleThreshold
	}
	if c.MinScanInterval > 0 {
		d.MinScanInterval = c.MinScanInterval
	}
	return d
}

// EvaluateTriggers runs the gm-s47n.9.1 trigger battery against
// state and returns every trigger that fired. Pure — no side effects,
// no clock reads (state.Now is authoritative). The caller pipes the
// returned triggers into Scheduler.Submit.
//
// Pre-dispatch demand (.9.2) is NOT evaluated here — it's a
// synchronous check the dispatch path runs separately via
// Scheduler.MustScanBeforeDispatch.
func EvaluateTriggers(state PlannerState, cfg Config) []Trigger {
	c := cfg.resolved()
	out := make([]Trigger, 0, 4)

	// Post-merge wave: ≥ N merges inside PostMergeWindow.
	wave := mergesIn(state.RecentMerges, state.Repo, state.Now.Add(-c.PostMergeWindow), state.Now)
	if wave >= c.PostMergeMinCount {
		out = append(out, Trigger{
			Kind:    TriggerPostMergeWave,
			Repo:    state.Repo,
			FiredAt: state.Now,
			Reason: fmt.Sprintf("%d merges in the last %s (threshold %d/%s)",
				wave, c.PostMergeWindow, c.PostMergeMinCount, c.PostMergeWindow),
		})
	}

	// Parallel-completion barrier: any AllBeadsDone batch for this
	// repo whose CompletedAt is after LastSuccessfulScan. Multiple
	// batches in one evaluation collapse to a single trigger — the
	// scheduler's coalescer would drop the duplicates anyway.
	for _, b := range state.ParallelBatches {
		if b.Repo != state.Repo || !b.AllBeadsDone {
			continue
		}
		if b.CompletedAt.After(state.LastSuccessfulScan) {
			out = append(out, Trigger{
				Kind:    TriggerParallelCompletionBarrier,
				Repo:    state.Repo,
				FiredAt: state.Now,
				Reason:  fmt.Sprintf("parallel batch %s completed at %s", b.BatchID, b.CompletedAt.Format(time.RFC3339)),
			})
			break
		}
	}

	// Wall-clock floor: ≥ T since the last successful scan AND any
	// merges in the interval. The "any merges" gate keeps the floor
	// from firing on idle days.
	if !state.LastSuccessfulScan.IsZero() {
		gap := state.Now.Sub(state.LastSuccessfulScan)
		if gap >= c.WallClockFloor && mergesIn(state.RecentMerges, state.Repo, state.LastSuccessfulScan, state.Now) > 0 {
			out = append(out, Trigger{
				Kind:    TriggerWallClockFloor,
				Repo:    state.Repo,
				FiredAt: state.Now,
				Reason:  fmt.Sprintf("%s since last scan; merges have happened in the interval", gap.Round(time.Minute)),
			})
		}
	}

	// Drift signal: backend itself reports stale index.
	if state.Freshness != nil && state.Freshness.Repo == state.Repo {
		if state.Freshness.IsStale(c.DriftStaleThreshold, state.Now) {
			reason := fmt.Sprintf("backend reports stale index (%d commits ahead)",
				state.Freshness.CommitsAhead)
			if state.Freshness.IndexedAt.IsZero() {
				reason = "backend reports no completed scan yet"
			}
			out = append(out, Trigger{
				Kind:    TriggerDriftSignal,
				Repo:    state.Repo,
				FiredAt: state.Now,
				Reason:  reason,
			})
		}
	}

	return out
}

// mergesIn counts merges of repo in the half-open interval [from, to).
func mergesIn(events []MergeEvent, repo string, from, to time.Time) int {
	n := 0
	for _, e := range events {
		if e.Repo != repo {
			continue
		}
		if !e.At.Before(from) && e.At.Before(to) {
			n++
		}
	}
	return n
}
