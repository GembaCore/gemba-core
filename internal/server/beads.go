package server

import (
	"errors"
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/go-chi/chi/v5"
)

// getBead is the GET /api/beads/{id} handler. It returns a single
// WorkItem with its full Relationship graph (blocks / parent_child /
// relates_to) plus any extension edges the adaptor surfaces on
// Custom["beads:dependencies"]. Drives the SPA drill-in drawer (M1.7c).
//
// Error envelopes go through the shared httperr package so every data
// handler emits the same wire shape (gm-root.1.1):
//
//	400 {"error": "bad_request", "message": "missing bead id"}
//	404 {"error": "session_not_found", "message": "bead gm-foo not found"}
//	503 {"error": "adaptor_not_configured" | "adaptor_degraded", ...}
//	500 {"error": "internal", "message": "..."}
//
// The handler itself does not walk the relationship graph — the bound
// WorkPlane adaptor is responsible for populating Relationships,
// Evidence, and Custom fields before returning. Core guarantees (gm-e3.2)
// require adaptors with native edges beyond the three core kinds to
// surface them under a well-known Custom key (for beads: "beads:dependencies").
func (r *Router) getBead(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing bead id")
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

	item, err := wp.GetWorkItem(req.Context(), core.WorkItemID(id))
	if err != nil {
		// Tag the bare sentinel with the bead id so the 404 envelope
		// carries a self-describing message. Adaptors that already
		// return a *core.AdaptorError flow through to httperr.WriteError
		// unchanged.
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "bead %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, item)
}
