// Govern verbs — labels (spec lint --strict) stub handler (gm-o9t8.14).
//
// /api/v1/workspaces/{wsid}/labels backs the strict spec lint that
// catalogs declared vs referenced labels. Stub for now; real
// implementation under gm-v0sp.

package server

import "net/http"

// listLabelsStub: GET /api/v1/workspaces/{wsid}/labels
func (r *Router) listLabelsStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error": map[string]string{
			"code":    "not_implemented",
			"message": "endpoint stub; tracked under gm-o9t8.14",
		},
	})
}
