// /api/agent-groups handler (gm-e12.8). Wraps the bound
// OrchestrationPlaneAdaptor's ListGroups with no filter and returns the
// envelope `{groups: AgentGroup[], total: N}`. Drives the SPA's
// AgentGroupBoard view, whose rendering is dispatched on AgentGroup.Mode
// (static | pool | graph) per gm-root DD-7.
//
// When no orchestration plane is bound the endpoint returns an empty
// list (200) — consistent with /api/agents — so the SPA's AgentGroups
// page renders a "no groups" empty state rather than crashing the route.
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

func (r *Router) listAgentGroups(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		writeJSON(w, http.StatusOK, map[string]any{"groups": []core.AgentGroup{}, "total": 0})
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		writeJSON(w, http.StatusOK, map[string]any{"groups": []core.AgentGroup{}, "total": 0})
		return
	}
	groups, err := op.ListGroups(req.Context())
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if groups == nil {
		groups = []core.AgentGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": groups,
		"total":  len(groups),
	})
}
