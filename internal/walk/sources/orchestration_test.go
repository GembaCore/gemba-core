// gm-3jv tests: live OrchestrationPlane wiring + Subscribe-bridge
// republishing.

package sources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/events"
)

// fakeLister is a minimal orchestrationLister stub. The full
// FakeOrchestrationPlane in internal/transport/testadaptors works
// too, but using a tiny local fake keeps the tests focused on the
// (workspace argument → empty filter → upstream call) bridge.
type fakeLister struct {
	called int
	last   core.EscalationFilter
	out    []core.EscalationRequest
	err    error
}

func (f *fakeLister) ListOpenEscalations(_ context.Context, filter core.EscalationFilter) ([]core.EscalationRequest, error) {
	f.called++
	f.last = filter
	return f.out, f.err
}

func TestOrchestrationPlaneEscalationLister_PassesThrough(t *testing.T) {
	want := []core.EscalationRequest{
		{ID: "e1", Title: "from upstream", State: core.EscalationOpen, CreatedAt: time.Now()},
		{ID: "e2", Title: "another", State: core.EscalationOpen, CreatedAt: time.Now()},
	}
	fk := &fakeLister{out: want}
	l := newListerFromMin(fk)

	got, err := l.ListOpenEscalations(context.Background(), "ws-alpha")
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].ID != want[i].ID {
			t.Errorf("got[%d].ID = %q want %q", i, got[i].ID, want[i].ID)
		}
	}
	if fk.called != 1 {
		t.Errorf("upstream called %d times; want 1", fk.called)
	}
	// gm-vch2: workspace flows through to EscalationFilter.Workspace.
	if fk.last.Workspace != "ws-alpha" {
		t.Errorf("upstream filter.Workspace = %q; want ws-alpha", fk.last.Workspace)
	}
	// And no other field bled through.
	if fk.last.AssignmentID != "" || fk.last.AgentID != "" || fk.last.Source != "" {
		t.Errorf("upstream filter = %+v; only Workspace should be set", fk.last)
	}
}

func TestOrchestrationPlaneEscalationLister_PropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	l := newListerFromMin(&fakeLister{err: wantErr})

	got, err := l.ListOpenEscalations(context.Background(), "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v; want %v", err, wantErr)
	}
	if got != nil {
		t.Errorf("got = %v; want nil on error", got)
	}
}

func TestOrchestrationPlaneEscalationLister_NilSafe(t *testing.T) {
	// Constructed against a nil adaptor — must contribute zero
	// without panicking. Same nil-safety contract as the other
	// listers in walk.Sources.
	l := NewOrchestrationPlaneEscalationLister(nil)
	if l != nil {
		// NewOrchestrationPlaneEscalationLister returns nil for
		// nil input; callers use it directly via Sources.Escalations.
		t.Fatalf("expected nil for nil adaptor input, got %T", l)
	}

	// Verify the typed-nil receiver path also works (defensive).
	var ln *OrchestrationPlaneEscalationLister
	got, err := ln.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("nil-receiver ListOpenEscalations: %v", err)
	}
	if got != nil {
		t.Errorf("nil-receiver should contribute zero; got %v", got)
	}
}

// fakeSubscriber is the minimal orchestrationSubscriber for the
// bridge tests. It exposes a controllable channel callers can
// drive.
type fakeSubscriber struct {
	ch           chan core.OrchestrationEvent
	subscribeErr error
	adaptorID    string
}

func newFakeSubscriber(adaptorID string) *fakeSubscriber {
	return &fakeSubscriber{ch: make(chan core.OrchestrationEvent, 4), adaptorID: adaptorID}
}

func (f *fakeSubscriber) Subscribe(_ context.Context, _ core.SubscribeFilter) (<-chan core.OrchestrationEvent, error) {
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	return f.ch, nil
}

func (f *fakeSubscriber) Describe() core.OrchestrationCapabilityManifest {
	return core.OrchestrationCapabilityManifest{AdaptorID: f.adaptorID}
}

