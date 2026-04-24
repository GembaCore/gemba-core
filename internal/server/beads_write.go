// Mutation handlers for /api/beads (gm-root.8 slice 1).
//
// Today: PATCH /api/beads/{id}. Future: POST /api/beads (create) and
// possibly DELETE /api/beads/{id} (close), though closing is just a
// PATCH to state_category=completed in our model.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/go-chi/chi/v5"
)

// patchBead is PATCH /api/beads/{id}. Body MUST be a core.WorkItemPatch
// JSON object; an empty object is a no-op (the underlying adaptor still
// returns the current item, useful for round-tripping a state probe).
//
// Response shape: 200 + the materialized core.WorkItem (so the SPA can
// drop the result straight into its react-query cache).
//
// Adaptor errors flow through the shared httperr mapper:
//
//	core.KindReadOnly        → 405 method_not_allowed   (dolt-url adaptor)
//	core.KindValidation      → 400 validation
//	core.KindAdaptorDegraded → 503 adaptor_degraded
//	core.KindSessionNotFound → 404 session_not_found
//	(untagged)               → 500 internal             (Conformance Group F violation)
func (r *Router) patchBead(w http.ResponseWriter, req *http.Request) {
	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing bead id")
		return
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"malformed bead id: "+err.Error())
		return
	}

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

	var patch core.WorkItemPatch
	if err := decodePatchBody(req, &patch); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	out, err := wp.UpdateWorkItem(req.Context(), core.WorkItemID(id), patch)
	if err != nil {
		// Tag bare ErrNotFound the same way the GET handler does so the
		// envelope is self-describing.
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "bead %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// decodePatchBody reads the JSON body into patch. An empty body is
// treated as "no fields to update" rather than an error — pairs nicely
// with the nonce idempotency contract (a retry with no body is a probe
// that returns the current item).
func decodePatchBody(req *http.Request, patch *core.WorkItemPatch) error {
	if req.Body == nil || req.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(patch); err != nil {
		return err
	}
	return nil
}
