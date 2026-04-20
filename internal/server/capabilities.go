package server

import (
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
)

// capabilitiesResponse is the envelope the SPA reads at /api/capabilities
// (gm-e11.4). Both fields are pointers so the JSON carries explicit null
// when an adaptor is not yet registered — the SPA renders a conservative
// "no capability" state on null rather than inventing defaults.
type capabilitiesResponse struct {
	WorkPlane          *core.CapabilityManifest              `json:"work_plane"`
	OrchestrationPlane *core.OrchestrationCapabilityManifest `json:"orchestration_plane"`
}

// capabilitiesEndpoint is /api/capabilities. It exposes both manifests in
// one envelope so the SPA fires a single request on connect and keeps a
// consistent snapshot across gates.
//
// The registry.Adaptor type only carries Detect/Probe today (the
// Describe() surface lands with the adaptor-runtime epics). Until those
// adaptors implement Describe, this handler returns nulls — which is
// still useful: the SPA's Capability gate treats null as "hide", which
// is the safe default.
//
// When adaptor Describe() wires land, swap the literal nulls here for
// the registry lookup and keep the envelope shape stable.
func capabilitiesEndpoint(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, capabilitiesResponse{
		WorkPlane:          nil,
		OrchestrationPlane: nil,
	})
}
