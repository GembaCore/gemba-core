// Tests for the workspace egress-policy HTTP surface (gm-o9t8.3.6.1).
//
// Coverage:
//   - POST creates a rule and returns 201 + the canonical representation
//   - GET (no flag) lists workspace rules only (no defaults)
//   - GET ?include_defaults=1 surfaces the baseline as a separate field
//   - GET /effective returns the merged + priority-sorted view
//   - DELETE removes a rule; defaults are immutable
//   - Audit events fire for create/delete

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/egress"
	"github.com/GembaCore/gemba-core/internal/server/audit"
)

func newEgressRouter(t *testing.T) (*Router, *egress.MemStore, *capturingAuditor) {
	t.Helper()
	store := egress.NewMemStore()
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachEgressStore(store)
	ca := &capturingAuditor{}
	r.AttachEgressAuditor(ca.emit)
	t.Cleanup(r.Close)
	return r, store, ca
}

type capturingAuditor struct {
	mu     sync.Mutex
	events []audit.Event
}

func (c *capturingAuditor) emit(ev audit.Event, _ string, _ egress.Rule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *capturingAuditor) snapshot() []audit.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]audit.Event, len(c.events))
	copy(out, c.events)
	return out
}

func doEgress(t *testing.T, r *Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodDelete {
		req.Header.Set(ConfirmHeader, "aaaaaaaa-bbbb-4ccc-8ddd-"+randHex12(t))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestEgress_CreateListDelete(t *testing.T) {
	r, _, ca := newEgressRouter(t)

	// Create
	rec := doEgress(t, r, http.MethodPost,
		"/api/v1/workspaces/ws1/egress-rules",
		`{"id":"r1","action":"allow","proto":"tcp","host_pattern":"internal.corp","port_start":443,"port_end":443,"priority":10}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%q", rec.Code, rec.Body.String())
	}

	// List (no defaults)
	rec = doEgress(t, r, http.MethodGet, "/api/v1/workspaces/ws1/egress-rules", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%q", rec.Code, rec.Body.String())
	}
	var listed struct {
		Rules    []egress.Rule `json:"rules"`
		Defaults []egress.Rule `json:"defaults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Rules) != 1 || listed.Rules[0].ID != "r1" {
		t.Errorf("rules = %+v", listed.Rules)
	}
	if len(listed.Defaults) != 0 {
		t.Errorf("defaults should be omitted without include_defaults: %+v", listed.Defaults)
	}

	// include_defaults=1
	rec = doEgress(t, r, http.MethodGet,
		"/api/v1/workspaces/ws1/egress-rules?include_defaults=1", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode include: %v", err)
	}
	if len(listed.Defaults) == 0 {
		t.Error("defaults should be included")
	}

	// Delete
	rec = doEgress(t, r, http.MethodDelete,
		"/api/v1/workspaces/ws1/egress-rules/r1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, body=%q", rec.Code, rec.Body.String())
	}

	// Audit: create then delete.
	evs := ca.snapshot()
	if len(evs) != 2 || evs[0] != audit.EventEgressRuleCreate || evs[1] != audit.EventEgressRuleDelete {
		t.Errorf("audit events = %+v, want [create, delete]", evs)
	}
}

func TestEgress_Effective_MergesDefaults(t *testing.T) {
	r, _, _ := newEgressRouter(t)
	// Add a high-priority workspace rule.
	_ = doEgress(t, r, http.MethodPost,
		"/api/v1/workspaces/ws1/egress-rules",
		`{"id":"r-top","action":"allow","proto":"tcp","host_pattern":"internal.corp","port_start":443,"port_end":443,"priority":100}`)

	rec := doEgress(t, r, http.MethodGet,
		"/api/v1/workspaces/ws1/egress-rules/effective", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("effective = %d", rec.Code)
	}
	var out struct {
		Rules []egress.Rule `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Rules) < 2 {
		t.Fatalf("expected workspace rule + defaults, got %d", len(out.Rules))
	}
	if out.Rules[0].ID != "r-top" {
		t.Errorf("top rule = %s, want r-top", out.Rules[0].ID)
	}
}

func TestEgress_DefaultDelete_Rejected(t *testing.T) {
	r, _, _ := newEgressRouter(t)
	def := egress.Defaults()[0]
	rec := doEgress(t, r, http.MethodDelete,
		"/api/v1/workspaces/ws1/egress-rules/"+def.ID, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete default = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
}

func TestEgress_StoreNotAttached_503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/workspaces/ws1/egress-rules", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("list without store = %d, want 503", rec.Code)
	}
}

func TestEgress_ValidationErrors(t *testing.T) {
	r, _, _ := newEgressRouter(t)
	bad := []string{
		`{"id":"","action":"allow","proto":"tcp","host_pattern":"x"}`,
		`{"id":"r1","action":"nope","proto":"tcp","host_pattern":"x"}`,
		`{"id":"r1","action":"allow","proto":"","host_pattern":"x"}`,
		`{"id":"r1","action":"allow","proto":"tcp","host_pattern":""}`,
		`{"id":"r1","action":"allow","proto":"tcp","host_pattern":"x","port_start":-1}`,
		`{"id":"r1","action":"allow","proto":"tcp","host_pattern":"x","port_start":500,"port_end":80}`,
		`{"id":"default-anthropic","action":"deny","proto":"tcp","host_pattern":"api.anthropic.com"}`,
	}
	for i, body := range bad {
		rec := doEgress(t, r, http.MethodPost, "/api/v1/workspaces/ws1/egress-rules", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d body=%s code=%d want 400", i, body, rec.Code)
		}
	}
}

func TestEgress_OpenAPI_DocumentsEndpoints(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := doc.Paths["/api/v1/workspaces/{wsid}/egress-rules"]; !ok {
		t.Error("openapi missing /egress-rules")
	}
	if _, ok := doc.Paths["/api/v1/workspaces/{wsid}/egress-rules/{id}"]; !ok {
		t.Error("openapi missing /egress-rules/{id}")
	}
	if _, ok := doc.Paths["/api/v1/workspaces/{wsid}/egress-rules/effective"]; !ok {
		t.Error("openapi missing /egress-rules/effective")
	}
}
