// /api/agents handler (gm-root.10). Wraps the bound
// OrchestrationPlaneAdaptor's ListAgents with a zero-valued filter and
// returns the envelope `{agents: AgentRef[], total: N}`. Drives the SPA's
// AssigneePicker / OwnerPicker.
//
// When no orchestration plane is bound the endpoint returns an empty
// list (200) rather than 503 — the operator picker simply has no
// options to choose from, and the freeform AgentEditor remains as the
// fallback. This keeps the SPA's drawer functional in single-plane
// (work-plane-only) deployments.
//
// When the plane is bound but degraded, the adaptor's tagged error
// flows through the shared httperr mapper (503 adaptor_degraded etc.)
// so the SPA can surface the banner.

package server

import (
	"net/http"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

func (r *Router) listAgents(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		writeJSON(w, http.StatusOK, map[string]any{"agents": []core.AgentRef{}, "total": 0})
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		writeJSON(w, http.StatusOK, map[string]any{"agents": []core.AgentRef{}, "total": 0})
		return
	}
	agents, err := op.ListAgents(req.Context(), core.AgentFilter{})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if agents == nil {
		agents = []core.AgentRef{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agents": agents,
		"total":  len(agents),
	})
}
