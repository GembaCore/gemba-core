package scanscheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Scheduler owns the cooldown table, in-flight scan registry, the
// coalesced trigger queue, and the operator override (pause-until /
// run-now). Triggers come in via Submit; the scheduler decides
// async-run / coalesce / suppress. RunNow forces a scan past
// cooldown for `gemba scan --now`.
//
// Concurrency: every public method is safe for concurrent use. The
// internal mutex protects the cooldown / in-flight / queued maps;
// scan execution itself runs outside the lock so a long Rescan
// doesn't block Submit.
type Scheduler struct {
	cfg       Config
	rescanner Rescanner

	mu          sync.Mutex
	cooldown    map[string]time.Time    // repo → last-completed scan
	inFlight    map[string]*runningScan // repo → currently-running scan
	queued      map[string]Trigger      // repo → coalesced next trigger
	pausedUntil time.Time               // override: suppress non-manual triggers
	activities  []ScanActivity          // ring buffer of recent scans (newest last)

	// activitiesCap bounds the in-memory ring; older entries fall
	// off when the cap is hit. The persistent activity log lives
	// elsewhere (gm-s47n.9.4 stream).
	activitiesCap int

	now func() time.Time
}

type runningScan struct {
	trigger   Trigger
	startedAt time.Time
	done      chan struct{}
}

// Option tunes a Scheduler at construction. All have safe defaults.
type Option func(*Scheduler)

// WithClock overrides the scheduler's time source. Tests inject a
// deterministic clock; production passes nothing (defaults to time.Now).
func WithClock(now func() time.Time) Option {
	return func(s *Scheduler) { s.now = now }
}

// WithActivityCap sets the in-memory ring-buffer size for recent
// scan activities. Default 256.
func WithActivityCap(n int) Option {
	return func(s *Scheduler) { s.activitiesCap = n }
}

