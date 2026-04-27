// /api/v1/* smoke tests (gm-e4.1).
//
// Exercises every canonical /api/v1 route against the noop reference
// adaptor pair (gm-e3.7). Each route's contract:
//
//   - Read endpoints (GET) return 200 + a JSON body that decodes
//     into a known envelope shape (or 200 + a sensible empty when
//     the adaptor is unbound).
//   - Mutating endpoints (POST/PATCH/DELETE) reject without an
//     X-GEMBA-Confirm nonce and accept with one.
//
// The smoke gate is "every route reaches its handler and returns a
// well-formed JSON envelope" — exhaustive per-handler tests live in
// agents_test.go / sprints_test.go / sessions_test.go / etc.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/adapter/noop"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/transport/api"
)

// newNoopRouter builds a Router with the noop reference adaptors
// attached so every handler reaches a real plane.
func newNoopRouter(t *testing.T) http.Handler {
	t.Helper()
	host := api.New()
	wp := noop.NewWorkPlaneWithTransport(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	op := noop.NewOrchestrationPlaneWithTransport(core.TransportAPI)
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}
	return NewRouter(config.ServeConfig{}, fakeSPA(), host)
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: decode JSON: %v\nbody=%s", path, err, rec.Body.String())
	}
	return rec, body
}

// ── canonical surface — read-only ───────────────────────────────

func TestV1Smoke_CapabilitiesIsJSON(t *testing.T) {
	h := newNoopRouter(t)
	rec, body := get(t, h, "/api/v1/capabilities")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/capabilities = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body == nil {
		t.Fatalf("expected JSON body")
	}
}

func TestV1Smoke_AgentsReturnsEnvelope(t *testing.T) {
	h := newNoopRouter(t)
	rec, body := get(t, h, "/api/v1/agents")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/agents = %d", rec.Code)
	}
	if _, ok := body["agents"]; !ok {
		t.Errorf("/api/v1/agents missing 'agents' key: %+v", body)
	}
	if _, ok := body["total"]; !ok {
		t.Errorf("/api/v1/agents missing 'total' key: %+v", body)
	}
}

func TestV1Smoke_GroupsReturnsEnvelope(t *testing.T) {
	h := newNoopRouter(t)
	rec, body := get(t, h, "/api/v1/groups")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/groups = %d", rec.Code)
	}
	if body == nil {
		t.Fatalf("expected JSON body")
	}
}

func TestV1Smoke_SessionsReturnsEnvelope(t *testing.T) {
	h := newNoopRouter(t)
	rec, body := get(t, h, "/api/v1/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/sessions = %d", rec.Code)
	}
	if body == nil {
		t.Fatalf("expected JSON body")
	}
}

func TestV1Smoke_EscalationsReturnsEnvelope(t *testing.T) {
	h := newNoopRouter(t)
	rec, body := get(t, h, "/api/v1/escalations")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/escalations = %d", rec.Code)
	}
	if _, ok := body["escalations"]; !ok {
		t.Errorf("/api/v1/escalations missing 'escalations' key: %+v", body)
	}
}

func TestV1Smoke_SprintsReturnsEnvelope(t *testing.T) {
	h := newNoopRouter(t)
	rec, _ := get(t, h, "/api/v1/sprints")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/sprints = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestV1Smoke_WorkitemsReturnsEnvelope(t *testing.T) {
	h := newNoopRouter(t)
	rec, _ := get(t, h, "/api/v1/workitems")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/workitems = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestV1Smoke_WorkspacesDeclaresKinds(t *testing.T) {
	h := newNoopRouter(t)
	rec, body := get(t, h, "/api/v1/workspaces")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/workspaces = %d", rec.Code)
	}
	kinds, ok := body["workspace_kinds"].([]any)
	if !ok {
		t.Fatalf("/api/v1/workspaces missing 'workspace_kinds' array: %+v", body)
	}
	// noop OrchestrationPlane declares at least one kind.
	if len(kinds) == 0 {
		t.Errorf("expected at least one workspace_kind from noop OP; got %+v", body)
	}
}

// ── nonce gate on mutating routes ───────────────────────────────

func TestV1Smoke_MutationsRequireNonce(t *testing.T) {
	h := newNoopRouter(t)
	cases := []struct {
		method, path, body string
	}{
		{"POST", "/api/v1/sessions", `{}`},
		{"POST", "/api/v1/workitems", `{"title":"x"}`},
		{"PATCH", "/api/v1/workitems/gemba/gemba/gm-1", `{"title":"y"}`},
		{"POST", "/api/v1/escalations/x/respond", `{"kind":"approve"}`},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, bytes.NewBufferString(c.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK || rec.Code == http.StatusCreated || rec.Code == http.StatusAccepted {
			t.Errorf("%s %s without nonce should NOT succeed; got %d body=%s",
				c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// ── unknown /api/v1 route returns the JSON 404 envelope ─────────

func TestV1Smoke_UnknownRouteIsJSON404(t *testing.T) {
	h := newNoopRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown /api/v1 route; got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON envelope; got %s", rec.Body.String())
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("404 envelope missing 'error' key: %+v", body)
	}
}

// ── CORS off-by-default ─────────────────────────────────────────

func TestV1Smoke_CORSOffByDefault(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/capabilities", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if hdr := rec.Header().Get("Access-Control-Allow-Origin"); hdr != "" {
		t.Errorf("CORS off-by-default but got Access-Control-Allow-Origin=%q", hdr)
	}
}

func TestV1Smoke_CORSOptInPropagatesOrigin(t *testing.T) {
	cfg := config.ServeConfig{
		CORSAllowedOrigins: []string{"https://trusted.example.com"},
	}
	h := NewRouter(cfg, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/capabilities", nil)
	req.Header.Set("Origin", "https://trusted.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://trusted.example.com" {
		t.Errorf("CORS opt-in: Access-Control-Allow-Origin = %q, want trusted origin", got)
	}
}

// Sanity assertion: the noop OrchestrationPlane keeps declaring at
// least WorkspaceWorktree so /api/v1/workspaces remains testable.
// If gm-e3.7's noop manifest changes, this catches the drift.
// Sanity assertion: the noop OP keeps declaring at least one
// WorkspaceKind so /api/v1/workspaces stays testable. The noop's
// canonical kind today is WorkspaceSubprocess (gm-e3.7); any kind
// satisfies the smoke test.
func TestV1Smoke_NoopOPDeclaresAtLeastOneWorkspaceKind(t *testing.T) {
	op := noop.NewOrchestrationPlaneWithTransport(core.TransportAPI)
	m := op.Describe()
	if len(m.WorkspaceKinds) == 0 {
		t.Errorf("noop OP must declare at least one WorkspaceKind for the /api/v1/workspaces smoke")
	}
}
