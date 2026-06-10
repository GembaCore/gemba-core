// work_items_notes.go: 501 stub for POST /api/work-items/{id}/notes
// (gm-o9t8.1.3.2).
//
// The CLI `gemba bead note <id> <text>` targets this endpoint. The
// wire surface is pinned in internal/server/openapi/openapi.json so
// typed clients can generate against it; the persistence + propagation
// semantics land in a follow-up bead.

package server

import "net/http"

func (r *Router) noteWorkItemStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "not_implemented",
		"message": "POST /api/work-items/{id}/notes is a stub; backend lands in a follow-up bead",
	})
}
