package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/workflow"
)

// stubRunner is a minimal pattern-matching runner the workflow handler
// tests reuse. Mirrors the one in internal/workflow/client_test.go but
// kept local so the test packages don't depend on each other.
type wfStubRunner struct {
	stubs map[string]wfStub
}

type wfStub struct {
	out []byte
	err error
}

func newWFRunner() *wfStubRunner {
	return &wfStubRunner{stubs: map[string]wfStub{}}
}

func (s *wfStubRunner) on(prefix string, out []byte, err error) *wfStubRunner {
	s.stubs[prefix] = wfStub{out: out, err: err}
	return s
}

func (s *wfStubRunner) Run() workflow.Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		// longest-prefix match wins so "mol show foo --json" beats "mol".
		var bestKey string
		var best wfStub
		matched := false
		for k, v := range s.stubs {
			if strings.HasPrefix(joined, k) && len(k) > len(bestKey) {
				bestKey = k
				best = v
				matched = true
			}
		}
		if !matched {
			return nil, errors.New("wfStubRunner: no stub for " + joined)
		}
		return best.out, best.err
	}
}

func newWorkflowRouter(t *testing.T, runner workflow.Runner) *Router {
	t.Helper()
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	h.AttachWorkflowClient(workflow.NewClientWithRunner(runner))
	return h
}

// GET /api/workflows/formulas — happy path.
func TestListWorkflowFormulas_HappyPath(t *testing.T) {
	r := newWFRunner().on("formula list --json", []byte(`[
		{"name":"shiny","type":"workflow","version":1,"description":"d","source":"/p/s.toml"}
	]`), nil)
	h := newWorkflowRouter(t, r.Run())

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/formulas", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Formulas []workflow.Formula `json:"formulas"`
		Total    int                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 1 || env.Formulas[0].Name != "shiny" {
		t.Fatalf("envelope: %+v", env)
	}
}

