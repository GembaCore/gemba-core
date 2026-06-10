// /api/workflows/* handler module (gm-e12.22.2). Thin proxy over the
// internal/workflow package, which itself shells to bd formula + bd mol.
//
// Routes:
//
//	GET  /api/workflows/formulas         — Library list
//	GET  /api/workflows/formulas/{name}  — Library detail (cooked DAG)
//	GET  /api/workflows/runs?status=...  — Active / done / all runs
//	GET  /api/workflows/runs/{id}        — Run detail with per-step state
//	POST /api/workflows/runs             — pour or wisp; nonce-gated
//	POST /api/workflows/runs/{id}/burn   — discard a wisp; nonce-gated
//
// Like the rest of /api/*, error responses ride the httperr envelope.
// 404 = unknown formula or run; 409 = burn requested on a persistent
// mol; 400 = malformed body or bad status filter; 503 = the workflow
// client wasn't wired into this Router.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/workflow"
)

// listWorkflowFormulas handles GET /api/workflows/formulas.
func (r *Router) listWorkflowFormulas(w http.ResponseWriter, req *http.Request) {
	if r.workflowClient == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "workflow client not registered")
		return
	}
	formulas, err := r.workflowClient.List(req.Context())
	if err != nil {
		httperr.Write(w, http.StatusServiceUnavailable, "adaptor_degraded", err.Error())
		return
	}
	if formulas == nil {
		formulas = []workflow.Formula{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"formulas": formulas,
		"total":    len(formulas),
	})
}

// getWorkflowFormula handles GET /api/workflows/formulas/{name}.
func (r *Router) getWorkflowFormula(w http.ResponseWriter, req *http.Request) {
	if r.workflowClient == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "workflow client not registered")
		return
	}
	name := chi.URLParam(req, "name")
	if strings.TrimSpace(name) == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing formula name")
		return
	}
	f, err := r.workflowClient.Show(req.Context(), name)
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// listWorkflowRuns handles GET /api/workflows/runs?status=active|done|all.
// Default status is "active". Unknown values 400.
func (r *Router) listWorkflowRuns(w http.ResponseWriter, req *http.Request) {
	if r.workflowClient == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "workflow client not registered")
		return
	}
	status := req.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	switch status {
	case "active", "done", "all":
	default:
		httperr.Write(w, http.StatusBadRequest, "validation",
			"status must be one of {active, done, all}")
		return
	}
	runs, err := r.workflowClient.ListRuns(req.Context(), status)
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	if runs == nil {
		runs = []workflow.Run{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"runs":  runs,
		"total": len(runs),
	})
}

// getWorkflowRun handles GET /api/workflows/runs/{id}.
func (r *Router) getWorkflowRun(w http.ResponseWriter, req *http.Request) {
	if r.workflowClient == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "workflow client not registered")
		return
	}
	id := chi.URLParam(req, "id")
	if strings.TrimSpace(id) == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing run id")
		return
	}
	run, err := r.workflowClient.GetRun(req.Context(), id)
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// startWorkflowRun handles POST /api/workflows/runs. Body shape:
//
//	{ formula, vars: {key: value}, attach_to_bead?, ephemeral: bool }
//
// ephemeral=true → bd mol wisp; false → bd mol pour. Nonce-gated at
// the router so a SPA double-click can't fork two runs from one click.
func (r *Router) startWorkflowRun(w http.ResponseWriter, req *http.Request) {
	if r.workflowClient == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "workflow client not registered")
		return
	}
	var body workflow.PourRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation",
			"invalid JSON body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Formula) == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "formula is required")
		return
	}
	var (
		run workflow.Run
		err error
	)
	if body.Ephemeral {
		run, err = r.workflowClient.Wisp(req.Context(), body.Formula, body.Vars, body.AttachToBead)
	} else {
		run, err = r.workflowClient.Pour(req.Context(), body.Formula, body.Vars, body.AttachToBead)
	}
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

// burnWorkflowRun handles POST /api/workflows/runs/{id}/burn. Refuses
// (409) when the target is a persistent mol — burn discards wisps,
// not mols.
func (r *Router) burnWorkflowRun(w http.ResponseWriter, req *http.Request) {
	if r.workflowClient == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "workflow client not registered")
		return
	}
	id := chi.URLParam(req, "id")
	if strings.TrimSpace(id) == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing run id")
		return
	}
	if err := r.workflowClient.Burn(req.Context(), id); err != nil {
		writeWorkflowErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "burned": true})
}

// writeWorkflowErr maps the internal/workflow sentinels onto httperr
// envelopes. Other (untagged) errors land at 503 adaptor_degraded —
// the nearest analogue for "bd subprocess failed", which the SPA's
// degraded-state banner already handles.
func writeWorkflowErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflow.ErrNotFound):
		httperr.Write(w, http.StatusNotFound, "session_not_found", err.Error())
	case errors.Is(err, workflow.ErrNotWisp):
		httperr.Write(w, http.StatusConflict, "burn_not_wisp", err.Error())
	default:
		httperr.Write(w, http.StatusServiceUnavailable, "adaptor_degraded", err.Error())
	}
}
