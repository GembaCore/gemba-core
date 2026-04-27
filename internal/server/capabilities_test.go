package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// beadsLikeManifest is a stand-in for the real beads adaptor manifest:
// it exercises the non-trivial manifest fields (state_map, feature gates,
// extensions) the SPA negotiates on, so the handler test catches wire-
// shape regressions without depending on the bd adaptor itself.
func beadsLikeManifest() core.CapabilityManifest {
	return core.CapabilityManifest{
		AdaptorName:     "beads",
		AdaptorVersion:  "0.2.0",
		ProtocolVersion: core.ProtocolVersion,
		Transport:       core.TransportAPI,
		StateMap: core.StateMap{
			"open":        core.StateBacklog,
			"in_progress": core.StateStarted,
			"closed":      core.StateCompleted,
		},
		EdgeExtensions: []core.EdgeExtension{
			{Name: "depends_on", Directed: true, Inverse: "blocks"},
		},
		FieldExtensions: []core.FieldExtension{
			{Name: "beads:priority", Type: "number"},
		},
		SprintNative:              false,
		TokenBudgetEnforced:       false,
		EvidenceSynthesisRequired: true,
	}
}

func newCapabilitiesHost(t *testing.T, m core.CapabilityManifest, describeErr error) *api.Host {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.Manifest = m
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	// Swap in a post-registration Describe error AFTER registration so
	// negotiation still succeeds; the error surfaces on the next /api/capabilities
	// request that calls Describe live.
	wp.DescribeErr = describeErr
	return host
}

// gm-6p7: /api/capabilities returns the registered adaptor's live
// CapabilityManifest under work_plane, matching what Describe() reports.
func TestCapabilitiesEndpoint_ReturnsWorkPlaneManifest(t *testing.T) {
	host := newCapabilitiesHost(t, beadsLikeManifest(), nil)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

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
		WorkPlane          *core.CapabilityManifest              `json:"work_plane"`
		OrchestrationPlane *core.OrchestrationCapabilityManifest `json:"orchestration_plane"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env.WorkPlane == nil {
		t.Fatalf("want work_plane manifest, got null; body=%q", rec.Body.String())
	}
	if env.WorkPlane.AdaptorName != "beads" {
		t.Fatalf("adaptor_name: want beads, got %q", env.WorkPlane.AdaptorName)
	}
	if env.WorkPlane.Transport != core.TransportAPI {
		t.Fatalf("transport: want api, got %q", env.WorkPlane.Transport)
	}
	if got := env.WorkPlane.StateMap["in_progress"]; got != core.StateStarted {
		t.Fatalf("state_map[in_progress]: want started, got %q", got)
	}
	if len(env.WorkPlane.EdgeExtensions) != 1 ||
		env.WorkPlane.EdgeExtensions[0].Name != "depends_on" {
		t.Fatalf("edge_extensions missing depends_on: %v", env.WorkPlane.EdgeExtensions)
	}
	if len(env.WorkPlane.FieldExtensions) != 1 ||
		env.WorkPlane.FieldExtensions[0].Name != "beads:priority" {
		t.Fatalf("field_extensions missing beads:priority: %v", env.WorkPlane.FieldExtensions)
	}
	if !env.WorkPlane.EvidenceSynthesisRequired {
		t.Fatalf("evidence_synthesis_required: want true")
	}
	// Orchestration is not registered in this test — null is expected.
	if env.OrchestrationPlane != nil {
		t.Fatalf("want orchestration_plane=null; got %+v", env.OrchestrationPlane)
	}
}

// When an OrchestrationPlane is also registered, its manifest populates
// orchestration_plane so the SPA can gate runtime controls in one fetch.
func TestCapabilitiesEndpoint_IncludesOrchestrationPlane(t *testing.T) {
	host := newCapabilitiesHost(t, beadsLikeManifest(), nil)
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}

	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		OrchestrationPlane *core.OrchestrationCapabilityManifest `json:"orchestration_plane"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env.OrchestrationPlane == nil {
		t.Fatalf("want orchestration_plane manifest, got null; body=%q", rec.Body.String())
	}
	if env.OrchestrationPlane.AdaptorID != "fake-orch" {
		t.Fatalf("adaptor_id: want fake-orch, got %q", env.OrchestrationPlane.AdaptorID)
	}
}

// With no Host wired the endpoint returns 503 adaptor_not_configured —
// the same envelope the single-bead handler emits, so SPA error handling
// is uniform across data routes.
func TestCapabilitiesEndpoint_NoHost_Returns503(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "adaptor_not_configured" {
		t.Fatalf("want error=adaptor_not_configured, got %v", env["error"])
	}
}

// A Host with no WorkPlane registered still reports adaptor_not_configured
// — the SPA can't distinguish "no host" from "no adaptor" and shouldn't
// need to.
func TestCapabilitiesEndpoint_NoWorkPlane_Returns503(t *testing.T) {
	host := api.New() // no RegisterWorkPlane
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "adaptor_not_configured" {
		t.Fatalf("want error=adaptor_not_configured, got %v", env["error"])
	}
}

// Describe() returning a tagged AdaptorDegraded surfaces as 503 through
// the shared httperr mapper, matching the single-bead contract.
func TestCapabilitiesEndpoint_AdaptorDegraded_Returns503(t *testing.T) {
	host := newCapabilitiesHost(t, beadsLikeManifest(),
		core.NewAdaptorError(core.KindAdaptorDegraded,
			"bd probe timed out; Dolt may be hung"))
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "adaptor_degraded" {
		t.Fatalf("want error=adaptor_degraded, got %v", env["error"])
	}
	if env["message"] != "bd probe timed out; Dolt may be hung" {
		t.Fatalf("want original message, got %v", env["message"])
	}
}

// An untagged Describe() error violates Conformance Group F and surfaces
// as 500 — same rule the single-bead handler enforces.
func TestCapabilitiesEndpoint_UntaggedError_Returns500(t *testing.T) {
	host := newCapabilitiesHost(t, beadsLikeManifest(), errors.New("boom"))
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "internal" {
		t.Fatalf("want error=internal, got %v", env["error"])
	}
}
