package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// startSessionStub captures every StartSession call so the manual-
// session tests can assert on the synthetic AssignmentID + threaded
// SessionPrompt without real adaptor state.
type startSessionStub struct {
	*testadaptors.FakeOrchestrationPlane
	gotAssignment string
	gotPrompt     core.SessionPrompt
}

func (s *startSessionStub) StartSession(_ context.Context, assignmentID string, prompt core.SessionPrompt) (core.Session, error) {
	s.gotAssignment = assignmentID
	s.gotPrompt = prompt
	return core.Session{
		ID:           "sess-" + assignmentID,
		AssignmentID: assignmentID,
		Status:       core.SessionReady,
		StartedAt:    time.Now(),
	}, nil
}

// gm-hmqj — manual session launcher path.

func TestStartSession_ManualHappyPath(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	stub := &startSessionStub{FakeOrchestrationPlane: op}
	h := newRouterWithOrch(t, stub)

	body := bytes.NewBufferString(`{
		"kind": "manual",
		"agent_type": "claude",
		"persona_id": "coach",
		"repository_id": "gemba",
		"prompt": "Explore the auth subsystem"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-manual-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(stub.gotAssignment, "manual-") {
		t.Errorf("assignment id should be synthetic and prefixed; got %q", stub.gotAssignment)
	}
	if stub.gotPrompt.Text != "Explore the auth subsystem" {
		t.Errorf("prompt text not threaded; got %q", stub.gotPrompt.Text)
	}
	if stub.gotPrompt.Extension["gemba:kind"] != "manual" {
		t.Errorf("manual marker missing from prompt extension: %+v", stub.gotPrompt.Extension)
	}
	if stub.gotPrompt.Extension["gemba:repository_id"] != "gemba" {
		t.Errorf("repository_id not threaded: %+v", stub.gotPrompt.Extension)
	}
	if stub.gotPrompt.Extension["gemba:persona_id"] != "coach" {
		t.Errorf("persona_id not threaded: %+v", stub.gotPrompt.Extension)
	}
}

func TestStartSession_ManualMissingPrompt_400(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	h := newRouterWithOrch(t, op)
	body := bytes.NewBufferString(`{
		"kind": "manual",
		"agent_type": "claude",
		"repository_id": "gemba"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-manual-2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400; got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "missing_field") {
		t.Errorf("envelope missing missing_field code: %s", rec.Body.String())
	}
}

func TestStartSession_ManualMissingRepository_400(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	h := newRouterWithOrch(t, op)
	body := bytes.NewBufferString(`{
		"kind": "manual",
		"agent_type": "claude",
		"prompt": "do a thing"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-manual-3")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStartSession_ManualWithBeadID_400(t *testing.T) {
	// kind=manual + bead_id is contradictory — surface a clear error
	// instead of silently dropping one of the fields.
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	h := newRouterWithOrch(t, op)
	body := bytes.NewBufferString(`{
		"kind": "manual",
		"agent_type": "claude",
		"repository_id": "gemba",
		"prompt": "x",
		"bead_id": "gm-1"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-manual-4")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400; got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "unexpected_field") {
		t.Errorf("envelope should call out unexpected_field: %s", rec.Body.String())
	}
}

func TestStartSession_UnknownKind_400(t *testing.T) {
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	h := newRouterWithOrch(t, op)
	body := bytes.NewBufferString(`{"kind":"hybrid","agent_type":"claude"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set(ConfirmHeader, "nonce-manual-5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400; got %d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "invalid_kind") {
		t.Errorf("envelope should call out invalid_kind: %s", rec.Body.String())
	}
}

func TestNewManualAssignmentID_Unique(t *testing.T) {
	// Same-tick collision guard: two ids minted back-to-back must
	// differ even when UnixNano is identical.
	a := newManualAssignmentID()
	b := newManualAssignmentID()
	if a == b {
		t.Errorf("two ids collided: %q", a)
	}
	if !strings.HasPrefix(a, "manual-") || !strings.HasPrefix(b, "manual-") {
		t.Errorf("ids missing manual- prefix: %q %q", a, b)
	}
}
