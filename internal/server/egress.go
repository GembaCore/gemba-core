// HTTP surface for the workspace egress-policy store (gm-o9t8.3.6.1).
//
// Wire shape (all under the tenant-aware /api block):
//
//   - GET    /api/v1/workspaces/{wsid}/egress-rules            → list workspace rules
//   - GET    /api/v1/workspaces/{wsid}/egress-rules?include_defaults=1
//     → list workspace rules + the baseline as a separate `defaults` field
//   - POST   /api/v1/workspaces/{wsid}/egress-rules            → create (nonce-gated)
//   - DELETE /api/v1/workspaces/{wsid}/egress-rules/{id}       → delete (nonce-gated)
//   - GET    /api/v1/workspaces/{wsid}/egress-rules/effective  → joined + priority-sorted preview
//
// Runtime enforcement (nftables / Firecracker rate limiter) is the
// follow-on story gm-o9t8.3.6.2. This handler is read/write storage
// only — no traffic ever touches it.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/egress"
	"github.com/GembaCore/gemba-core/internal/server/audit"
)

// EgressAuditor receives one event per egress-rule mutation. Bound at
// boot via AttachEgressAuditor; nil is fine (handlers skip emission).
type EgressAuditor func(event audit.Event, wsid string, rule egress.Rule)

// AttachEgressStore binds the egress storage implementation to the
// router. Until called, all four egress endpoints return 503
// adaptor_not_configured.
func (r *Router) AttachEgressStore(s egress.Store) { r.egressStore = s }

// AttachEgressAuditor binds the audit emitter. nil clears it.
func (r *Router) AttachEgressAuditor(a EgressAuditor) { r.egressAuditor = a }

// emitEgressAudit fires only when an auditor is bound. Centralized so
// the handlers stay terse.
func (r *Router) emitEgressAudit(event audit.Event, wsid string, rule egress.Rule) {
	if r.egressAuditor != nil {
		r.egressAuditor(event, wsid, rule)
	}
}

func (r *Router) egressUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":   "adaptor_not_configured",
		"message": "egress store not attached",
	})
}

// listEgressRules — GET /api/v1/workspaces/{wsid}/egress-rules.
// When ?include_defaults=1 the response includes a separate `defaults`
// field carrying the hard-coded baseline; the `rules` field is always
// the workspace-owned rules only.
func (r *Router) listEgressRules(w http.ResponseWriter, req *http.Request) {
	if r.egressStore == nil {
		r.egressUnavailable(w)
		return
	}
	wsid := chi.URLParam(req, "wsid")
	rules, err := r.egressStore.List(req.Context(), wsid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	if rules == nil {
		rules = []egress.Rule{}
	}
	out := map[string]any{"rules": rules}
	if req.URL.Query().Get("include_defaults") == "1" {
		out["defaults"] = egress.Defaults()
	}
	writeJSON(w, http.StatusOK, out)
}

// effectiveEgressRules — GET /api/v1/workspaces/{wsid}/egress-rules/effective.
// Returns the merged + priority-sorted view (workspace ∪ defaults).
func (r *Router) effectiveEgressRules(w http.ResponseWriter, req *http.Request) {
	if r.egressStore == nil {
		r.egressUnavailable(w)
		return
	}
	wsid := chi.URLParam(req, "wsid")
	rules, err := r.egressStore.Effective(req.Context(), wsid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// egressRuleCreateRequest is the POST body for create. Fields mirror
// Rule with the workspace_id stripped (the path supplies it) and
// without server-owned bookkeeping fields.
type egressRuleCreateRequest struct {
	ID          string        `json:"id"`
	Action      egress.Action `json:"action"`
	Proto       egress.Proto  `json:"proto"`
	HostPattern string        `json:"host_pattern"`
	PortStart   int           `json:"port_start"`
	PortEnd     int           `json:"port_end"`
	Priority    int           `json:"priority"`
}

// createEgressRule — POST /api/v1/workspaces/{wsid}/egress-rules.
// Nonce-gated upstream by requireConfirmNonce. Validates the rule
// shape then upserts via the store.
func (r *Router) createEgressRule(w http.ResponseWriter, req *http.Request) {
	if r.egressStore == nil {
		r.egressUnavailable(w)
		return
	}
	wsid := chi.URLParam(req, "wsid")

	var body egressRuleCreateRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation",
			"message": "invalid JSON body: " + err.Error(),
		})
		return
	}
	if msg := validateRulePayload(body); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation",
			"message": msg,
		})
		return
	}

	rule := egress.Rule{
		ID:          body.ID,
		WorkspaceID: wsid,
		Action:      body.Action,
		Proto:       body.Proto,
		HostPattern: body.HostPattern,
		PortStart:   body.PortStart,
		PortEnd:     body.PortEnd,
		Priority:    body.Priority,
		CreatedAt:   time.Now().UTC(),
	}
	if err := r.egressStore.Put(req.Context(), rule); err != nil {
		if errors.Is(err, egress.ErrDefaultImmutable) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "validation",
				"message": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	r.emitEgressAudit(audit.EventEgressRuleCreate, wsid, rule)
	writeJSON(w, http.StatusCreated, rule)
}

// deleteEgressRule — DELETE /api/v1/workspaces/{wsid}/egress-rules/{id}.
// Nonce-gated. Refuses defaults; returns 404 when the id is unknown.
func (r *Router) deleteEgressRule(w http.ResponseWriter, req *http.Request) {
	if r.egressStore == nil {
		r.egressUnavailable(w)
		return
	}
	wsid := chi.URLParam(req, "wsid")
	id := chi.URLParam(req, "id")
	if egress.IsDefaultID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation",
			"message": egress.ErrDefaultImmutable.Error(),
		})
		return
	}
	if err := r.egressStore.Delete(req.Context(), wsid, id); err != nil {
		if errors.Is(err, egress.ErrRuleNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":   "not_found",
				"message": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	r.emitEgressAudit(audit.EventEgressRuleDelete, wsid, egress.Rule{ID: id, WorkspaceID: wsid})
	w.WriteHeader(http.StatusNoContent)
}

// validateRulePayload returns a human-readable error message when the
// body is malformed, or "" when it's accepted.
func validateRulePayload(b egressRuleCreateRequest) string {
	if b.ID == "" {
		return "id is required"
	}
	if egress.IsDefaultID(b.ID) {
		return egress.ErrDefaultImmutable.Error()
	}
	switch b.Action {
	case egress.ActionAllow, egress.ActionDeny:
	default:
		return "action must be 'allow' or 'deny'"
	}
	switch b.Proto {
	case egress.ProtoTCP, egress.ProtoUDP, egress.ProtoAny:
	case "":
		return "proto is required"
	default:
		return "proto must be one of tcp|udp|any"
	}
	if b.HostPattern == "" {
		return "host_pattern is required"
	}
	if b.PortStart < 0 || b.PortEnd < 0 || b.PortStart > 65535 || b.PortEnd > 65535 {
		return "port range out of bounds (0..65535)"
	}
	if b.PortEnd != 0 && b.PortStart > b.PortEnd {
		return "port_start must be <= port_end"
	}
	return ""
}
