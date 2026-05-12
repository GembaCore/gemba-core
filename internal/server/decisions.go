// Govern verbs — decision family stub handlers (gm-o9t8.14).
//
// Stubs for the /api/v1/workspaces/{wsid}/decisions surface. Real
// implementation tracks under gm-o9t8.6/.8/.9/.10/.12 and the ASDD
// epic gm-v0sp.

package server

import "net/http"

// decisionsNotImplemented is the shared 501 envelope for decision
// stubs. Same shape as specsNotImplemented so the typed client can
// treat all 501s uniformly.
func decisionsNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error": map[string]string{
			"code":    "not_implemented",
			"message": "endpoint stub; tracked under gm-o9t8.14",
		},
	})
}

// listDecisionsStub: GET /api/v1/workspaces/{wsid}/decisions
func (r *Router) listDecisionsStub(w http.ResponseWriter, req *http.Request) {
	decisionsNotImplemented(w, req)
}

// createDecisionStub: POST /api/v1/workspaces/{wsid}/decisions
func (r *Router) createDecisionStub(w http.ResponseWriter, req *http.Request) {
	decisionsNotImplemented(w, req)
}

// getDecisionStub: GET /api/v1/workspaces/{wsid}/decisions/{id}
func (r *Router) getDecisionStub(w http.ResponseWriter, req *http.Request) {
	decisionsNotImplemented(w, req)
}

// lockDecisionStub: PATCH /api/v1/workspaces/{wsid}/decisions/{id}/lock
func (r *Router) lockDecisionStub(w http.ResponseWriter, req *http.Request) {
	decisionsNotImplemented(w, req)
}

// decisionRefsStub: GET /api/v1/workspaces/{wsid}/decisions/refs
// Backs `constitution lint --strict` — emits the cross-reference
// graph the lint walks to detect orphan or cyclic references.
func (r *Router) decisionRefsStub(w http.ResponseWriter, req *http.Request) {
	decisionsNotImplemented(w, req)
}
