// /api/sessions handlers (gm-native.15). The SPA's /sessions inventory
// page lists every live pane, lets the operator end any of them, and
// dispatches new sessions for a chosen bead + agent type.
//
// Plane-bound semantics:
//   - No OrchestrationPlane registered → GET returns an empty list,
//     POST + DELETE return 503 adaptor_not_configured. The SPA page
//     still renders ("nothing here yet") instead of erroring.
//   - Adaptor returns a typed error → flows through httperr so the SPA
//     gets the standard {code,message,retryable} envelope.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/go-chi/chi/v5"
)

// listSessions handles GET /api/sessions. Optional query params:
//
//	include_terminal=true  — include completed/failed sessions (off by default)
//	status=working,ready    — comma-separated SessionStatus filter
//	agent_id=<id>           — exact-match agent filter
func (r *Router) listSessions(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []core.Session{}, "total": 0})
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []core.Session{}, "total": 0})
		return
	}
	f := core.SessionFilter{}
	if v := req.URL.Query().Get("include_terminal"); v == "true" || v == "1" {
		f.IncludeTerminal = true
	}
	if v := req.URL.Query().Get("agent_id"); v != "" {
		f.AgentID = core.AgentID(v)
	}
	if v := req.URL.Query().Get("status"); v != "" {
		// Comma-separated; we don't validate values here — unknown
		// strings just match nothing, which is the user's problem.
		for _, s := range splitCSV(v) {
			f.Status = append(f.Status, core.SessionStatus(s))
		}
	}
	sessions, err := op.ListSessions(req.Context(), f)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if sessions == nil {
		sessions = []core.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"total":    len(sessions),
	})
}

// startSessionRequest is the wire body of POST /api/sessions. The
// operator picks a bead + agent type from the SPA dialog; the server
// turns that into the SessionPrompt the adaptor expects (workspace
// auto-provisioned by the adaptor when omitted).
type startSessionRequest struct {
	BeadID    string `json:"bead_id"`
	AgentType string `json:"agent_type"`
	Title     string `json:"title,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// startSession handles POST /api/sessions. Wraps OrchestrationPlane's
// StartSession with the SPA-friendly body shape. The request must be
// gated by requireConfirmNonce — the same nonce is also threaded into
// SessionPrompt.Extension so the adaptor's own dedup table treats
// retries correctly (gm-native.9 contract).
func (r *Router) startSession(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		httperr.WriteError(w, core.NewAdaptorError(core.KindUnsupported,
			"orchestration plane not configured"))
		return
	}
	var body startSessionRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_body",
			"message": "request body must be JSON {bead_id, agent_type}",
		})
		return
	}
	if body.BeadID == "" || body.AgentType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_field",
			"message": "bead_id and agent_type are required",
		})
		return
	}
	nonce := req.Header.Get(ConfirmHeader)
	prompt := core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":    body.BeadID,
			"gemba:agent_type": body.AgentType,
			"gemba:nonce":      nonce,
		},
	}
	if body.Workspace != "" {
		prompt.Extension["gemba:workspace"] = body.Workspace
	}
	if body.Title != "" {
		prompt.Extension["gemba:title"] = body.Title
	}
	// AssignmentID is generally provisioned by the orchestrator from the
	// bead; for the native adaptor's MVP we use bead_id directly so the
	// session record's AssignmentID is operator-meaningful.
	sess, err := r.host.OrchestrationPlane().StartSession(req.Context(), body.BeadID, prompt)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// endSession handles DELETE /api/sessions/{id}. The mode comes from a
// query param (?mode=canceled|completed|failed); default is canceled
// since the SPA's End button is operator-driven (user_stop).
func (r *Router) endSession(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		httperr.WriteError(w, core.NewAdaptorError(core.KindUnsupported,
			"orchestration plane not configured"))
		return
	}
	id := chi.URLParam(req, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_id",
			"message": "session id required",
		})
		return
	}
	mode := core.SessionEndMode(req.URL.Query().Get("mode"))
	switch mode {
	case core.SessionEndCompleted, core.SessionEndFailed, core.SessionEndCanceled:
	case "":
		mode = core.SessionEndCanceled
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_mode",
			"message": "mode must be one of: completed, failed, canceled",
		})
		return
	}
	nonce := core.ConfirmNonce(req.Header.Get(ConfirmHeader))
	sess, err := r.host.OrchestrationPlane().EndSession(req.Context(), id, mode, nonce)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// peekSession handles GET /api/sessions/{id}/peek. Returns a snapshot
// of the live session — currently a transcript tail from the backing
// pane (gm-e7.3). The endpoint is read-only (no nonce gate); it just
// proxies the OrchestrationPlane's PeekSession method, which already
// surfaces the typed envelopes (KindSessionNotFound, KindUnsupported,
// KindAdaptorDegraded) the SPA's drawer differentiates on.
func (r *Router) peekSession(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "orchestration plane not configured")
		return
	}
	id := chi.URLParam(req, "id")
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "session id required")
		return
	}
	peek, err := r.host.OrchestrationPlane().PeekSession(req.Context(), id)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, peek)
}

// splitCSV splits "a,b,c" with whitespace trim. No fancy escapes —
// SessionStatus values are bare lowercase identifiers.
func splitCSV(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if v := trimASCII(s[start:i]); v != "" {
				out = append(out, v)
			}
			start = i + 1
		}
	}
	if v := trimASCII(s[start:]); v != "" {
		out = append(out, v)
	}
	return out
}

func trimASCII(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
