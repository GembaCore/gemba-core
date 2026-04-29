// Auto-dispatch daemon tests (gm-s47n.6.3).

package autodispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
)

// ── Fakes ───────────────────────────────────────────────────────

type fakeIdle struct {
	sessions []planner.OperationalContext
	err      error
}

func (f fakeIdle) ListIdle(_ context.Context) ([]planner.OperationalContext, error) {
	return f.sessions, f.err
}

type fakeLive struct {
	sessions []planner.OperationalContext
	err      error
}

func (f fakeLive) ListLive(_ context.Context) ([]planner.OperationalContext, error) {
	return f.sessions, f.err
}

type fakeReady struct {
	beads []ReadyBead
	err   error
}

func (f fakeReady) ReadySet(_ context.Context) ([]ReadyBead, error) {
	return f.beads, f.err
}

type fakeDispatcher struct {
	calls []dispatchCall
	err   error
}

type dispatchCall struct {
	sessionID string
	beadID    core.WorkItemID
}

func (f *fakeDispatcher) Dispatch(_ context.Context, sessionID string, beadID core.WorkItemID) error {
	f.calls = append(f.calls, dispatchCall{sessionID: sessionID, beadID: beadID})
	return f.err
}

type fakeRecycler struct {
	calls []string
	err   error
}

func (f *fakeRecycler) Recycle(_ context.Context, sessionID string) error {
	f.calls = append(f.calls, sessionID)
	return f.err
}

// ── Helpers ────────────────────────────────────────────────────

func fixedClock(t *testing.T) func() time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, "2026-04-26T17:00:00Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return func() time.Time { return tt }
}

func sessCtx(id, repo string, profile *planner.SessionProfile, health *planner.SessionHealth) planner.OperationalContext {
	return planner.OperationalContext{
		Session:   &core.Session{ID: id, Status: core.SessionReady},
		Agent:     &core.AgentRef{ID: core.AgentID(id + "-agent")},
		Workspace: &core.Workspace{ID: id + "-ws", Repository: repo, Branch: "main"},
		Profile:   profile,
		Health:    health,
	}
}

func bead(id, repo string, concepts ...planner.ConceptTag) ReadyBead {
	return ReadyBead{
		BeadID:       core.WorkItemID(id),
		Concepts:     concepts,
		Repositories: []string{repo},
		Branch:       "main",
	}
}

func newDaemon(
	idle []planner.OperationalContext,
	live []planner.OperationalContext,
	ready []ReadyBead,
	gate *planner.DispatchGate,
	dsp *fakeDispatcher,
) *Daemon {
	return &Daemon{
		Idle:       fakeIdle{sessions: idle},
		Live:       fakeLive{sessions: live},
		Ready:      fakeReady{beads: ready},
		Dispatcher: dsp,
		Gate:       gate,
	}
}

func enabledGate() *planner.DispatchGate {
	g := planner.NewDispatchGate(planner.DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: time.Minute,
		MaxConcurrent:         8,
	})
	return g
}

// ── Happy path ──────────────────────────────────────────────────

func TestTick_DispatchesHighestAffinityBead(t *testing.T) {
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{ContextPressure: 0.2}),
	}
	ready := []ReadyBead{
		bead("gm-1", "gemba", "auth"), // perfect concept match
		bead("gm-2", "gemba", "unrelated"),
	}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(r.Actions))
	}
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Errorf("outcome = %q", r.Actions[0].Outcome)
	}
	if r.Actions[0].BeadID != "gm-1" {
		t.Errorf("picked bead = %s, want gm-1 (highest affinity)", r.Actions[0].BeadID)
	}
	if len(dsp.calls) != 1 || dsp.calls[0].beadID != "gm-1" {
		t.Errorf("dispatcher calls = %+v", dsp.calls)
	}
}

func TestTick_SkipsBeadConflictingWithLiveSession(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	// Live session is already in (gemba/main), so any bead targeting
	// the same repo+branch is conflict-adjacent.
	live := []planner.OperationalContext{
		sessCtx("sess-live", "gemba", nil, nil),
	}
	ready := []ReadyBead{
		bead("gm-conflicting", "gemba"),
		bead("gm-clear", "other-repo"),
	}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, live, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 || r.Actions[0].BeadID != "gm-clear" {
		t.Errorf("expected gm-clear; got %+v", r.Actions)
	}
}

