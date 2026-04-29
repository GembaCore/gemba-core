package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
)

func TestOperationalContext_HappyPath(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	op.SessionsFn = func(_ context.Context, _ core.SessionFilter) ([]core.Session, error) {
		return []core.Session{
			{ID: "sess-1", AgentID: "alice", AssignmentID: "asg-1", Status: core.SessionWorking, StartedAt: time.Now().Add(-time.Hour)},
		}, nil
	}
	h := newRouterWithOrch(t, op)

	req := httptest.NewRequest(http.MethodGet, "/api/operational-context?session_id=sess-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ctx planner.OperationalContext
	if err := json.Unmarshal(rec.Body.Bytes(), &ctx); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if ctx.Session == nil || ctx.Session.ID != "sess-1" {
		t.Errorf("expected session sess-1, got %+v", ctx.Session)
	}
	// Health is derived from the session alone (no profile yet).
	// TimeOnTask should be ~ 1 hour, ContextPressure 0.
	if ctx.Health == nil {
		t.Fatalf("expected non-nil Health")
	}
	if ctx.Health.TimeOnTask < 50*time.Minute {
		t.Errorf("TimeOnTask=%v, expected ~1h", ctx.Health.TimeOnTask)
	}
}

func TestOperationalContext_MissingSessionID(t *testing.T) {
	h := newRouterWithOrch(t, testadaptors.NewFakeOrchestrationPlane(core.TransportAPI))
	req := httptest.NewRequest(http.MethodGet, "/api/operational-context", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperationalContext_NoOrchestrationReturns503(t *testing.T) {
	h := newRouterWithOrch(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/operational-context?session_id=any", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperationalContext_UnknownSessionReturns404(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	op.SessionsFn = func(_ context.Context, _ core.SessionFilter) ([]core.Session, error) {
		return []core.Session{}, nil
	}
	h := newRouterWithOrch(t, op)

	req := httptest.NewRequest(http.MethodGet, "/api/operational-context?session_id=missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}
