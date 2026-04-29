// Mutation handlers for /api/work-items (gm-root.8 slice 1, renamed
// in gm-root.9).
//
// Today: POST /api/work-items + PATCH /api/work-items/{id}. DELETE
// is intentionally absent — closing is a PATCH to
// state_category=completed in our model.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/transport/api"
)

// createWorkItem is POST /api/work-items. Body MUST be the boundary
// shape accepted by transport.DecodeCreateWorkItem: `{"item": {...}}`
// with title, kind, status, state_category all non-empty and id +
// timestamps unset. Server-side fields land on the response.
//
// Response: 201 Created + the materialized core.WorkItem. SPA drops
// it straight into the react-query cache.
//
// Adaptor errors flow through the shared httperr mapper with the same
// kind→status mapping patchWorkItem uses.
func (r *Router) createWorkItem(w http.ResponseWriter, req *http.Request) {
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

	wi, err := api.DecodeCreateWorkItem(req)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	// gm-lw6h: auto-prefix milestone titles with `M<n>` (per-project
	// monotonic numbering) unless the operator already supplied one.
	if wi.Kind == core.KindMilestone {
		if !milestonePrefixRe.MatchString(strings.TrimSpace(wi.Title)) {
			n := nextMilestoneNumber(existingMilestoneTitles(req.Context(), wp))
			wi.Title = applyMilestonePrefix(wi.Title, n)
		}
	}

	out, err := wp.CreateWorkItem(req.Context(), wi)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// patchWorkItem is PATCH /api/work-items/{id}. Body MUST be a
// core.WorkItemPatch JSON object; an empty object is a no-op (the
// underlying adaptor still returns the current item, useful for
// round-tripping a state probe).
//
// Response shape: 200 + the materialized core.WorkItem (so the SPA
// can drop the result straight into its react-query cache).
//
// Adaptor errors flow through the shared httperr mapper:
//
//	core.KindReadOnly        → 405 method_not_allowed   (dolt-url adaptor)
//	core.KindValidation      → 400 validation
//	core.KindAdaptorDegraded → 503 adaptor_degraded
//	core.KindSessionNotFound → 404 session_not_found
//	(untagged)               → 500 internal             (Conformance Group F violation)
func (r *Router) patchWorkItem(w http.ResponseWriter, req *http.Request) {
	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing work item id")
		return
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"malformed work item id: "+err.Error())
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
			err = core.NewAdaptorError(core.KindSessionNotFound, "work item %s not found", id)
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
