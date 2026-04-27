package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// gm-root.11: /api/sprints wraps WorkPlane.ListSprints. Returns the
// envelope shape the SprintPicker hooks off of.
func TestListSprints_HappyPath(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	t1, _ := time.Parse(time.RFC3339, "2026-04-01T00:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2026-04-15T00:00:00Z")
	t3, _ := time.Parse(time.RFC3339, "2026-04-16T00:00:00Z")
	t4, _ := time.Parse(time.RFC3339, "2026-04-30T00:00:00Z")
	wp.SprintsFn = func(_ context.Context) ([]core.Sprint, error) {
		return []core.Sprint{
			{ID: "sp-1", Name: "Sprint 1", StartsAt: t1, EndsAt: t2},
			{ID: "sp-2", Name: "Sprint 2", StartsAt: t3, EndsAt: t4},
		}, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/sprints", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Sprints []core.Sprint `json:"sprints"`
		Total   int           `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 2 || len(env.Sprints) != 2 {
		t.Fatalf("envelope shape mismatch: %+v", env)
	}
	if env.Sprints[0].ID != "sp-1" {
		t.Fatalf("first sprint: %+v", env.Sprints[0])
	}
}

// Adaptor with sprint_native=false (the bd default) returns nil; the
// handler must normalise to an empty array so the SPA renders []
// rather than null.
func TestListSprints_NoSprints_ReturnsEmptyArray(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	// SprintsFn left nil → defaults to (nil, nil)
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/sprints", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), `"sprints":[]`) {
		t.Fatalf("want empty sprints array; body=%q", rec.Body.String())
	}
}

// No host wired → 503 adaptor_not_configured. Mirrors the
// /api/work-items handler's behaviour so the SPA's degraded-state
// handling is uniform.
func TestListSprints_NoHost_Returns503(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/sprints", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

// gm-j51y: workspaces with sprint_native=false (bd, dolt) surface
// the capability guard's denial as an AdaptorError. The handler MUST
// translate that into a 200-empty list so the SPA's SprintPicker
// hides cleanly without flooding the console with 500s on every
// page load.
func TestListSprints_CapabilityDenied_Returns200Empty(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.SprintsFn = func(context.Context) ([]core.Sprint, error) {
		return nil, core.NewAdaptorError(core.KindCapabilityDenied,
			"list_sprints denied: manifest sprint_native=false")
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/sprints", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 on capability_denied (gm-j51y), got %d body=%q",
			rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), `"sprints":[]`) {
		t.Fatalf("want empty sprints array; body=%q", rec.Body.String())
	}
	if !contains(rec.Body.String(), `"total":0`) {
		t.Fatalf("want total:0; body=%q", rec.Body.String())
	}
}

// Other adaptor errors (process_failed, request_failed, etc.) MUST
// still surface — only KindCapabilityDenied is the contract-aware
// "this adaptor doesn't have sprints" signal.
func TestListSprints_OtherAdaptorErrorStillSurfaces(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.SprintsFn = func(context.Context) ([]core.Sprint, error) {
		return nil, core.NewAdaptorError(core.KindAdaptorDegraded,
			"upstream sprints API unreachable")
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/sprints", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("KindAdaptorDegraded must NOT collapse to 200; got %d", rec.Code)
	}
}
