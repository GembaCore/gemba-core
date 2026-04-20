package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

// gm-e11.4: /api/capabilities returns a JSON envelope with both planes.
// The SPA's Capability gates treat null manifests as "no capability,
// hide the control", so the envelope MUST be reachable even before any
// adaptor wires Describe() — otherwise every gate would evaluate as an
// error and the UI would degrade unnecessarily.
func TestCapabilitiesEndpoint_ReturnsEnvelope(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want application/json, got %q", ct)
	}

	var env struct {
		WorkPlane          *map[string]any `json:"work_plane"`
		OrchestrationPlane *map[string]any `json:"orchestration_plane"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must decode: %v; body=%q", err, rec.Body.String())
	}
	// Both nil is the current expected shape until adaptor.Describe() lands.
	// The important contract is that the keys exist and decode.
	if env.WorkPlane != nil {
		t.Fatalf("expected work_plane=null until adaptor wires Describe(); got %v", env.WorkPlane)
	}
	if env.OrchestrationPlane != nil {
		t.Fatalf("expected orchestration_plane=null; got %v", env.OrchestrationPlane)
	}
}
