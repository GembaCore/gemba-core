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
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MikeBengtson/gemba/core"
	nativeadapter "github.com/MikeBengtson/gemba/internal/adapter/native"
	"github.com/MikeBengtson/gemba/internal/server/dispatch"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
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

// startSessionRequest is the wire body of POST /api/sessions. Two
// shapes share one endpoint, discriminated by Kind:
//
//	"bead" (default, back-compat): operator picks bead_id + agent_type;
//	  the bead's id doubles as the AssignmentID so the session row is
//	  operator-meaningful.
//
//	"manual" (gm-hmqj): no bead. Operator picks agent_type + a
//	  repository_id (working dir) + a free-form prompt. The server
//	  mints a synthetic AssignmentID — "manual-<unixnano>-<rand4>" —
//	  so the orchestrator can still key on it without colliding with
//	  real bead ids.
type startSessionRequest struct {
	// Kind selects "bead" (default) or "manual". Empty defaults to
	// "bead" so existing /api/sessions callers keep working.
	Kind      string `json:"kind,omitempty"`
	BeadID    string `json:"bead_id,omitempty"`
	AgentType string `json:"agent_type"`
	Title     string `json:"title,omitempty"`
	Workspace string `json:"workspace,omitempty"`

	// PaneID is an explicit reuse-pane override. When set, the server
	// skips the dispatcher policy entirely and threads this directly to
	// the adaptor as gemba:reuse_pane_id. Used when the SPA presents
	// "send to existing session" UI. See gm-root.16.4.
	PaneID string `json:"pane_id,omitempty"`

	// Manual-mode fields. Required when Kind == "manual"; ignored
	// otherwise.
	PersonaID    string `json:"persona_id,omitempty"`
	RepositoryID string `json:"repository_id,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
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
			"message": "request body must be JSON {kind, bead_id|persona_id+repository_id+prompt, agent_type}",
		})
		return
	}
	kind := body.Kind
	if kind == "" {
		kind = "bead"
	}
	switch kind {
	case "bead":
		r.startBeadSession(w, req, body)
	case "manual":
		r.startManualSession(w, req, body)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_kind",
			"message": `kind must be "bead" or "manual"`,
		})
	}
}

func (r *Router) startBeadSession(w http.ResponseWriter, req *http.Request, body startSessionRequest) {
	if body.BeadID == "" || body.AgentType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_field",
			"message": "bead_id and agent_type are required",
		})
		return
	}
	nonce := req.Header.Get(ConfirmHeader)
	op := r.host.OrchestrationPlane()

	// Pick a reuse target. Explicit PaneID overrides the policy. When
	// neither the body nor the policy yields one, the prompt extension
	// is left empty and the adaptor spawns a fresh pane (the historical
	// behavior).
	reusePaneID := body.PaneID
	if reusePaneID == "" {
		reusePaneID = pickReusePane(op, body.AgentType)
	}

	prompt := buildBeadPrompt(body, nonce, reusePaneID)

	// AssignmentID is generally provisioned by the orchestrator from the
	// bead; for the native adaptor's MVP we use bead_id directly so the
	// session record's AssignmentID is operator-meaningful.
	sess, err := op.StartSession(req.Context(), body.BeadID, prompt)
	if err != nil && reusePaneID != "" && body.PaneID == "" && isCapRace(err) {
		// Race: the picked pane filled between policy check and dispatch.
		// Retry with no reuse so a fresh pane absorbs the bead instead
		// of bubbling a confusing 4xx up to the SPA.
		retryPrompt := buildBeadPrompt(body, nonce, "")
		sess, err = op.StartSession(req.Context(), body.BeadID, retryPrompt)
	}
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// buildBeadPrompt assembles the SessionPrompt for a bead-driven start.
// Centralized so the policy + retry path produce identical extension
// maps modulo the reuse_pane_id key.
func buildBeadPrompt(body startSessionRequest, nonce, reusePaneID string) core.SessionPrompt {
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
	if reusePaneID != "" {
		prompt.Extension["gemba:reuse_pane_id"] = reusePaneID
	}
	return prompt
}

// pickReusePane consults the dispatcher policy to find an existing
// intra-parallel session of the right agent type with capacity. Today
// only the native adaptor exposes the candidate set; on adaptors that
// don't, we degrade gracefully to "no reuse target" so dispatch falls
// through to a fresh spawn.
func pickReusePane(op core.OrchestrationPlaneAdaptor, agentType string) string {
	src, ok := op.(interface {
		SessionsByAgentType(string) []nativeadapter.SessionDispatchInfo
	})
	if !ok {
		return ""
	}
	infos := src.SessionsByAgentType(agentType)
	if len(infos) == 0 {
		return ""
	}
	cands := make([]dispatch.Candidate, 0, len(infos))
	for _, info := range infos {
		cands = append(cands, dispatch.Candidate{
			SessionID:   info.SessionID,
			PaneID:      info.PaneID,
			StartedAt:   info.StartedAt,
			InFlight:    info.InFlight,
			MaxParallel: info.MaxParallel,
		})
	}
	pane, ok := dispatch.Pick(cands)
	if !ok {
		return ""
	}
	return pane
}

// isCapRace recognizes the dispatch race surfaced by the native
// adaptor when a picked pane filled between policy and dispatch. The
// adaptor uses KindValidation for both genuine validation errors and
// the race; we treat any KindValidation from a reuse-mode StartSession
// as retryable so the operator never sees a 4xx for a mechanical race.
func isCapRace(err error) bool {
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) {
		return false
	}
	return aerr.Kind == core.KindValidation
}

func (r *Router) startManualSession(w http.ResponseWriter, req *http.Request, body startSessionRequest) {
	if body.AgentType == "" || body.RepositoryID == "" || body.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_field",
			"message": "manual session requires agent_type + repository_id + prompt",
		})
		return
	}
	if body.BeadID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "unexpected_field",
			"message": `bead_id must be empty when kind="manual" (use kind="bead" to dispatch a bead-driven session)`,
		})
		return
	}
	nonce := req.Header.Get(ConfirmHeader)
	prompt := core.SessionPrompt{
		Text: body.Prompt,
		Extension: map[string]any{
			"gemba:kind":          "manual",
			"gemba:agent_type":    body.AgentType,
			"gemba:repository_id": body.RepositoryID,
			"gemba:nonce":         nonce,
		},
	}
	if body.PersonaID != "" {
		prompt.Extension["gemba:persona_id"] = body.PersonaID
	}
	if body.Workspace != "" {
		prompt.Extension["gemba:workspace"] = body.Workspace
	}
	if body.Title != "" {
		prompt.Extension["gemba:title"] = body.Title
	}
	// Synthetic assignment id so a manual session is keyable without
	// colliding with a bead id. The "manual-" prefix lets downstream
	// readers (audit log, retro pipeline) detect manual sessions
	// without inspecting the prompt extensions.
	assignmentID := newManualAssignmentID()
	sess, err := r.host.OrchestrationPlane().StartSession(req.Context(), assignmentID, prompt)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// newManualAssignmentID mints a unique id for a manual session.
// Format: "manual-<unixnano>-<4 hex chars>". The unixnano keeps it
// monotonic per process; the random suffix guards against same-
// nano collisions when a script fires two starts back-to-back.
func newManualAssignmentID() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("manual-%d-%x", time.Now().UnixNano(), buf)
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