func TestTick_NoEligibleBeadWhenAllConflict(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	live := []planner.OperationalContext{
		sessCtx("sess-live", "gemba", nil, nil),
	}
	ready := []ReadyBead{
		bead("gm-1", "gemba"),
		bead("gm-2", "gemba"),
	}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, live, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeNoEligibleBead {
		t.Errorf("expected no_eligible_bead; got %+v", r.Actions)
	}
	if len(dsp.calls) != 0 {
		t.Errorf("dispatcher should not be called; got %+v", dsp.calls)
	}
}

// ── Gate ────────────────────────────────────────────────────────

func TestTick_KillSwitchBlocksDispatch(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba")}
	dsp := &fakeDispatcher{}
	gate := planner.NewDispatchGate(planner.DispatchPolicy{Enabled: false})
	d := newDaemon(idle, nil, ready, gate, dsp)
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeBlockedByGate {
		t.Errorf("expected blocked_by_gate; got %+v", r.Actions)
	}
	if r.Actions[0].Reason != "auto-dispatch disabled" {
		t.Errorf("reason = %q", r.Actions[0].Reason)
	}
	if len(dsp.calls) != 0 {
		t.Errorf("dispatcher should not be called; got %+v", dsp.calls)
	}
}

func TestTick_RateLimitBlocksRepeatDispatch(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba")}
	dsp := &fakeDispatcher{}
	gate := enabledGate()
	gate.RecordDispatch("sess-1", time.Now()) // pretend a recent dispatch happened

	d := newDaemon(idle, nil, ready, gate, dsp)
	d.Now = func() time.Time { return time.Now() } // close to "now"

	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeBlockedByGate {
		t.Errorf("expected blocked_by_gate; got %+v", r.Actions)
	}
	if r.Actions[0].Reason != "per-session rate limit" {
		t.Errorf("reason = %q", r.Actions[0].Reason)
	}
}

// ── Recycle ─────────────────────────────────────────────────────

func TestTick_RecyclesWhenPressureHighAndAffinityBelowMedian(t *testing.T) {
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	// High-pressure session. Conflict-blocking the high-affinity
	// "auth" beads forces the top eligible pick to be the
	// "unrelated" bead, which scores below the median of the full
	// ready set — rule 1 fires.
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{ContextPressure: 0.95}),
	}
	// Live session in "other-repo" forces the auth beads (which
	// are also in "other-repo") into conflict.
	live := []planner.OperationalContext{
		{
			Session:   &core.Session{ID: "sess-live", Status: core.SessionWorking},
			Workspace: &core.Workspace{Repository: "other-repo", Branch: "main"},
		},
	}
	ready := []ReadyBead{
		bead("gm-low", "different-repo", "unrelated"),
		bead("gm-high1", "other-repo", "auth"),
		bead("gm-high2", "other-repo", "auth"),
	}
	dsp := &fakeDispatcher{}
	rec := &fakeRecycler{}
	d := newDaemon(idle, live, ready, enabledGate(), dsp)
	d.Recycler = rec
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeRecycled {
		t.Fatalf("expected recycled; got %+v", r.Actions)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "sess-1" {
		t.Errorf("recycler calls = %+v", rec.calls)
	}
	if len(dsp.calls) != 0 {
		t.Errorf("dispatcher should NOT fire on recycle; got %+v", dsp.calls)
	}
}

func TestTick_RecycleWithoutRecyclerStillRecordsAction(t *testing.T) {
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{ContextPressure: 0.95}),
	}
	live := []planner.OperationalContext{
		{
			Session:   &core.Session{ID: "sess-live", Status: core.SessionWorking},
			Workspace: &core.Workspace{Repository: "other-repo", Branch: "main"},
		},
	}
	ready := []ReadyBead{
		bead("gm-low", "different-repo", "unrelated"),
		bead("gm-high1", "other-repo", "auth"),
		bead("gm-high2", "other-repo", "auth"),
	}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, live, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	// Recycler intentionally nil — daemon should still emit the
	// recycle action (caller may not have a handoff hook bound yet).

	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeRecycled {
		t.Errorf("expected recycled action with nil recycler; got %+v", r.Actions)
	}
}

