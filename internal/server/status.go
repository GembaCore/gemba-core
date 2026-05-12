// Convenience verbs — workspace status stub handler (gm-o9t8.14).
//
// /api/v1/workspaces/{wsid}/status backs `gemba` invoked with no
// args: a single-shot read returning the unified work/spec/decision
// posture for the workspace. Stub for now; real implementation
// under the Govern ASDD epic gm-v0sp.

package server

import "net/http"

// workspaceStatusStub: GET /api/v1/workspaces/{wsid}/status
func (r *Router) workspaceStatusStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error": map[string]string{
			"code":    "not_implemented",
			"message": "endpoint stub; tracked under gm-o9t8.14",
		},
	})
}
