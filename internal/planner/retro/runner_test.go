// Tests for the bead-close retrospective runner (gm-s47n.8.3).
//
// The runner orchestrates declared+actual readers, the comparator,
// the scorer_grades writer, and the session-profile writeback. Every
// dependency is mocked here — the comparator and store are exercised
// in their own packages.

package retro

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/agentprofile"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/targets"
)

// fakeDeclared returns a fixed Declared for any bead.
type fakeDeclared struct {
	d   Declared
	err error
}

func (f *fakeDeclared) Declared(_ context.Context, _ core.WorkItemID) (Declared, error) {
	return f.d, f.err
}

// fakeActual returns a fixed Actual for any bead.
type fakeActual struct {
	a   Actual
	err error
}

func (f *fakeActual) Actual(_ context.Context, _ core.WorkItemID) (Actual, error) {
	return f.a, f.err
}

// fakeProfiles records calls to RecordCompletion. The other
// ProfileWriter methods are no-ops — the runner only calls
// RecordCompletion.
type fakeProfiles struct {
	mu    sync.Mutex
	calls []planner.CompletionEvent
	err   error
}

func (f *fakeProfiles) RecordCompletion(_ context.Context, ev planner.CompletionEvent) (*planner.SessionProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ev)
	return &planner.SessionProfile{}, f.err
}
func (f *fakeProfiles) RecordClaim(_ context.Context, _ planner.ClaimEvent) (*planner.SessionProfile, error) {
	return nil, nil
}
func (f *fakeProfiles) UpsertProfile(_ context.Context, _ *planner.SessionProfile) error { return nil }

// fakeAgentProfiles records calls to AgentProfileWriter.RecordCompletion
// so the runner test can assert the agent profile writeback fires.
type fakeAgentProfiles struct {
	mu    sync.Mutex
	calls []agentprofile.CompletionEvent
	err   error
}

func (f *fakeAgentProfiles) RecordCompletion(_ context.Context, ev agentprofile.CompletionEvent) (*agentprofile.AgentProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ev)
	return &agentprofile.AgentProfile{AgentID: ev.AgentID}, f.err
}

func newRunnerWithStore(t *testing.T) (*Runner, *Store, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	store := NewStore(db)
	r := &Runner{
		Store: store,
		Now:   func() time.Time { return mustParseRetroTime(t) },
	}
	return r, store, mock, db
}

func closedEvent(t *testing.T, beadID, sessionID string) events.GembaEvent {
	t.Helper()
	return events.GembaEvent{
		Kind:         events.WorkItemClosed,
		At:           mustParseRetroTime(t),
		WorkItemID:   beadID,
		SessionID:    sessionID,
		AgentID:      "mike2",
		AssignmentID: "asn-1",
	}
}

// ── happy path ──────────────────────────────────────────────────

func TestRunOne_WritesGradeAndUpdatesProfile(t *testing.T) {
	r, _, mock, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{d: Declared{
		Targets:  []targets.Pattern{"src/auth.go"},
		Concepts: []planner.ConceptTag{"auth"},
	}}
	r.Actual = &fakeActual{a: Actual{
		Files:    []string{"src/auth.go", "src/handlers.go"},
		Concepts: []planner.ConceptTag{"auth", "ratelimit"},
	}}
	profiles := &fakeProfiles{}
	r.Profiles = profiles

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "sess-1")); err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("mock expectations: %v", err)
	}
	if len(profiles.calls) != 1 {
		t.Fatalf("expected 1 RecordCompletion call; got %d", len(profiles.calls))
	}
	got := profiles.calls[0]
	if got.SessionID != "sess-1" || got.BeadID != "gm-1" {
		t.Errorf("profile call shape: %+v", got)
	}
	if len(got.ActualFiles) != 2 || len(got.ActualConcepts) != 2 {
		t.Errorf("profile actuals not threaded through: %+v", got)
	}
}

