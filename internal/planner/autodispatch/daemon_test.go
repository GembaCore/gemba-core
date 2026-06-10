// Auto-dispatch daemon tests (gm-s47n.6.3).

package autodispatch

import (
	"context"
	"errors"
	"strings"
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
	cold  []core.WorkItemID
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

func (f *fakeDispatcher) StartCold(_ context.Context, beadID core.WorkItemID) (string, error) {
	f.cold = append(f.cold, beadID)
	if f.err != nil {
		return "", f.err
	}
	return "sess-cold-" + string(beadID), nil
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

func TestTick_ColdStartsWhenPoolHasNoIdleSessions(t *testing.T) {
	ready := []ReadyBead{
		bead("gm-1", "gemba", "auth"),
		bead("gm-2", "other"),
	}
	dsp := &fakeDispatcher{}
	d := newDaemon(nil, nil, ready, enabledGate(), dsp)
	d.ColdStart = dsp
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(r.Actions))
	}
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Fatalf("outcome = %q, want dispatched", r.Actions[0].Outcome)
	}
	if r.Actions[0].SessionID != "sess-cold-gm-1" {
		t.Errorf("sessionID = %q", r.Actions[0].SessionID)
	}
	if len(dsp.cold) != 1 || dsp.cold[0] != "gm-1" {
		t.Errorf("cold starts = %+v", dsp.cold)
	}
	if len(dsp.calls) != 0 {
		t.Errorf("reuse dispatcher should not be called on cold start: %+v", dsp.calls)
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

// ── Auto-dispatch floor (gm-s47n.12, spec §8.1) ─────────────────

// TestTick_BlockedByFloorWhenScoreBelow pins the spec §8.1 gate. A
// daemon with AutoDispatchFloor=0.5 must refuse a dispatch when the
// top pick's combined affinity is below the floor — even when no
// other gate would block.
func TestTick_BlockedByFloorWhenScoreBelow(t *testing.T) {
	// Empty profile + minimal health → top affinity will be near zero.
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba", "unrelated-concept")}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	d.AutoDispatchFloor = 0.5 // spec default

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(r.Actions))
	}
	if r.Actions[0].Outcome != OutcomeBelowFloor {
		t.Errorf("outcome = %q, want %q", r.Actions[0].Outcome, OutcomeBelowFloor)
	}
	if len(dsp.calls) != 0 {
		t.Errorf("no dispatch should fire below floor; got %+v", dsp.calls)
	}
}

// TestTick_FloorZeroDisabled confirms that AutoDispatchFloor=0 keeps
// the daemon's prior (Phase 0) behavior — every dispatchable pick
// goes through. Phase 0 zero-delta requires this.
func TestTick_FloorZeroDisabled(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba", "unrelated")}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	// d.AutoDispatchFloor zero → no floor gate

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Errorf("outcome = %q, want %q (no floor)", r.Actions[0].Outcome, OutcomeDispatched)
	}
}

// TestTick_FloorAllowsHighAffinity confirms that the floor only
// blocks scores below it — when the top pick clears the floor, the
// dispatch fires normally.
func TestTick_FloorAllowsHighAffinity(t *testing.T) {
	// Strong concept match → high affinity.
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba", "auth")}
	dsp := &fakeDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)
	d.AutoDispatchFloor = 0.1

	r := d.Tick(context.Background())
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Errorf("high-affinity match should clear floor; got %+v", r.Actions[0])
	}
}

// ── ClaimModel manifest gate (gm-e3.8) ──────────────────────────

// fakeAlreadyClaimedDispatcher fails the first N Dispatch calls with the
// already-claimed sentinel, then succeeds on the (N+1)th. Records every
// (sessionID, beadID) it observes so tests can assert the daemon walked
// past the right candidates.
type fakeAlreadyClaimedDispatcher struct {
	failFirstN int
	calls      []dispatchCall
	finalErr   error // when set, the (N+1)th call also errors
}

func (f *fakeAlreadyClaimedDispatcher) Dispatch(_ context.Context, sessionID string, beadID core.WorkItemID) error {
	f.calls = append(f.calls, dispatchCall{sessionID: sessionID, beadID: beadID})
	if len(f.calls) <= f.failFirstN {
		return core.NewAlreadyClaimedError("bead %q already hooked", beadID)
	}
	return f.finalErr
}

