// /api/sprints handler (gm-root.11). Wraps the bound WorkPlane's
// ListSprints and returns `{sprints: Sprint[], total: N}`. Drives the
// SPA's SprintPicker.
//
// Adaptors with manifest.sprint_native=false (today: bd, dolt) MAY
// return an empty list rather than 503 — and most do. The SPA reads
// the empty list, hides the picker, and falls back to the existing
// freeform SprintEditor.

package server

import (
	"net/http"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

func (r *Router) listSprints(w http.ResponseWriter, req *http.Request) {
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
	sprints, err := wp.ListSprints(req.Context())
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if sprints == nil {
		sprints = []core.Sprint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sprints": sprints,
		"total":   len(sprints),
	})
}