func TestStartOrchestrationBridge_RepublishesEscalationEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := events.NewHub(events.Config{})
	defer hub.Close()

	out := hub.Subscribe(ctx, events.Filter{Kinds: []events.Kind{WalkEscalationChanged}})

	sub := newFakeSubscriber("test-adaptor")
	startOrchestrationBridge(ctx, sub, hub)

	// Drive an escalation.raised through.
	now := time.Now()
	sub.ch <- core.OrchestrationEvent{
		ID:           "ev-1",
		Kind:         "escalation.raised",
		At:           now,
		AssignmentID: "asg-1",
		SessionID:    "sess-1",
		AgentID:      core.AgentID("polecat-obsidian"),
		Payload:      map[string]any{"escalation_id": "e-1", "work_item_id": "gm-77"},
	}

	select {
	case ev, ok := <-out:
		if !ok {
			t.Fatal("hub channel closed before event arrived")
		}
		if ev.Kind != WalkEscalationChanged {
			t.Errorf("Kind = %q; want %q", ev.Kind, WalkEscalationChanged)
		}
		if ev.ID != "ev-1" {
			t.Errorf("ID = %q; want ev-1", ev.ID)
		}
		if ev.AssignmentID != "asg-1" || ev.SessionID != "sess-1" {
			t.Errorf("scope hooks lost: assignment=%q session=%q", ev.AssignmentID, ev.SessionID)
		}
		if ev.AgentID != "polecat-obsidian" {
			t.Errorf("AgentID = %q; want polecat-obsidian", ev.AgentID)
		}
		if ev.WorkItemID != "gm-77" {
			t.Errorf("WorkItemID = %q; want gm-77 (lifted from payload)", ev.WorkItemID)
		}
		if ev.Source.Plane != events.PlaneOrchestration {
			t.Errorf("Source.Plane = %q; want %q", ev.Source.Plane, events.PlaneOrchestration)
		}
		if ev.Source.AdaptorID != "test-adaptor" {
			t.Errorf("Source.AdaptorID = %q; want test-adaptor", ev.Source.AdaptorID)
		}
		if ev.Payload["upstream_kind"] != "escalation.raised" {
			t.Errorf("upstream_kind not preserved on payload: %v", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for republished walk.escalation_changed event")
	}
}

func TestStartOrchestrationBridge_IgnoresNonEscalationEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := events.NewHub(events.Config{})
	defer hub.Close()

	// Subscribe to *all* hub events so we can prove the non-
	// escalation event was dropped (and not republished as
	// some other kind).
	out := hub.Subscribe(ctx, events.Filter{})

	sub := newFakeSubscriber("test-adaptor")
	startOrchestrationBridge(ctx, sub, hub)

	sub.ch <- core.OrchestrationEvent{
		ID:   "ev-2",
		Kind: "session.transition",
		At:   time.Now(),
	}
	// Push an escalation event after to give the bridge time
	// to drop the first; if it didn't, this second event will
	// arrive *second* and the assertion catches the race.
	sub.ch <- core.OrchestrationEvent{
		ID:   "ev-3",
		Kind: "escalation.resolved",
		At:   time.Now(),
	}

	select {
	case ev := <-out:
		if ev.ID != "ev-3" {
			t.Errorf("first published event ID = %q; want ev-3 (the session.transition must be dropped)", ev.ID)
		}
		if ev.Kind != WalkEscalationChanged {
			t.Errorf("Kind = %q; want %q", ev.Kind, WalkEscalationChanged)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for republished event")
	}
}

func TestStartOrchestrationBridge_ExitsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	hub := events.NewHub(events.Config{})
	defer hub.Close()

	sub := newFakeSubscriber("test-adaptor")
	startOrchestrationBridge(ctx, sub, hub)

	// Cancel — the pump must exit. Verify by closing the upstream
	// channel afterwards: a still-running pump would panic on send
	// to a closed channel; an exited pump never reads.
	cancel()
	// Give the pump a tick to notice.
	time.Sleep(20 * time.Millisecond)
	close(sub.ch)
	// No assertion — the test passes if no goroutine panics.
}

func TestStartOrchestrationBridge_NilSafe(t *testing.T) {
	// Public entry: nil plane, nil hub. Must not panic.
	StartOrchestrationBridge(context.Background(), nil, nil)

	// Nil plane with a real hub.
	hub := events.NewHub(events.Config{})
	defer hub.Close()
	StartOrchestrationBridge(context.Background(), nil, hub)
}

func TestStartOrchestrationBridge_SubscribeError(t *testing.T) {
	// A Subscribe error must NOT panic — the bridge logs and the
	// SPA degrades to its polling path.
	hub := events.NewHub(events.Config{})
	defer hub.Close()

	sub := newFakeSubscriber("test-adaptor")
	sub.subscribeErr = errors.New("upstream offline")
	startOrchestrationBridge(context.Background(), sub, hub)
	// The pump never started; nothing to assert beyond no-panic.
}

func TestIsEscalationEvent(t *testing.T) {
	cases := []struct {
		kind string
		want bool
	}{
		{"escalation.raised", true},
		{"escalation.resolved", true},
		{"escalation.canceled", true},
		{"escalation.expired", true},
		// Future kind in the same namespace — must participate.
		{"escalation.deferred_until", true},
		// Out-of-namespace.
		{"session.transition", false},
		{"workspace.acquired", false},
		{"escalation", false}, // missing the dot — not in the namespace.
		{"", false},
	}
	for _, c := range cases {
		if got := isEscalationEvent(c.kind); got != c.want {
			t.Errorf("isEscalationEvent(%q) = %v; want %v", c.kind, got, c.want)
		}
	}
}

func TestShouldRepublish(t *testing.T) {
	cases := []struct {
		kind string
		want bool
	}{
		{"escalation.raised", true},
		{"escalation.resolved", true},
		// gm-vch2 additions — agenda triggers.
		{"budget.warn", true},
		{"budget.stop", true},
		{"capability.refresh-degraded", true},
		{"adaptor.degraded", true},
		// budget.inform is informational only — must NOT churn the agenda.
		{"budget.inform", false},
		// capability.refresh (without -degraded) is benign.
		{"capability.refresh", false},
		{"session.transition", false},
		{"workspace.acquired", false},
		{"", false},
	}
	for _, c := range cases {
		if got := shouldRepublish(c.kind); got != c.want {
			t.Errorf("shouldRepublish(%q) = %v; want %v", c.kind, got, c.want)
		}
	}
}

func TestStartOrchestrationBridge_RepublishesAgendaTriggers(t *testing.T) {
	cases := []string{"budget.warn", "budget.stop", "capability.refresh-degraded", "adaptor.degraded"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			hub := events.NewHub(events.Config{})
			defer hub.Close()

			out := hub.Subscribe(ctx, events.Filter{Kinds: []events.Kind{WalkEscalationChanged}})

			sub := newFakeSubscriber("test-adaptor")
			startOrchestrationBridge(ctx, sub, hub)

			sub.ch <- core.OrchestrationEvent{
				ID:   "ev-trigger",
				Kind: kind,
				At:   time.Now(),
			}

			select {
			case ev := <-out:
				if ev.Kind != WalkEscalationChanged {
					t.Errorf("Kind = %q; want %q", ev.Kind, WalkEscalationChanged)
				}
				if ev.Payload["upstream_kind"] != kind {
					t.Errorf("upstream_kind = %v; want %q", ev.Payload["upstream_kind"], kind)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("timed out waiting for republished %q event", kind)
			}
		})
	}
}