// TestTick_InlineSoftSkipsAlreadyClaimedAndDispatchesNext is the
// gm-e3.8 inline-claim soft-skip pin. The first candidate's Dispatch
// returns ErrBeadAlreadyClaimed; the daemon must record the soft-skip
// and dispatch the second candidate without bubbling an error.
func TestTick_InlineSoftSkipsAlreadyClaimedAndDispatchesNext(t *testing.T) {
	profile := &planner.SessionProfile{
		Concepts: map[planner.ConceptTag]float64{"auth": 1.0},
	}
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", profile, &planner.SessionHealth{}),
	}
	// Two candidates; the highest-affinity one will lose the inline race.
	ready := []ReadyBead{
		bead("gm-top", "gemba", "auth"),  // wins ranking
		bead("gm-next", "gemba", "auth"), // dispatched after soft skip
	}
	dsp := &fakeAlreadyClaimedDispatcher{failFirstN: 1}
	d := newDaemon(idle, nil, ready, enabledGate(), nil)
	d.Dispatcher = dsp
	d.Now = fixedClock(t)
	// ClaimModel left empty — must default to inline (back-compat).

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %d, want 1 (final action only)", len(r.Actions))
	}
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Errorf("outcome = %q, want %q", r.Actions[0].Outcome, OutcomeDispatched)
	}
	if r.Actions[0].BeadID != "gm-next" {
		t.Errorf("dispatched bead = %s, want gm-next", r.Actions[0].BeadID)
	}
	if len(dsp.calls) != 2 {
		t.Errorf("dispatcher calls = %d, want 2 (skip + win)", len(dsp.calls))
	}
	if dsp.calls[0].beadID != "gm-top" || dsp.calls[1].beadID != "gm-next" {
		t.Errorf("dispatch order = %+v, want [gm-top, gm-next]", dsp.calls)
	}
}

// TestTick_InlineSoftSkipBoundExhausted confirms the
// MaxSoftSkipRetriesPerTick bound holds: when every candidate returns
// already-claimed, the daemon stops after the budget and surfaces an
// OutcomeAlreadyClaimed action carrying the budget-exhausted reason.
func TestTick_InlineSoftSkipBoundExhausted(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{
		bead("gm-1", "gemba"),
		bead("gm-2", "gemba"),
		bead("gm-3", "gemba"),
		bead("gm-4", "gemba"),
		bead("gm-5", "gemba"),
	}
	// All candidates collide.
	dsp := &fakeAlreadyClaimedDispatcher{failFirstN: 100}
	d := newDaemon(idle, nil, ready, enabledGate(), nil)
	d.Dispatcher = dsp
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if len(r.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(r.Actions))
	}
	if r.Actions[0].Outcome != OutcomeAlreadyClaimed {
		t.Errorf("outcome = %q, want %q", r.Actions[0].Outcome, OutcomeAlreadyClaimed)
	}
	// Budget enforcement: the daemon attempts at most
	// MaxSoftSkipRetriesPerTick candidates before giving up. The
	// total dispatcher call count MUST be capped — it can not walk
	// every bead.
	if got := len(dsp.calls); got > MaxSoftSkipRetriesPerTick {
		t.Errorf("dispatcher call count = %d, exceeds bound %d", got, MaxSoftSkipRetriesPerTick)
	}
	if got := len(dsp.calls); got < 1 {
		t.Errorf("dispatcher must be called at least once; got %d", got)
	}
	if !strings.Contains(r.Actions[0].Reason, "retry budget exhausted") {
		t.Errorf("reason = %q, expected 'retry budget exhausted'", r.Actions[0].Reason)
	}
}

// TestTick_InlineNonClaimedErrorDoesNotSoftSkip pins a load-bearing
// distinction: only ErrBeadAlreadyClaimed triggers the soft-skip path.
// A generic dispatcher error still surfaces as OutcomeError (and the
// daemon does NOT walk to the next candidate).
func TestTick_InlineNonClaimedErrorDoesNotSoftSkip(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{
		bead("gm-1", "gemba"),
		bead("gm-2", "gemba"),
	}
	// Generic error — NOT the sentinel. Should NOT soft-skip.
	dsp := &fakeDispatcher{err: errors.New("provider down")}
	d := newDaemon(idle, nil, ready, enabledGate(), dsp)
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Actions[0].Outcome != OutcomeError {
		t.Errorf("outcome = %q, want %q", r.Actions[0].Outcome, OutcomeError)
	}
	if len(dsp.calls) != 1 {
		t.Errorf("dispatcher calls = %d, want 1 (no soft-skip on non-sentinel error)", len(dsp.calls))
	}
}

