// /api/sprints handler (gm-root.11). Wraps the bound WorkPlane's
// ListSprints and returns `{sprints: Sprint[], total: N}`. Drives the
// SPA's SprintPicker.
//
// Adaptors with manifest.sprint_native=false (today: bd, dolt) MAY
// return an empty list rather than an error — and most do. When the
// adaptor's capability guard denies the call instead, this handler
// translates that into the same 200-empty contract (gm-j51y) so the
// SPA's SprintPicker hides cleanly without surfacing 500s on every
// /board / /grid / /backlog page-load. The freeform SprintEditor
// remains the operator's path on those workspaces.

package server

import (
	"errors"
	"net/http"

	"github.com/MikeBengtson/gemba/core"
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
		// gm-j51y: a capability guard that denies on sprint_native=false
		// must surface to the client as an empty list, not a 500. The
		// SPA already handles the empty case (hides the picker); a
		// 500 here floods the console on every workspace that doesn't
		// own a sprint primitive (bd, dolt).
		var ae *core.AdaptorError
		if errors.As(err, &ae) && ae.Kind == core.KindCapabilityDenied {
			writeJSON(w, http.StatusOK, map[string]any{
				"sprints": []core.Sprint{},
				"total":   0,
			})
			return
		}
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
