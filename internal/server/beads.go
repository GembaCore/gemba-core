package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/go-chi/chi/v5"
)

// getBead is the GET /api/beads/{id} handler. It returns a single
// WorkItem with its full Relationship graph (blocks / parent_child /
// relates_to) plus any extension edges the adaptor surfaces on
// Custom["beads:dependencies"]. Drives the SPA drill-in drawer (M1.7c).
//
// Response shapes:
//
//	200 { ... WorkItem ... }
//	404 {"error": "not_found", "message": "bead gm-foo not found"}
//	503 {"error": "adaptor_not_configured" | "adaptor_error", ...}
//
// The handler itself does not walk the relationship graph — the bound
// WorkPlane adaptor is responsible for populating Relationships,
// Evidence, and Custom fields before returning. Core guarantees (gm-e3.2)
// require adaptors with native edges beyond the three core kinds to
// surface them under a well-known Custom key (for beads: "beads:dependencies").
func (r *Router) getBead(w http.ResponseWriter, req *http.Request) {
	id := chi.URLParam(req, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "bad_request",
			"message": "missing bead id",
		})
		return
	}

	if r.host == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_not_configured",
			"plane":   "work",
			"message": "no WorkPlane adaptor registered",
		})
		return
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_not_configured",
			"plane":   "work",
			"message": "no WorkPlane adaptor registered",
		})
		return
	}

	item, err := wp.GetWorkItem(req.Context(), core.WorkItemID(id))
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":   "not_found",
				"message": fmt.Sprintf("bead %s not found", id),
			})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_error",
			"plane":   "work",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, item)
}