// fakeTwoPhaseDispatcher implements TwoPhaseDispatcher for the
// dormant ClaimModelTwoPhase path. Records every method invocation
// so tests can assert routing.
type fakeTwoPhaseDispatcher struct {
	claims    []core.WorkItemID
	converts  []string
	releases  []string
	claimErr  error // returned by next Claim
	failFirst int   // first N claims return already-claimed
}

func (f *fakeTwoPhaseDispatcher) ClaimReservation(_ context.Context, _ string, beadID core.WorkItemID) (*core.Reservation, error) {
	f.claims = append(f.claims, beadID)
	if len(f.claims) <= f.failFirst {
		return nil, core.NewAlreadyClaimedError("bead %q already reserved", beadID)
	}
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return &core.Reservation{ID: "res-" + string(beadID), WorkItemID: beadID}, nil
}

func (f *fakeTwoPhaseDispatcher) ConvertReservation(_ context.Context, _ string, r *core.Reservation) error {
	f.converts = append(f.converts, r.ID)
	return nil
}

func (f *fakeTwoPhaseDispatcher) ReleaseReservation(_ context.Context, id string) error {
	f.releases = append(f.releases, id)
	return nil
}

// TestTick_TwoPhaseRoutesThroughReservationChain confirms the
// ClaimModelTwoPhase manifest gate routes through the
// claim → convert chain. Even though no in-tree adaptor declares
// two_phase, the daemon's branch must work end-to-end.
func TestTick_TwoPhaseRoutesThroughReservationChain(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba")}
	// A regular SessionDispatcher is still required by validate(),
	// even though dispatchTwoPhase ignores it.
	tp := &fakeTwoPhaseDispatcher{}
	d := newDaemon(idle, nil, ready, enabledGate(), &fakeDispatcher{})
	d.ClaimModel = core.ClaimModelTwoPhase
	d.TwoPhase = tp
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Err != nil {
		t.Fatalf("Tick: %v", r.Err)
	}
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Errorf("outcome = %q, want dispatched", r.Actions[0].Outcome)
	}
	if len(tp.claims) != 1 || tp.claims[0] != "gm-1" {
		t.Errorf("claims = %+v, want [gm-1]", tp.claims)
	}
	if len(tp.converts) != 1 {
		t.Errorf("converts = %+v, want 1 entry", tp.converts)
	}
	if len(tp.releases) != 0 {
		t.Errorf("no release expected on success path; got %+v", tp.releases)
	}
}

// TestTick_TwoPhaseSoftSkipsAlreadyClaimed mirrors the inline test on
// the two-phase path: a reservation collision soft-skips to the next
// candidate.
func TestTick_TwoPhaseSoftSkipsAlreadyClaimed(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{
		bead("gm-top", "gemba"),
		bead("gm-next", "gemba"),
	}
	tp := &fakeTwoPhaseDispatcher{failFirst: 1}
	d := newDaemon(idle, nil, ready, enabledGate(), &fakeDispatcher{})
	d.ClaimModel = core.ClaimModelTwoPhase
	d.TwoPhase = tp
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Actions[0].Outcome != OutcomeDispatched {
		t.Errorf("outcome = %q", r.Actions[0].Outcome)
	}
	if r.Actions[0].BeadID != "gm-next" {
		t.Errorf("bead = %s, want gm-next", r.Actions[0].BeadID)
	}
}

// TestTick_TwoPhaseMissingDispatcherErrors confirms an operator
// misconfiguration (ClaimModel=two_phase + nil TwoPhase) surfaces as a
// clean error action rather than a panic.
func TestTick_TwoPhaseMissingDispatcherErrors(t *testing.T) {
	idle := []planner.OperationalContext{
		sessCtx("sess-1", "gemba", &planner.SessionProfile{}, &planner.SessionHealth{}),
	}
	ready := []ReadyBead{bead("gm-1", "gemba")}
	d := newDaemon(idle, nil, ready, enabledGate(), &fakeDispatcher{})
	d.ClaimModel = core.ClaimModelTwoPhase
	// d.TwoPhase intentionally nil
	d.Now = fixedClock(t)

	r := d.Tick(context.Background())
	if r.Actions[0].Outcome != OutcomeError {
		t.Errorf("outcome = %q, want error", r.Actions[0].Outcome)
	}
}
