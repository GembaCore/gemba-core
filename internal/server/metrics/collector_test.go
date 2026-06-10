package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/GembaCore/gemba-core/internal/events"
)

func TestObserve_EscalationOpenCloseGauge(t *testing.T) {
	c := NewCollector()
	c.observe(events.GembaEvent{
		ID: "esc-1", Kind: events.EscalationOpened,
		Payload: map[string]any{"source_kind": "budget"},
	})
	c.observe(events.GembaEvent{
		ID: "esc-2", Kind: events.EscalationOpened,
		Payload: map[string]any{"source_kind": "budget"},
	})
	c.observe(events.GembaEvent{
		ID: "esc-3", Kind: events.EscalationOpened,
		Payload: map[string]any{"source_kind": "session"},
	})

	if got := testutil.ToFloat64(c.escalationOpenCount.WithLabelValues("budget")); got != 2 {
		t.Errorf("budget gauge: want 2 got %v", got)
	}
	if got := testutil.ToFloat64(c.escalationOpenCount.WithLabelValues("session")); got != 1 {
		t.Errorf("session gauge: want 1 got %v", got)
	}

	c.observe(events.GembaEvent{ID: "esc-1", Kind: events.EscalationResolved})
	if got := testutil.ToFloat64(c.escalationOpenCount.WithLabelValues("budget")); got != 1 {
		t.Errorf("after resolve: want 1 got %v", got)
	}

	// Re-emit of opened with the same id is a no-op (set semantics) — the
	// gauge must not climb past the true open count.
	c.observe(events.GembaEvent{
		ID: "esc-2", Kind: events.EscalationOpened,
		Payload: map[string]any{"source_kind": "budget"},
	})
	if got := testutil.ToFloat64(c.escalationOpenCount.WithLabelValues("budget")); got != 1 {
		t.Errorf("dup-open should not double-count: want 1 got %v", got)
	}
}

func TestObserve_AssignmentCostCounter(t *testing.T) {
	c := NewCollector()
	c.observe(events.GembaEvent{
		Kind: events.SessionCostSample, AssignmentID: "asn-1",
		Source:  events.Source{AdaptorID: "claude"},
		Payload: map[string]any{"cost_dollars": 0.42},
	})
	c.observe(events.GembaEvent{
		Kind: events.SessionCostSample, AssignmentID: "asn-1",
		Source:  events.Source{AdaptorID: "claude"},
		Payload: map[string]any{"cost_dollars": 1.00},
	})
	if got := testutil.ToFloat64(c.assignmentCost.WithLabelValues("claude", "asn-1")); got < 1.41 || got > 1.43 {
		t.Errorf("counter: want ~1.42 got %v", got)
	}
	// Zero/negative cost ignored — counters never decrement.
	c.observe(events.GembaEvent{
		Kind: events.SessionCostSample, AssignmentID: "asn-1",
		Payload: map[string]any{"cost_dollars": -1.0},
	})
	if got := testutil.ToFloat64(c.assignmentCost.WithLabelValues("claude", "asn-1")); got > 1.43 {
		t.Errorf("negative ignored: got %v", got)
	}
}

func TestObserve_WorkItemStateDuration(t *testing.T) {
	c := NewCollector()
	now := time.Now()

	c.observe(events.GembaEvent{
		Kind: events.WorkItemCreated, WorkItemID: "wi-1", At: now.Add(-10 * time.Second),
		Source:  events.Source{AdaptorID: "beads"},
		Payload: map[string]any{"state_category": "unstarted"},
	})
	c.observe(events.GembaEvent{
		Kind: events.WorkItemUpdated, WorkItemID: "wi-1", At: now,
		Source:  events.Source{AdaptorID: "beads"},
		Payload: map[string]any{"state_category": "started"},
	})

	if cnt := testutil.CollectAndCount(c.workitemStateDuration); cnt == 0 {
		t.Errorf("expected at least one observation")
	}

	// Same-state update should NOT observe a duration (no transition).
	preCount := metricSampleCount(t, c, "unstarted")
	c.observe(events.GembaEvent{
		Kind: events.WorkItemUpdated, WorkItemID: "wi-1", At: now,
		Source:  events.Source{AdaptorID: "beads"},
		Payload: map[string]any{"state_category": "started"},
	})
	postCount := metricSampleCount(t, c, "unstarted")
	if postCount != preCount {
		t.Errorf("same-state update should not increment unstarted bucket: pre=%d post=%d", preCount, postCount)
	}
}