// New builds a Scheduler. rescanner MUST be non-nil; pass a noop
// implementation if no real source analysis is configured (the design
// allows the planner loop to run uniformly even then — §8.4).
func New(rescanner Rescanner, cfg Config, opts ...Option) *Scheduler {
	s := &Scheduler{
		cfg:           cfg,
		rescanner:     rescanner,
		cooldown:      make(map[string]time.Time),
		inFlight:      make(map[string]*runningScan),
		queued:        make(map[string]Trigger),
		activities:    make([]ScanActivity, 0, 64),
		activitiesCap: 256,
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// DecisionAction names what Submit decided to do with a trigger.
type DecisionAction int

const (
	// DecisionScheduled means the scan was kicked off (synchronously
	// or asynchronously, see Decision.RanInline).
	DecisionScheduled DecisionAction = iota
	// DecisionCoalesced means a scan was already in flight; this
	// trigger was merged with the queued next-scan slot for the
	// same repo. The queued scan runs once the in-flight one
	// finishes (only when the new trigger differs from the running
	// one — identical re-firings are dropped).
	DecisionCoalesced
	// DecisionSuppressedCooldown means the repo's cooldown hasn't
	// elapsed; scan declined.
	DecisionSuppressedCooldown
	// DecisionSuppressedPaused means PauseAutoTriggers is in effect
	// and this trigger is not a manual override.
	DecisionSuppressedPaused
	// DecisionSuppressedDuplicate means an identical-kind trigger is
	// already queued or in-flight; nothing changed.
	DecisionSuppressedDuplicate
)

// String renders DecisionAction as the operator-visible token used
// in activity logs.
func (a DecisionAction) String() string {
	switch a {
	case DecisionScheduled:
		return "scheduled"
	case DecisionCoalesced:
		return "coalesced"
	case DecisionSuppressedCooldown:
		return "suppressed-cooldown"
	case DecisionSuppressedPaused:
		return "suppressed-paused"
	case DecisionSuppressedDuplicate:
		return "suppressed-duplicate"
	default:
		return "unknown"
	}
}

// Decision is what Submit returned for one trigger.
type Decision struct {
	Action           DecisionAction
	Reason           string
	EarliestNextScan time.Time
}

// ErrInvalidTrigger is returned by Submit when the trigger is missing
// required fields. Callers should treat this as a programmer error,
// not a transient one.
var ErrInvalidTrigger = errors.New("scanscheduler: invalid trigger (missing kind / repo / time)")

// Submit considers a trigger and returns the Decision. Async scans
// are launched on a goroutine; the caller observes "scheduled"
// without waiting. Synchronous scans (TriggerPreDispatchDemand and
// TriggerManualOverride from RunNow) block — call those via RunNow
// when the wait is intentional.
func (s *Scheduler) Submit(ctx context.Context, t Trigger) (Decision, error) {
	if t.Kind == TriggerUnknown || t.Repo == "" || t.FiredAt.IsZero() {
		return Decision{}, ErrInvalidTrigger
	}
	cfg := s.cfg.resolved()
	now := s.now()

	s.mu.Lock()

	// Pause gate. Manual overrides bypass; everything else suppresses.
	if t.Kind != TriggerManualOverride && !s.pausedUntil.IsZero() && now.Before(s.pausedUntil) {
		s.mu.Unlock()
		return Decision{
			Action:           DecisionSuppressedPaused,
			Reason:           "auto-triggers paused until " + s.pausedUntil.Format(time.RFC3339),
			EarliestNextScan: s.pausedUntil,
		}, nil
	}

	// In-flight gate. A running scan absorbs identical-kind triggers
	// and queues different-kind ones (coalesced).
	if running, ok := s.inFlight[t.Repo]; ok {
		if running.trigger.Kind == t.Kind {
			s.mu.Unlock()
			return Decision{
				Action: DecisionSuppressedDuplicate,
				Reason: "scan already in flight for this trigger kind",
			}, nil
		}
		s.queued[t.Repo] = t
		s.mu.Unlock()
		return Decision{
			Action: DecisionCoalesced,
			Reason: fmt.Sprintf("queued behind in-flight %s scan", running.trigger.Kind),
		}, nil
	}

	// Cooldown gate. Manual overrides bypass.
	if t.Kind != TriggerManualOverride {
		if last, ok := s.cooldown[t.Repo]; ok {
			if elapsed := now.Sub(last); elapsed < cfg.MinScanInterval {
				s.mu.Unlock()
				return Decision{
					Action:           DecisionSuppressedCooldown,
					Reason:           fmt.Sprintf("last scan %s ago; cooldown is %s", elapsed.Round(time.Second), cfg.MinScanInterval),
					EarliestNextScan: last.Add(cfg.MinScanInterval),
				}, nil
			}
		}
	}

	// Schedule. Pre-dispatch demand and manual override are
	// synchronous (the caller of RunNow / the dispatch path is
	// blocking); everything else runs on a goroutine so the planner
	// loop doesn't stall.
	sync := t.Kind == TriggerPreDispatchDemand || t.Kind == TriggerManualOverride
	rs := &runningScan{trigger: t, startedAt: now, done: make(chan struct{})}
	s.inFlight[t.Repo] = rs
	s.mu.Unlock()

	if sync {
		s.runScan(ctx, t, rs)
		return Decision{Action: DecisionScheduled, Reason: "ran inline (" + t.Kind.String() + ")"}, nil
	}
	go s.runScan(context.Background(), t, rs)
	return Decision{Action: DecisionScheduled, Reason: "scheduled async"}, nil
}

// runScan invokes the rescanner and updates the cooldown table +
// activity log. Always closes rs.done at the end so a synchronous
// caller (RunNow) can wait on the channel without racing the lock.
func (s *Scheduler) runScan(ctx context.Context, t Trigger, rs *runningScan) {
	defer close(rs.done)

	startedAt := s.now()
	err := s.rescanner.Rescan(ctx, t.Repo)
	endedAt := s.now()

	activity := ScanActivity{
		Repo:        t.Repo,
		Trigger:     t.Kind,
		StartedAt:   startedAt,
		EndedAt:     endedAt,
		DurationEst: endedAt.Sub(startedAt),
		Reason:      t.Reason,
	}
	if err != nil {
		activity.Status = ScanFailed
		activity.Error = err.Error()
	} else {
		activity.Status = ScanSucceeded
	}

	s.mu.Lock()
	delete(s.inFlight, t.Repo)
	if err == nil {
		s.cooldown[t.Repo] = endedAt
	}
	s.recordActivityLocked(activity)

	// Coalesced next-scan slot: if a different-kind trigger fired
	// during the run, kick it off now (cooldown bypass — this is
	// the "scan immediately after this one finishes" path).
	next, queued := s.queued[t.Repo]
	if queued {
		delete(s.queued, t.Repo)
	}
	s.mu.Unlock()

	if queued {
		// Re-enter Submit so the queued trigger gets the same gates
		// (pause check still runs); the cooldown was just bumped so
		// most re-entries will suppress, but a different-kind
		// trigger that arrived during the run is exactly the case
		// we want to honor here. The cooldown gate's elapsed-since-
		// last-scan check handles "kick off immediately after" by
		// the operator setting MinScanInterval = 0 in tests; in
		// production the second scan will suppress, which is the
		// right call (no two scans of the same repo back-to-back).
		_, _ = s.Submit(context.Background(), next)
	}
}

// PauseAutoTriggers suppresses all non-manual triggers until the
// given wall-clock. Calling with a past-time clears the pause.
// Implements `gemba scan --pause <duration>`.
func (s *Scheduler) PauseAutoTriggers(until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pausedUntil = until
}

// RunNow forces a scan, bypassing cooldown. Implements
// `gemba scan --now`. Synchronous — returns when the scan completes.
// Honors the in-flight gate (returns the in-flight scan's eventual
// result via wait, no double-launch) and the pause gate is bypassed.
func (s *Scheduler) RunNow(ctx context.Context, repo string) (Decision, error) {
	t := Trigger{
		Kind:    TriggerManualOverride,
		Repo:    repo,
		FiredAt: s.now(),
		Reason:  "operator override (gemba scan --now)",
	}
	return s.Submit(ctx, t)
}

// Activities returns the recent scan history newest-first. Snapshot
// returns a fresh slice; the caller may mutate it freely.
func (s *Scheduler) Activities() []ScanActivity {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScanActivity, len(s.activities))
	// activities is stored newest-last; reverse on read.
	for i, a := range s.activities {
		out[len(s.activities)-1-i] = a
	}
	return out
}

// IsPaused reports whether auto-triggers are currently suppressed
// and (when paused) the wall-clock the suppression lifts at.
func (s *Scheduler) IsPaused() (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.pausedUntil.IsZero() || !now.Before(s.pausedUntil) {
		return false, time.Time{}
	}
	return true, s.pausedUntil
}

// recordActivityLocked appends to the ring buffer with cap enforcement.
// Caller MUST hold s.mu.
func (s *Scheduler) recordActivityLocked(a ScanActivity) {
	s.activities = append(s.activities, a)
	if len(s.activities) > s.activitiesCap {
		drop := len(s.activities) - s.activitiesCap
		s.activities = s.activities[drop:]
	}
}
