package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
)

func serverConfig() config.ServeConfig { return config.ServeConfig{} }

// newRouterWithBoth wires both a WorkPlane (with the seeded items)
// and an OrchestrationPlane (with the seeded sessions). Built on
// the same testadaptors as the rest of the server tests.
func newRouterWithBoth(t *testing.T, items []core.WorkItem, sessions []core.Session) *Router {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.ListFn = func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
		return items, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	op.SessionsFn = func(_ context.Context, _ core.SessionFilter) ([]core.Session, error) {
		return sessions, nil
	}
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}
	return NewRouter(serverConfig(), fakeSPA(), host)
}

func TestPlannerCoach_NoOrchestrationReturns503(t *testing.T) {
	h := newRouterWithOrch(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/planner/coach", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", rec.Code)
	}
}

func TestPlannerCoach_HappyPath(t *testing.T) {
	items := []core.WorkItem{
		{
			ID:                  "gm-1",
			Title:               "first",
			Kind:                "task",
			Status:              "open",
			StateCategory:       core.StateUnstarted,
			PrimaryRepositoryID: "gemba",
			Custom: map[string]any{
				"concepts": []any{"auth"},
				"targets":  []any{"src/auth.go"},
			},
		},
		{
			ID:                  "gm-2",
			Title:               "second",
			Kind:                "task",
			Status:              "open",
			StateCategory:       core.StateUnstarted,
			PrimaryRepositoryID: "gemba",
			Custom: map[string]any{
				"concepts": []any{"auth"},
				"targets":  []any{"src/auth.go"},
			},
		},
		{
			ID:                  "gm-done",
			Title:               "done",
			Kind:                "task",
			Status:              "closed",
			StateCategory:       core.StateCompleted,
			PrimaryRepositoryID: "gemba",
		},
	}
	sessions := []core.Session{
		{ID: "sess-1", AgentID: "alice", AssignmentID: "asg-1", Status: core.SessionWorking, StartedAt: time.Now().Add(-time.Hour)},
	}
	h := newRouterWithBoth(t, items, sessions)
	req := httptest.NewRequest(http.MethodGet, "/api/planner/coach", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env coachResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, rec.Body.String())
	}
	// Completed bead must NOT appear in ready set.
	if len(env.ReadyBeads) != 2 {
		t.Errorf("expected 2 ready beads, got %d", len(env.ReadyBeads))
	}
	if len(env.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(env.Sessions))
	}
	// Two beads on the same target → at least one conflict edge.
	if len(env.Conflicts) == 0 {
		t.Errorf("expected at least 1 conflict edge from shared target")
	}
	// One affinity row per (bead, session) pair = 2 rows.
	if len(env.Affinity) != 2 {
		t.Errorf("expected 2 affinity rows, got %d", len(env.Affinity))
	}
	if len(env.Batches) == 0 {
		t.Errorf("expected at least 1 batch")
	}
}

func TestPlannerCoach_FiltersToReadyBeads(t *testing.T) {
	items := []core.WorkItem{
		{ID: "ready", StateCategory: core.StateUnstarted, Title: "ready"},
		{ID: "in-flight", StateCategory: core.StateStarted, Title: "in-flight"},
		{ID: "completed", StateCategory: core.StateCompleted, Title: "completed"},
		{ID: "canceled", StateCategory: core.StateCanceled, Title: "canceled"},
		{ID: "staged", StateCategory: core.StateStaged, Title: "staged"},
		{ID: "backlog", StateCategory: core.StateBacklog, Title: "backlog"},
	}
	h := newRouterWithBoth(t, items, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/planner/coach", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env coachResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	gotIDs := map[core.WorkItemID]bool{}
	for _, b := range env.ReadyBeads {
		gotIDs[b.ID] = true
	}
	for _, want := range []core.WorkItemID{"ready", "staged", "backlog"} {
		if !gotIDs[want] {
			t.Errorf("expected %q in ready set; got %+v", want, gotIDs)
		}
	}
	for _, drop := range []core.WorkItemID{"in-flight", "completed", "canceled"} {
		if gotIDs[drop] {
			t.Errorf("did not expect %q in ready set", drop)
		}
	}
}