func TestObserve_WorkspaceAcquireSpawnAndLatency(t *testing.T) {
	c := NewCollector()
	c.observe(events.GembaEvent{
		Kind:    events.WorkspaceAcquired,
		Payload: map[string]any{"acquire_duration_seconds": 0.05},
	})
	c.observe(events.GembaEvent{
		Kind:    events.WorkspaceAcquired,
		Payload: map[string]any{"acquire_duration_seconds": 0.5},
	})
	if got := testutil.ToFloat64(c.sessionSpawnTotal); got != 2 {
		t.Errorf("spawn counter: want 2 got %v", got)
	}
}

func TestHandler_RendersExpositionFormat(t *testing.T) {
	c := NewCollector()
	// Drive every metric at least once so each series shows up in the
	// scrape (Prometheus omits never-observed *Vec series).
	now := time.Now()
	c.observe(events.GembaEvent{
		ID: "esc-1", Kind: events.EscalationOpened,
		Payload: map[string]any{"source_kind": "budget"},
	})
	c.observe(events.GembaEvent{
		Kind: events.SessionCostSample, AssignmentID: "asn-1",
		Source:  events.Source{AdaptorID: "claude"},
		Payload: map[string]any{"cost_dollars": 0.10},
	})
	c.observe(events.GembaEvent{
		Kind: events.WorkItemCreated, WorkItemID: "wi-1", At: now.Add(-time.Second),
		Source:  events.Source{AdaptorID: "beads"},
		Payload: map[string]any{"state_category": "unstarted"},
	})
	c.observe(events.GembaEvent{
		Kind: events.WorkItemUpdated, WorkItemID: "wi-1", At: now,
		Source:  events.Source{AdaptorID: "beads"},
		Payload: map[string]any{"state_category": "started"},
	})
	c.observe(events.GembaEvent{
		Kind:    events.WorkspaceAcquired,
		Payload: map[string]any{"acquire_duration_seconds": 0.05},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	c.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status: want 200 got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"gemba_escalation_open_count",
		"gemba_assignment_total_cost_dollars",
		"gemba_workitem_state_duration_seconds",
		"gemba_session_spawn_total",
		"gemba_workspace_acquire_latency_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected exposition to include %q", want)
		}
	}
}

func TestRun_ExitsOnContextCancel(t *testing.T) {
	c := NewCollector()
	hub := events.NewHub(events.Config{})
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, hub); close(done) }()

	// Wait until Run's hub.Subscribe has registered before publishing —
	// Publish into a hub with zero matching subs drops the event.
	deadline := time.Now().Add(2 * time.Second)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.SubscriberCount() == 0 {
		t.Fatal("Run did not subscribe within 2s")
	}

	hub.Publish(events.GembaEvent{
		ID: "esc-1", Kind: events.EscalationOpened,
		Payload: map[string]any{"source_kind": "x"},
	})

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testutil.ToFloat64(c.escalationOpenCount.WithLabelValues("x")) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := testutil.ToFloat64(c.escalationOpenCount.WithLabelValues("x")); got != 1 {
		t.Errorf("Run did not consume publish: got %v", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not exit within 2s of cancel")
	}
}

// metricSampleCount returns the count for the (adaptor=beads,
// state_category=<state>) histogram series. Tiny helper so tests can
// assert "no new observation happened" without poking through
// prometheus internals.
func metricSampleCount(t *testing.T, c *Collector, state string) uint64 {
	t.Helper()
	h := c.workitemStateDuration.WithLabelValues("beads", state).(prometheus.Histogram)
	pb := &dto.Metric{}
	if err := h.Write(pb); err != nil {
		t.Fatalf("histogram write: %v", err)
	}
	return pb.GetHistogram().GetSampleCount()
}
