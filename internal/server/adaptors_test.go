package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// gm-b1: /api/adaptors reports per-adaptor health with the shape the SPA
// banner depends on. The endpoint must succeed (200) even when some
// adaptors are degraded — a degraded backend is a banner, not a 500.
func TestAdaptorsEndpoint_ReportsPerAdaptorHealth(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)

	registry.Register(registry.Adaptor{
		Name:  "beads",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult {
			return registry.DetectResult{Ok: false, Reason: "daemon offline"}
		},
	})
	registry.Register(registry.Adaptor{
		Name:  "gastown",
		Plane: registry.OrchestrationPlane,
		Probe: func() registry.DetectResult { return registry.DetectResult{Ok: true} },
	})

	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/adaptors", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 even with a degraded adaptor, got %d; body=%q", rec.Code, rec.Body.String())
	}

	var env struct {
		Adaptors []registry.AdaptorStatus `json:"adaptors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must parse: %v; body=%q", err, rec.Body.String())
	}
	if len(env.Adaptors) != 2 {
		t.Fatalf("want 2 adaptors, got %d: %+v", len(env.Adaptors), env.Adaptors)
	}

	byName := map[string]registry.AdaptorStatus{}
	for _, a := range env.Adaptors {
		byName[a.Name] = a
	}
	if byName["beads"].Healthy {
		t.Errorf("beads should be unhealthy, got %+v", byName["beads"])
	}
	if byName["beads"].Reason != "daemon offline" {
		t.Errorf("beads reason should reach the wire; got %q", byName["beads"].Reason)
	}
	if !byName["gastown"].Healthy {
		t.Errorf("gastown should be healthy, got %+v", byName["gastown"])
	}
}

// /api/adaptors must filter to adaptors actually bound on the host.
// init()-registered adaptors that the operator didn't pick (the
// sibling work adaptor, uninstalled orchestrators, etc.) belong to
// `gemba doctor`'s discovery view, not the runtime banner. Without
// the filter the banner used to scold operators about
// "beads-dolt: not configured" when they had picked --beads-dir.
func TestAdaptorsEndpoint_FiltersToBoundAdaptors(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)

	// Three adaptors self-register, mirroring the production set
	// (bd CLI, dolt SQL, an orchestrator).
	registry.Register(registry.Adaptor{
		Name:  "fake",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult { return registry.DetectResult{Ok: true} },
	})
	registry.Register(registry.Adaptor{
		Name:  "beads-dolt",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult {
			return registry.DetectResult{Ok: false, Reason: "not configured (sibling)"}
		},
	})
	registry.Register(registry.Adaptor{
		Name:  "gascity",
		Plane: registry.OrchestrationPlane,
		Probe: func() registry.DetectResult {
			return registry.DetectResult{Ok: false, Reason: "gc CLI not on PATH"}
		},
	})

	// Bind only the "fake" work adaptor to a Host. The dolt sibling and
	// the orchestrator stay registered but unbound.
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}

	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	req := httptest.NewRequest(http.MethodGet, "/api/adaptors", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Adaptors []registry.AdaptorStatus `json:"adaptors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must parse: %v; body=%q", err, rec.Body.String())
	}
	if len(env.Adaptors) != 1 {
		t.Fatalf("expected the bound work adaptor only, got %d: %+v",
			len(env.Adaptors), env.Adaptors)
	}
	if env.Adaptors[0].Name != "fake" {
		t.Fatalf("want fake, got %q", env.Adaptors[0].Name)
	}
}

// When no host is wired (early boot, doctor mode), fall through to the
// raw registry so an operator probing /api/adaptors before bind sees
// the discovery view. Pinning this so the filter can't accidentally
// hide everything.
func TestAdaptorsEndpoint_FallsThroughWhenNoHost(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)
	registry.Register(registry.Adaptor{
		Name:  "fake",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult { return registry.DetectResult{Ok: true} },
	})

	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/adaptors", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var env struct {
		Adaptors []registry.AdaptorStatus `json:"adaptors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if len(env.Adaptors) != 1 {
		t.Fatalf("nil-host fallback should keep returning the registry; got %d", len(env.Adaptors))
	}
}

// gm-b1: The mutation middleware rejects writes against a degraded
// adaptor with a clear 503 envelope — no 30s hang, no opaque 5xx.
func TestRequireHealthyAdaptor_Rejects503OnDegraded(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)

	registry.Register(registry.Adaptor{
		Name:  "beads",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult {
			return registry.DetectResult{Ok: false, Reason: "Dolt unreachable"}
		},
	})

	r := chi.NewRouter()
	called := false
	r.With(requireHealthyAdaptor(registry.WorkPlane)).
		Post("/beads", func(http.ResponseWriter, *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodPost, "/beads", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatalf("inner handler must not run when adaptor is degraded")
	}

	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must parse: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "adaptor_degraded" {
		t.Errorf("want error=adaptor_degraded, got %v", env["error"])
	}
	if env["adaptor"] != "beads" {
		t.Errorf("want adaptor=beads, got %v", env["adaptor"])
	}
	if env["reason"] != "Dolt unreachable" {
		t.Errorf("reason must reach the wire; got %v", env["reason"])
	}
}

// gm-b1: When the plane's adaptor is healthy, the middleware is a no-op
// and the inner handler runs normally.
func TestRequireHealthyAdaptor_AllowsWhenHealthy(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)

	registry.Register(registry.Adaptor{
		Name:  "beads",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult { return registry.DetectResult{Ok: true} },
	})

	r := chi.NewRouter()
	r.With(requireHealthyAdaptor(registry.WorkPlane)).
		Post("/beads", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})

	req := httptest.NewRequest(http.MethodPost, "/beads", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201 when adaptor is healthy, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// gm-b1: With no adaptor registered for the plane, the middleware
// reports a distinct "not configured" error so operators can tell
// "misconfigured" apart from "degraded". Both still 503 — they're both
// unusable — but the reason is different.
func TestRequireHealthyAdaptor_NotConfigured(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)

	r := chi.NewRouter()
	r.With(requireHealthyAdaptor(registry.WorkPlane)).
		Post("/beads", func(http.ResponseWriter, *http.Request) {})

	req := httptest.NewRequest(http.MethodPost, "/beads", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must parse: %v", err)
	}
	if env["error"] != "adaptor_not_configured" {
		t.Errorf("want error=adaptor_not_configured, got %v", env["error"])
	}
}