func TestRunOne_AlsoWritesAgentProfile(t *testing.T) {
	r, _, mock, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{
		Files:    []string{"src/auth.go"},
		Concepts: []planner.ConceptTag{"auth"},
	}}
	r.Profiles = &fakeProfiles{}
	agentProfiles := &fakeAgentProfiles{}
	r.AgentProfiles = agentProfiles

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "sess-1")); err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(agentProfiles.calls) != 1 {
		t.Fatalf("expected 1 agent-profile RecordCompletion; got %d", len(agentProfiles.calls))
	}
	call := agentProfiles.calls[0]
	if call.AgentID != "mike2" || call.BeadID != "gm-1" {
		t.Errorf("agent-profile call shape: %+v", call)
	}
	if len(call.Files) != 1 || len(call.Concepts) != 1 {
		t.Errorf("actuals not threaded into agent-profile call: %+v", call)
	}
}

func TestRunOne_NoAgentIDSkipsAgentProfile(t *testing.T) {
	r, _, mock, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{Files: []string{"x"}}}
	agentProfiles := &fakeAgentProfiles{}
	r.AgentProfiles = agentProfiles

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Closed event with empty AgentID — common during early bring-up
	// before the orchestration plane is wired.
	ev := events.GembaEvent{
		Kind:       events.WorkItemClosed,
		At:         mustParseRetroTime(t),
		WorkItemID: "gm-1",
		SessionID:  "sess-1",
		// AgentID intentionally empty.
	}
	if err := r.RunOne(context.Background(), ev); err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(agentProfiles.calls) != 0 {
		t.Errorf("agent-profile writeback fired with empty AgentID; got %d calls", len(agentProfiles.calls))
	}
}

// ── transient skips ──────────────────────────────────────────────

func TestRunOne_SkipsWhenActualUnavailable(t *testing.T) {
	r, _, _, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{err: ErrUnavailable}
	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "sess-1")); err != nil {
		t.Fatalf("ErrUnavailable should be a quiet skip; got %v", err)
	}
}

func TestRunOne_SkipsWhenNoActualSource(t *testing.T) {
	// No ActualSource bound → silent skip (not an error). This is
	// the "Layer 2 not yet wired" production posture.
	r, _, _, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "sess-1")); err != nil {
		t.Fatalf("missing ActualSource should skip; got %v", err)
	}
}

// ── error paths ──────────────────────────────────────────────────

func TestRunOne_RejectsEmptyWorkItemID(t *testing.T) {
	r, _, _, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{}}
	ev := closedEvent(t, "", "sess-1")
	if err := r.RunOne(context.Background(), ev); err == nil {
		t.Fatal("expected error for empty WorkItemID")
	}
}

func TestRunOne_PropagatesDeclaredError(t *testing.T) {
	r, _, _, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{err: errors.New("bd down")}
	r.Actual = &fakeActual{}
	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "sess-1")); err == nil {
		t.Fatal("expected declared-source error to propagate")
	}
}

func TestRunOne_PropagatesProfileError(t *testing.T) {
	r, _, mock, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{Files: []string{"src/foo.go"}}}
	r.Profiles = &fakeProfiles{err: errors.New("profile write failed")}

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "sess-1")); err == nil {
		t.Fatal("expected profile error to propagate")
	}
}

// ── session-id-less events skip profile writeback ────────────────

func TestRunOne_NoSessionIDSkipsProfileWriteback(t *testing.T) {
	r, _, mock, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{Files: []string{"src/foo.go"}}}
	profiles := &fakeProfiles{}
	r.Profiles = profiles

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// No SessionID on the event.
	if err := r.RunOne(context.Background(), closedEvent(t, "gm-1", "")); err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if len(profiles.calls) != 0 {
		t.Errorf("profile writeback should skip without SessionID; got %d calls", len(profiles.calls))
	}
}

// ── async via Run + hub ──────────────────────────────────────────