// ── No idle / no ready ──────────────────────────────────────────

func TestTick_EmptyIdleSetReturnsNoActions(t *testing.T) {
	dsp := &fakeDispatcher{}
	d := newDaemon(nil, nil, []ReadyBead{bead("gm-1", "gemba")}, enabledGate(), dsp)
	d.Now = fixedClock(t)
	r := d.Tick(context.Background())
	if len(r.Actions) != 0 {
		t.Errorf("expected no actions; got %+v", r.Actions)
	}
}

func TestTick_EmptyReadySetReturnsNoEligibleBead(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, nil, enabledGate(), dsp)
	d.Now = fixedClock(t)
	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeNoEligibleBead {
		t.Errorf("expected no_eligible_bead; got %+v", r.Actions)
	}
}

// ── Errors ───────────────────────────────────────────────────────

func TestTick_PropagatesIdleListError(t *testing.T) {
	d := &Daemon{
		Idle:       fakeIdle{err: errors.New("boom")},
		Live:       fakeLive{},
		Ready:      fakeReady{},
		Dispatcher: &fakeDispatcher{},
		Gate:       enabledGate(),
		Now:        fixedClock(t),
	}
	r := d.Tick(context.Background())
	if r.Err == nil {
		t.Fatal("expected error from ListIdle")
	}
}

func TestTick_PropagatesReadyError(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	d := &Daemon{
		Idle:       fakeIdle{sessions: idle},
		Live:       fakeLive{},
		Ready:      fakeReady{err: errors.New("read fail")},
		Dispatcher: &fakeDispatcher{},
		Gate:       enabledGate(),
		Now:        fixedClock(t),
	}
	r := d.Tick(context.Background())
	if r.Err == nil {
		t.Fatal("expected error from ReadySet")
	}
}

func TestTick_DispatcherErrorYieldsErrorAction(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba")}
	dsp := &fakeDispatcher{err: errors.New("plane down")}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeError {
		t.Fatalf("expected error action; got %+v", r.Actions)
	}
	if r.Actions[0].Reason == "" {
		t.Errorf("error action must carry a reason")
	}
}

// ── Validation ──────────────────────────────────────────────────

