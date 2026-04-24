package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

func newRouterWithOrch(t *testing.T, op core.OrchestrationPlaneAdaptor) *Router {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	if op != nil {
		if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
			t.Fatalf("RegisterOrchestrationPlane: %v", err)
		}
	}
	return NewRouter(config.ServeConfig{}, fakeSPA(), host)
}

func TestListSessions_HappyPath(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	op.SessionsFn = func(_ context.Context, _ core.SessionFilter) ([]core.Session, error) {
		return []core.Session{
			{ID: "s1", AssignmentID: "gm-1", Status: core.SessionWorking, StartedAt: time.Now()},
			{ID: "s2", AssignmentID: "gm-2", Status: core.SessionReady, StartedAt: time.Now()},
		}, nil
	}
	h := newRouterWithOrch(t, op)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Sessions []core.Session `json:"sessions"`
		Total    int            `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Total != 2 || len(env.Sessions) != 2 {
		t.Fatalf("envelope: %+v", env)
	}
}

func TestListSessions_NoOrchestration_EmptyList(t *testing.T) {
	h := newRouterWithOrch(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !contains(rec.Body.String(), `"sessions":[]`) {
		t.Errorf("want empty list, got %q", rec.Body.String())
	}
}

func TestListSessions_FilterPassesThrough(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	var seen core.SessionFilter
	op.SessionsFn = func(_ context.Context, f core.SessionFilter) ([]core.Session, error) {
		seen = f
		return nil, nil
	}
	h := newRouterWithOrch(t, op)

	req := httptest.NewRequest(http.MethodGet,
		"/api/sessions?include_terminal=true&status=working,ready&agent_id=mike", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !seen.IncludeTerminal {
		t.Error("IncludeTerminal not propagated")
	}
	if seen.AgentID != "mike" {
		t.Errorf("AgentID=%q", seen.AgentID)
	}
	if len(seen.Status) != 2 || seen.Status[0] != core.SessionWorking || seen.Status[1] != core.SessionReady {
		t.Errorf("Status=%v", seen.Status)
	}
}

func TestStartSession_NoOrchestration_Errors(t *testing.T) {
	h := newRouterWithOrch(t, nil)
	body := bytes.NewBufferString(`{"bead_id":"gm-1","agent_type":"claude"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Fatalf("want error status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "unsupported") &&
		!contains(rec.Body.String(), "not configured") {
		t.Errorf("envelope missing unsupported/not-configured: %s", rec.Body.String())
	}
}

func TestStartSession_MissingBeadID_400(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	h := newRouterWithOrch(t, op)
	body := bytes.NewBufferString(`{"agent_type":"claude"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "missing_field") {
		t.Errorf("error code missing: %s", rec.Body.String())
	}
}

func TestStartSession_MissingNonce_400(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	h := newRouterWithOrch(t, op)
	body := bytes.NewBufferString(`{"bead_id":"gm-1","agent_type":"claude"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "missing_confirm_nonce") {
		t.Errorf("nonce error missing: %s", rec.Body.String())
	}
}

func TestEndSession_DefaultsToCanceled(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	// Override the default "not implemented" with a stub that returns
	// a session and captures the call. Use an embedded type pattern.
	stub := &endSessionStub{FakeOrchestrationPlane: op}
	h := newRouterWithOrch(t, stub)
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/s-42", nil)
	req.Header.Set(ConfirmHeader, "nonce-3")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if stub.gotMode != core.SessionEndCanceled {
		t.Errorf("default mode=%q, want canceled", stub.gotMode)
	}
	if stub.gotID != "s-42" {
		t.Errorf("id=%q", stub.gotID)
	}
}

func TestEndSession_InvalidMode_400(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	stub := &endSessionStub{FakeOrchestrationPlane: op}
	h := newRouterWithOrch(t, stub)
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/s-1?mode=bogus", nil)
	req.Header.Set(ConfirmHeader, "nonce-4")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// endSessionStub embeds FakeOrchestrationPlane and overrides EndSession
// so the test can capture inputs and avoid the default "not implemented"
// error.
type endSessionStub struct {
	*testadaptors.FakeOrchestrationPlane
	gotID   string
	gotMode core.SessionEndMode
}

func (s *endSessionStub) EndSession(_ context.Context, id string, mode core.SessionEndMode, _ core.ConfirmNonce) (core.Session, error) {
	s.gotID = id
	s.gotMode = mode
	return core.Session{ID: id, Status: core.SessionCompleted}, nil
}
