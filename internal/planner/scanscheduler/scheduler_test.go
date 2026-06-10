package scanscheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRescanner counts calls + lets a test pin the per-repo error +
// freshness response. Safe for concurrent use.
type fakeRescanner struct {
	mu        sync.Mutex
	calls     map[string]int
	rescanErr map[string]error
	freshness map[string]Freshness
	freshErr  map[string]error
	// blockUntil — when set for repo, Rescan blocks on the channel
	// before returning. Lets tests exercise the in-flight gate.
	blockUntil map[string]chan struct{}
}

func newFake() *fakeRescanner {
	return &fakeRescanner{
		calls:      make(map[string]int),
		rescanErr:  make(map[string]error),
		freshness:  make(map[string]Freshness),
		freshErr:   make(map[string]error),
		blockUntil: make(map[string]chan struct{}),
	}
}

func (f *fakeRescanner) Rescan(_ context.Context, repo string) error {
	f.mu.Lock()
	f.calls[repo]++
	block := f.blockUntil[repo]
	err := f.rescanErr[repo]
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return err
}

func (f *fakeRescanner) Freshness(_ context.Context, repo string) (Freshness, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.freshErr[repo]; e != nil {
		return Freshness{}, e
	}
	return f.freshness[repo], nil
}

func (f *fakeRescanner) callCount(repo string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[repo]
}

func newSchedAt(now time.Time, opts ...Option) (*Scheduler, *fakeRescanner) {
	r := newFake()
	clockOpts := append([]Option{WithClock(func() time.Time { return now })}, opts...)
	return New(r, DefaultConfig(), clockOpts...), r
}

func TestSubmit_RejectsInvalidTrigger(t *testing.T) {
	s, _ := newSchedAt(t0())
	if _, err := s.Submit(context.Background(), Trigger{}); !errors.Is(err, ErrInvalidTrigger) {
		t.Errorf("err = %v, want ErrInvalidTrigger", err)
	}
	if _, err := s.Submit(context.Background(), Trigger{Kind: TriggerPostMergeWave, FiredAt: t0()}); !errors.Is(err, ErrInvalidTrigger) {
		t.Errorf("missing-repo err = %v, want ErrInvalidTrigger", err)
	}
}

func TestSubmit_AsyncTriggerSchedulesAndRunsRescan(t *testing.T) {
	s, fr := newSchedAt(t0())
	d, err := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: t0(), Reason: "test",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if d.Action != DecisionScheduled {
		t.Fatalf("Action = %s, want scheduled", d.Action)
	}
	// Async — let the goroutine settle.
	for i := 0; i < 100 && fr.callCount("gt") == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	if fr.callCount("gt") != 1 {
		t.Errorf("Rescan call count = %d, want 1", fr.callCount("gt"))
	}
}

func TestSubmit_CooldownSuppressesRecentAutoTrigger(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	// First scan succeeds.
	d1, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "first",
	})
	if d1.Action != DecisionScheduled {
		t.Fatalf("first: action = %s", d1.Action)
	}
	for i := 0; i < 100 && fr.callCount("gt") == 0; i++ {
		time.Sleep(time.Millisecond)
	}

	// Second auto-trigger immediately after — should suppress.
	d2, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "second",
	})
	if d2.Action != DecisionSuppressedCooldown {
		t.Errorf("second: action = %s, want suppressed-cooldown", d2.Action)
	}
}

func TestSubmit_ManualOverrideBypassesCooldown(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	// Seed cooldown.
	_, _ = s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "seed",
	})
	for i := 0; i < 100 && fr.callCount("gt") == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	// Manual override should bypass cooldown and run inline.
	d, err := s.RunNow(context.Background(), "gt")
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if d.Action != DecisionScheduled {
		t.Errorf("RunNow action = %s, want scheduled", d.Action)
	}
	if fr.callCount("gt") != 2 {
		t.Errorf("Rescan call count = %d, want 2 (seed + manual)", fr.callCount("gt"))
	}
}

func TestSubmit_PauseSuppressesAutoTriggers(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	s.PauseAutoTriggers(now.Add(1 * time.Hour))

	// Auto-trigger should suppress.
	d, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "auto",
	})
	if d.Action != DecisionSuppressedPaused {
		t.Errorf("auto action = %s, want suppressed-paused", d.Action)
	}
	if fr.callCount("gt") != 0 {
		t.Errorf("auto trigger ran a scan despite pause: count=%d", fr.callCount("gt"))
	}

	// Manual override bypasses pause.
	if _, err := s.RunNow(context.Background(), "gt"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if fr.callCount("gt") != 1 {
		t.Errorf("manual count = %d, want 1 (pause bypass)", fr.callCount("gt"))
	}
}