// No client wired → 503 adaptor_not_configured.
func TestListWorkflowFormulas_NoClient(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/formulas", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

// bd subprocess error → 503 adaptor_degraded.
func TestListWorkflowFormulas_BdError(t *testing.T) {
	r := newWFRunner().on("formula list --json", nil, errors.New("dolt down"))
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/formulas", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// GET /api/workflows/formulas/{name} — happy path with cook override.
func TestGetWorkflowFormula_HappyPath(t *testing.T) {
	r := newWFRunner().
		on("formula show shiny --json", []byte(`{"formula":"shiny","type":"workflow","version":1,"description":"d","source":"/p/s.toml","steps":[{"id":"a","title":"A"}]}`), nil).
		on("cook shiny --json", []byte(`{"steps":[{"id":"a","title":"A"},{"id":"b","title":"B","needs":["a"]}]}`), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/formulas/shiny", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var f workflow.Formula
	_ = json.Unmarshal(rec.Body.Bytes(), &f)
	if f.Name != "shiny" || len(f.Steps) != 2 {
		t.Fatalf("formula: %+v", f)
	}
}

// 404 on unknown formula.
func TestGetWorkflowFormula_NotFound(t *testing.T) {
	r := newWFRunner().on("formula show ghost --json", nil, errors.New("formula not found: ghost"))
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/formulas/ghost", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// GET /api/workflows/runs — happy path with default (active) filter.
func TestListWorkflowRuns_DefaultActive(t *testing.T) {
	r := newWFRunner().
		on("list --type molecule --json --status open", []byte(`[
			{"id":"gm-mol1","title":"P","status":"open","created_at":"2026-04-01T00:00:00Z"}
		]`), nil).
		on("mol wisp list --json", []byte(`{"wisps":[
			{"id":"gm-wisp-1","title":"W1","status":"open","created_at":"2026-04-02T00:00:00Z"}
		]}`), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/runs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Runs  []workflow.Run `json:"runs"`
		Total int            `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Total != 2 {
		t.Fatalf("expected 2 runs, got %d", env.Total)
	}
}

// Unknown status filter → 400.
func TestListWorkflowRuns_BadFilter(t *testing.T) {
	h := newWorkflowRouter(t, newWFRunner().Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/runs?status=bogus", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// GET /api/workflows/runs/{id} — happy path.
func TestGetWorkflowRun_HappyPath(t *testing.T) {
	r := newWFRunner().on("mol show gm-wisp-x --json", []byte(`{
		"root":{"id":"gm-wisp-x","title":"x","status":"open","ephemeral":true},
		"issues":[{"id":"gm-wisp-x","title":"x","status":"open","ephemeral":true},
		          {"id":"gm-wisp-x-s1","title":"S1","status":"open"}]
	}`), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/runs/gm-wisp-x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var run workflow.Run
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if run.ID != "gm-wisp-x" || !run.Ephemeral {
		t.Fatalf("run: %+v", run)
	}
}

// 404 on unknown run.
func TestGetWorkflowRun_NotFound(t *testing.T) {
	r := newWFRunner().on("mol show ghost --json", []byte(`{"error":"molecule 'ghost' not found"}`), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodGet, "/api/workflows/runs/ghost", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

// POST /api/workflows/runs without X-GEMBA-Confirm → 400 missing_confirm_nonce.
func TestStartWorkflowRun_MissingNonce(t *testing.T) {
	h := newWorkflowRouter(t, newWFRunner().Run())
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/runs",
		strings.NewReader(`{"formula":"shiny","ephemeral":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing_confirm_nonce") {
		t.Fatalf("body missing missing_confirm_nonce: %q", rec.Body.String())
	}
}

// POST /api/workflows/runs ephemeral=true with nonce → 201, dispatches wisp.
func TestStartWorkflowRun_WispHappyPath(t *testing.T) {
	r := newWFRunner().
		on("mol wisp shiny --json", []byte(`{"id":"gm-wisp-new"}`), nil).
		on("mol show gm-wisp-new --json", []byte(`{
			"root":{"id":"gm-wisp-new","title":"mol-shiny","status":"open","ephemeral":true},
			"issues":[{"id":"gm-wisp-new","title":"mol-shiny","status":"open","ephemeral":true}]
		}`), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/runs",
		strings.NewReader(`{"formula":"shiny","ephemeral":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GEMBA-Confirm", "nonce-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	var run workflow.Run
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if run.ID != "gm-wisp-new" || !run.Ephemeral {
		t.Fatalf("run: %+v", run)
	}
}

// POST /api/workflows/runs missing formula → 400.
func TestStartWorkflowRun_MissingFormula(t *testing.T) {
	h := newWorkflowRouter(t, newWFRunner().Run())
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/runs",
		strings.NewReader(`{"ephemeral":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GEMBA-Confirm", "nonce-2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// POST /api/workflows/runs/{id}/burn without nonce → 400 missing_confirm_nonce.
func TestBurnWorkflowRun_MissingNonce(t *testing.T) {
	h := newWorkflowRouter(t, newWFRunner().Run())
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/runs/gm-wisp-x/burn", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing_confirm_nonce") {
		t.Fatalf("body: %q", rec.Body.String())
	}
}

// POST /api/workflows/runs/{id}/burn on a persistent mol → 409 burn_not_wisp.
func TestBurnWorkflowRun_PersistentMolReturns409(t *testing.T) {
	r := newWFRunner().on("mol show gm-mol-x --json", []byte(`{
		"root":{"id":"gm-mol-x","title":"x","status":"open"},
		"issues":[{"id":"gm-mol-x","title":"x","status":"open"}]
	}`), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/runs/gm-mol-x/burn", nil)
	req.Header.Set("X-GEMBA-Confirm", "nonce-3")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "burn_not_wisp") {
		t.Fatalf("body missing burn_not_wisp: %q", rec.Body.String())
	}
}

// POST /api/workflows/runs/{id}/burn on a wisp → 200.
func TestBurnWorkflowRun_HappyPath(t *testing.T) {
	r := newWFRunner().
		on("mol show gm-wisp-x --json", []byte(`{
			"root":{"id":"gm-wisp-x","title":"x","status":"open","ephemeral":true},
			"issues":[{"id":"gm-wisp-x","title":"x","status":"open","ephemeral":true}]
		}`), nil).
		on("mol burn gm-wisp-x --force", []byte(``), nil)
	h := newWorkflowRouter(t, r.Run())
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/runs/gm-wisp-x/burn", nil)
	req.Header.Set("X-GEMBA-Confirm", "nonce-4")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), `"burned":true`) {
		t.Fatalf("body: %s", body)
	}
}
