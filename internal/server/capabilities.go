package server

import (
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// capabilitiesResponse is the envelope the SPA reads at /api/capabilities
// (gm-e11.4). Both fields are pointers so the JSON carries explicit null
// when an adaptor is not yet registered — the SPA renders a conservative
// "no capability" state on null rather than inventing defaults.
type capabilitiesResponse struct {
	WorkPlane          *core.CapabilityManifest              `json:"work_plane"`
	OrchestrationPlane *core.OrchestrationCapabilityManifest `json:"orchestration_plane"`
}

// capabilities is /api/capabilities. It exposes both registered plane
// manifests in one envelope so the SPA fires a single request on connect
// and keeps a consistent snapshot across gates (gm-e11.4).
//
// No WorkPlane registered → 503 adaptor_not_configured. A registered
// adaptor whose Describe() fails surfaces as 503 (when tagged
// AdaptorDegraded) or 500 (untagged) through the shared httperr mapper
// (gm-root.1.1). The OrchestrationPlane is optional: its manifest is
// included when an adaptor is registered and omitted (null) otherwise,
// since the SPA's Capability gates treat null as "hide".
func (r *Router) capabilities(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}

	workManifest, err := wp.Describe(req.Context())
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	resp := capabilitiesResponse{WorkPlane: &workManifest}
	if op := r.host.OrchestrationPlane(); op != nil {
		orchManifest := op.Describe()
		resp.OrchestrationPlane = &orchManifest
	}
	writeJSON(w, http.StatusOK, resp)
}
