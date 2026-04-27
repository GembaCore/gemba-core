// /api/escalations handlers (gm-native.13). Surface the bound
// OrchestrationPlaneAdaptor's escalation index to the SPA and route
// operator replies back to the terminal.
//
// When no orchestration plane is bound, GET returns an empty list
// (200) and POST returns 503 adaptor_not_configured — the SPA
// hides the escalation panel in that case.

package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

func (r *Router) listEscalations(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		writeJSON(w, http.StatusOK, map[string]any{"escalations": []core.EscalationRequest{}, "total": 0})
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		writeJSON(w, http.StatusOK, map[string]any{"escalations": []core.EscalationRequest{}, "total": 0})
		return
	}
	filter := core.EscalationFilter{}
	if v := req.URL.Query().Get("session_id"); v != "" {
		// session_id filter uses ListPendingRequests which is
		// session-scoped; the filter shape on ListOpenEscalations
		// doesn't include session_id (by design — escalations are
		// assignment-scoped in the core model). Route accordingly.
		escs, err := op.ListPendingRequests(req.Context(), v)
		if err != nil {
			httperr.WriteError(w, err)
			return
		}
		if escs == nil {
			escs = []core.EscalationRequest{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"escalations": escs,
			"total":       len(escs),
		})
		return
	}
	escs, err := op.ListOpenEscalations(req.Context(), filter)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if escs == nil {
		escs = []core.EscalationRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"escalations": escs,
		"total":       len(escs),
	})
}

// respondRequest is the wire body of POST /api/escalations/{id}/respond.
// The operator picks a resolution kind (approve / deny / modify /
// defer); modify carries a free-text value the adaptor sends as
// keystrokes.
type respondRequest struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

func (r *Router) respondEscalation(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		httperr.WriteError(w, core.NewAdaptorError(core.KindUnsupported,
			"orchestration plane not configured"))
		return
	}
	id := chi.URLParam(req, "id")
	if id == "" {
		http.Error(w, "escalation id required", http.StatusBadRequest)
		return
	}
	var body respondRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	kind := core.EscalationResolutionKind(body.Kind)
	switch kind {
	case core.ResolutionApprove, core.ResolutionDeny, core.ResolutionModify, core.ResolutionDefer:
	default:
		http.Error(w, "invalid resolution kind", http.StatusBadRequest)
		return
	}
	res := core.EscalationResolution{
		Kind:       kind,
		Value:      body.Value,
		ResolvedAt: time.Now(),
	}
	// Nonce already consumed by requireConfirmNonce middleware.
	esc, err := r.host.OrchestrationPlane().ResolveEscalation(req.Context(), id, res, core.ConfirmNonce(""))
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, esc)
}
