package server

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/go-chi/chi/v5"
)

// listWorkItems is the GET /api/work-items handler. It calls the
// registered WorkPlane's ListWorkItems with a zero-valued filter (no
// narrowing — filtering and pagination are deferred to later
// milestones) and returns the envelope `{items: []WorkItem, total: N}`.
// The SPA's useWorkItems() hook drives off this shape.
//
// Error envelopes follow the shared httperr contract (gm-root.1.1):
//
//	503 {"error": "adaptor_not_configured", ...} — no host/WorkPlane wired
//	503 {"error": "adaptor_degraded", ...}       — tagged AdaptorError
//	500 {"error": "internal", ...}               — untagged adaptor error
//
// An empty-but-healthy adaptor surfaces as 200 with `{items: [], total: 0}`;
// the handler normalises nil slices so the wire shape is a JSON array, not null.
func (r *Router) listWorkItems(w http.ResponseWriter, req *http.Request) {
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

	items, err := wp.ListWorkItems(req.Context(), core.WorkItemFilter{})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if items == nil {
		items = []core.WorkItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// getWorkItem is the GET /api/work-items/{id} handler. Returns a single
// WorkItem with its full Relationship graph (blocks / parent_child /
// relates_to) plus any extension edges the adaptor surfaces on
// Custom["beads:dependencies"] (or whatever the active adaptor names
// its native-edge custom key). Drives the SPA drill-in drawer.
//
// Error envelopes go through the shared httperr package so every data
// handler emits the same wire shape (gm-root.1.1):
//
//	400 {"error": "bad_request", "message": "missing work item id"}
//	404 {"error": "session_not_found", "message": "work item gm-foo not found"}
//	503 {"error": "adaptor_not_configured" | "adaptor_degraded", ...}
//	500 {"error": "internal", "message": "..."}
//
// The handler itself does not walk the relationship graph — the bound
// WorkPlane adaptor is responsible for populating Relationships,
// Evidence, and Custom fields before returning.
func (r *Router) getWorkItem(w http.ResponseWriter, req *http.Request) {
	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing work item id")
		return
	}
	// chi returns the path param verbatim; adaptors that prefix ids with
	// a workspace segment (the dolt adaptor's "gemba/gemba/" prefix) mean
	// the SPA sends %2F-encoded slashes. Decode here so the adaptor
	// receives the canonical id and not the still-encoded wire form.
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

	item, err := wp.GetWorkItem(req.Context(), core.WorkItemID(id))
	if err != nil {
		// Tag the bare sentinel with the work item id so the 404 envelope
		// carries a self-describing message. Adaptors that already
		// return a *core.AdaptorError flow through to httperr.WriteError
		// unchanged.
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "work item %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, item)
}