func TestTick_ValidatesRequiredDeps(t *testing.T) {
	cases := []struct {
		name string
		mut  func(d *Daemon)
	}{
		{"no idle", func(d *Daemon) { d.Idle = nil }},
		{"no live", func(d *Daemon) { d.Live = nil }},
		{"no ready", func(d *Daemon) { d.Ready = nil }},
		{"no dispatcher", func(d *Daemon) { d.Dispatcher = nil }},
		{"no gate", func(d *Daemon) { d.Gate = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDaemon(nil, nil, nil, enabledGate(), &fakeDispatcher{})
			tc.mut(d)
			r := d.Tick(context.Background())
			if r.Err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

// ── Run loop ────────────────────────────────────────────────────

func TestRun_StopsOnContextCancel(t *testing.T) {
	d := newDaemon(nil, nil, nil, enabledGate(), &fakeDispatcher{})
	d.Now = fixedClock(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // stop immediately
	err := d.Run(ctx, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestRun_RejectsZeroPeriod(t *testing.T) {
	d := newDaemon(nil, nil, nil, enabledGate(), &fakeDispatcher{})
	if err := d.Run(context.Background(), 0); err == nil {
		t.Error("expected error for period=0")
	}
}

func TestRun_RejectsInvalidDaemon(t *testing.T) {
	d := &Daemon{}
	err := d.Run(context.Background(), time.Millisecond)
	if err == nil {
		t.Error("expected validation error from Run")
	}
}

// ── Fairness boost ──────────────────────────────────────────────

func TestFairness_BoostIsLinearAndCapped(t *testing.T) {
	f := FairnessConfig{PerHour: 0.05, Max: 0.30}
	cases := []struct {
		name string
		age  time.Duration
		want float64
	}{
		{"zero age", 0, 0},
		{"negative age", -time.Hour, 0},
		{"one hour", time.Hour, 0.05},
		{"three hours", 3 * time.Hour, 0.15},
		{"capped at max", 12 * time.Hour, 0.30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.Boost(tc.age)
			if got < tc.want-1e-9 || got > tc.want+1e-9 {
				t.Errorf("Boost(%s) = %v, want %v", tc.age, got, tc.want)
			}
		})
	}
}

func TestFairness_DisabledWhenPerHourZero(t *testing.T) {
	f := FairnessConfig{Max: 1.0}
	if got := f.Boost(10 * time.Hour); got != 0 {
		t.Errorf("zero PerHour must disable; got %v", got)
	}
}

func TestFairness_NoCapWhenMaxZero(t *testing.T) {
	f := FairnessConfig{PerHour: 0.05}
	if got := f.Boost(100 * time.Hour); got != 5.0 {
		t.Errorf("no-cap should keep ramping; got %v want 5.0", got)
	}
}

func TestTick_FairnessBoostFlipsRanking(t *testing.T) {
	// Aged bead loses on raw affinity but wins on boost. Test
	// uses an aggressive boost (PerHour=0.10, Max=0.60) so the
	// arithmetic is unambiguous: fresh "auth" bead scores ~0.45,
	// old "unrelated" bead scores ~0.15 + 0.60 boost = 0.75.
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{}),
	}
	old := bead("gm-old", "other", "unrelated")
	old.Age = 6 * time.Hour
	fresh := bead("gm-fresh", "other", "auth")
	ready := []ReadyBead{fresh, old}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	d.Fairness = &FairnessConfig{PerHour: 0.10, Max: 0.60}

	r := d.Tick(context.Background())
	if len(r.Actions) != 1 || r.Actions[0].Outcome != OutcomeDispatched {
		t.Fatalf("expected dispatched; got %+v", r.Actions)
	}
	if r.Actions[0].BeadID != "gm-old" {
		t.Errorf("aged bead should win after boost; picked %s", r.Actions[0].BeadID)
	}
}

func TestTick_FairnessOptOutPreservesRawRanking(t *testing.T) {
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{}),
	}
	old := bead("gm-old", "other", "unrelated")
	old.Age = 6 * time.Hour
	fresh := bead("gm-fresh", "other", "auth")
	ready := []ReadyBead{fresh, old}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	d.Fairness = &FairnessConfig{} // explicit opt-out

	r := d.Tick(context.Background())
	if r.Actions[0].BeadID != "gm-fresh" {
		t.Errorf("with fairness off, raw affinity should win; picked %s", r.Actions[0].BeadID)
	}
}

func TestTick_RecycleUsesRawScoresNotBoosted(t *testing.T) {
	// Confirm fairness boost does NOT bypass recycle. Setup: high
	// pressure, conflict-block the high-affinity beads so the
	// eligible top is below the median (rule 1 conditions). Even
	// with a large boost, recycle should still fire because the
	// median is computed over raw scores.
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{ContextPressure: 0.95}),
	}
	live := []planner.OperationalContext{
		{
			Session:   &core.Session{ID: "sess-live", Status: core.SessionWorking},
			Workspace: &core.Workspace{Repository: "other-repo", Branch: "main"},
		},
	}
	low := bead("gm-low", "different-repo", "unrelated")
	low.Age = 24 * time.Hour // huge fairness boost
	ready := []ReadyBead{
		low,
		bead("gm-high1", "other-repo", "auth"),
		bead("gm-high2", "other-repo", "auth"),
	}
	dsp := &fakeDispatcher{}
	rec := &fakeRecycler{}
	d := newDaemon(idle, live, ready, enabledGate(), dsp)
	d.Recycler = rec
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Actions[0].Outcome != OutcomeRecycled {
		t.Errorf("fairness must not bypass recycle; got %+v", r.Actions[0])
	}
}

// ── Rate gate updates on dispatch ───────────────────────────────

func TestTick_DispatchAdvancesRateLimit(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", nil, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba")}
	dsp := &fakeDispatcher{}
	gate := enabledGate()
	d := newDaemon(idle, nil, ready, gate, dsp)
	d.Now = fixedClock(t)

	// First tick dispatches.
	if r := d.Tick(context.Background()); r.Actions[0].Outcome != OutcomeDispatched {
		t.Fatalf("first tick: %+v", r)
	}
	// Second tick on the same clock must be blocked by rate limit.
	r := d.Tick(context.Background())
	if r.Actions[0].Outcome != OutcomeBlockedByGate {
		t.Errorf("second tick should be rate-limited; got %+v", r)
	}
}