func TestSubmit_InFlightCoalescesDifferentKindAndDropsDuplicate(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	// Pin the rescan to block so we have a deterministic in-flight
	// window to submit during.
	gate := make(chan struct{})
	fr.mu.Lock()
	fr.blockUntil["gt"] = gate
	fr.mu.Unlock()

	d, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "first",
	})
	if d.Action != DecisionScheduled {
		t.Fatalf("first: action = %s", d.Action)
	}
	// Wait for the goroutine to enter Rescan.
	for i := 0; i < 100; i++ {
		if fr.callCount("gt") > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Same-kind trigger during in-flight → duplicate.
	dup, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "dup",
	})
	if dup.Action != DecisionSuppressedDuplicate {
		t.Errorf("dup action = %s, want suppressed-duplicate", dup.Action)
	}

	// Different-kind trigger → coalesced.
	co, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerDriftSignal, Repo: "gt", FiredAt: now, Reason: "drift",
	})
	if co.Action != DecisionCoalesced {
		t.Errorf("co action = %s, want coalesced", co.Action)
	}

	// Release the in-flight scan.
	close(gate)
}

func TestSubmit_ReturnsEarliestNextScanForCooldown(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	_, _ = s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "seed",
	})
	for i := 0; i < 100 && fr.callCount("gt") == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	d, _ := s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "second",
	})
	if d.Action != DecisionSuppressedCooldown {
		t.Fatalf("expected cooldown suppression")
	}
	if d.EarliestNextScan.IsZero() {
		t.Error("EarliestNextScan should be populated for cooldown suppression")
	}
}

func TestActivities_RecordsSucceededScan(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	_, _ = s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "wave",
	})
	for i := 0; i < 100 && fr.callCount("gt") == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	// Allow runScan to finish recording the activity.
	time.Sleep(5 * time.Millisecond)
	acts := s.Activities()
	if len(acts) != 1 {
		t.Fatalf("activities len = %d, want 1", len(acts))
	}
	if acts[0].Status != ScanSucceeded {
		t.Errorf("status = %s, want succeeded", acts[0].Status)
	}
	if acts[0].Trigger != TriggerPostMergeWave {
		t.Errorf("trigger = %s, want post-merge-wave", acts[0].Trigger)
	}
}

func TestActivities_RecordsFailedScan(t *testing.T) {
	now := t0()
	s, fr := newSchedAt(now)
	fr.mu.Lock()
	fr.rescanErr["gt"] = errors.New("backend exploded")
	fr.mu.Unlock()
	_, _ = s.Submit(context.Background(), Trigger{
		Kind: TriggerPostMergeWave, Repo: "gt", FiredAt: now, Reason: "wave",
	})
	for i := 0; i < 100 && fr.callCount("gt") == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(5 * time.Millisecond)
	acts := s.Activities()
	if len(acts) != 1 || acts[0].Status != ScanFailed {
		t.Fatalf("activity status = %v, want failed; got %+v", acts, acts)
	}
	if acts[0].Error == "" {
		t.Error("failed activity must carry an Error string")
	}
}

func TestIsPaused_ClearsAfterUntil(t *testing.T) {
	now := t0()
	clock := atomicTime{v: now}
	s, _ := newSchedAt(now, WithClock(clock.read))
	s.PauseAutoTriggers(now.Add(time.Minute))
	paused, until := s.IsPaused()
	if !paused || !until.Equal(now.Add(time.Minute)) {
		t.Fatalf("expected paused until %s; got paused=%v until=%s", now.Add(time.Minute), paused, until)
	}
	clock.set(now.Add(2 * time.Minute))
	paused, _ = s.IsPaused()
	if paused {
		t.Error("expected pause cleared after until")
	}
}

// atomicTime is a tiny mutable clock for tests that need to advance.
type atomicTime struct {
	v time.Time
	a atomic.Int64
}

func (a *atomicTime) read() time.Time {
	if n := a.a.Load(); n > 0 {
		return time.Unix(0, n)
	}
	return a.v
}

func (a *atomicTime) set(t time.Time) {
	a.a.Store(t.UnixNano())
}