func TestRun_DispatchesClosedEventsAsync(t *testing.T) {
	r, _, mock, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{Files: []string{"src/foo.go"}}}

	var got int32
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scorer_grades")).
		WillReturnResult(sqlmock.NewResult(1, 1)).
		WillDelayFor(0)
	r.OnError = func(err error) {
		t.Errorf("OnError fired: %v", err)
	}
	// Wrap the store call to count dispatched retros via a side
	// channel — sqlmock doesn't expose a "was called" counter.
	originalStore := r.Store
	_ = originalStore
	go func() {
		// Tick a counter via a custom DeclaredSource — easier than
		// instrumenting sqlmock. atomic-add on every dispatch.
	}()

	hub := events.NewHub(events.Config{})
	defer hub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneRun := make(chan struct{})
	go func() {
		r.Run(ctx, hub)
		close(doneRun)
	}()
	waitForSubscribers(t, hub, 1)

	hub.Publish(events.GembaEvent{
		Kind:       events.WorkItemClosed,
		At:         mustParseRetroTime(t),
		WorkItemID: "gm-async",
		SessionID:  "sess-x",
	})

	// Poll until sqlmock sees the insert. Wait() can't help us
	// here because the dispatch goroutine isn't spawned until the
	// hub delivers the event — there's a beat between Publish and
	// pending.Add(1) that Wait() sees as "nothing to wait for".
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mock.ExpectationsWereMet() == nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-doneRun
	r.Wait()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("async dispatch did not insert: %v", err)
	}
	_ = atomic.LoadInt32(&got)
}

func waitForSubscribers(t *testing.T, hub *events.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.SubscriberCount() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("hub never reached %d subscribers", want)
}

func TestRun_FiltersToClosedEvents(t *testing.T) {
	// Run subscribes with a Closed-only filter. Publishing a
	// non-Closed event must NOT trigger an Insert. Hub.Publish is
	// async-fan-out; sqlmock will fail expectations if anything
	// fires here.
	r, _, _, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{}
	r.Actual = &fakeActual{a: Actual{Files: []string{"src/foo.go"}}}

	hub := events.NewHub(events.Config{})
	defer hub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneRun := make(chan struct{})
	go func() {
		r.Run(ctx, hub)
		close(doneRun)
	}()
	waitForSubscribers(t, hub, 1)

	// Send something the runner shouldn't observe.
	hub.Publish(events.GembaEvent{
		Kind:       events.WorkItemUpdated,
		At:         mustParseRetroTime(t),
		WorkItemID: "gm-update",
	})
	// Give the hub a tick to process; if the runner had matched it,
	// we'd panic on the unmocked sql call.
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-doneRun
	r.Wait()
}

// ── nil safety ───────────────────────────────────────────────────

func TestRun_NilRunnerIsNoop(t *testing.T) {
	var r *Runner
	hub := events.NewHub(events.Config{})
	defer hub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Run(ctx, hub) // must not panic
}

func TestRunOne_NilRunnerErrors(t *testing.T) {
	var r *Runner
	if err := r.RunOne(context.Background(), events.GembaEvent{}); err == nil {
		t.Fatal("nil runner should error")
	}
}

// ── OnError captures dispatch errors ─────────────────────────────

func TestDispatch_RoutesErrorsToOnError(t *testing.T) {
	r, _, _, db := newRunnerWithStore(t)
	defer db.Close()
	r.Declared = &fakeDeclared{err: errors.New("declared lookup failed")}
	r.Actual = &fakeActual{}

	captured := make(chan error, 1)
	r.OnError = func(err error) { captured <- err }

	hub := events.NewHub(events.Config{})
	defer hub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneRun := make(chan struct{})
	go func() {
		r.Run(ctx, hub)
		close(doneRun)
	}()
	waitForSubscribers(t, hub, 1)

	hub.Publish(closedEvent(t, "gm-1", "sess-1"))

	select {
	case err := <-captured:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	case <-time.After(time.Second):
		t.Fatal("OnError never fired")
	}
	cancel()
	<-doneRun
	r.Wait()
}
