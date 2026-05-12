// Govern verbs — spec family stub handlers (gm-o9t8.14).
//
// These endpoints back the canonical Govern surface declared in
// docs/superpowers/specs/2026-05-11-gemba-remote-design.md §4 and
// the OpenAPI document at internal/server/openapi/openapi.json.
//
// All handlers in this file are 501 stubs: the wire surface is
// pinned (path, method, auth, nonce gate) so the typed Go client
// (gm-o9t8.1.1.4) and the SPA can generate against it, but the
// real reconcile/snapshot/adopt logic lives under the ASDD epic
// gm-v0sp and lands in follow-up beads.

package server

import "net/http"

// specsNotImplemented is the shared 501 envelope every Govern/spec
// stub returns. The error code is stable so callers branching on
// "stub vs failure" don't have to parse the message.
func specsNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error": map[string]string{
			"code":    "not_implemented",
			"message": "endpoint stub; tracked under gm-o9t8.14",
		},
	})
}

// listSpecsStub: GET /api/v1/workspaces/{wsid}/specs
func (r *Router) listSpecsStub(w http.ResponseWriter, req *http.Request) {
	specsNotImplemented(w, req)
}

// reconcileSpecStub: POST /api/v1/workspaces/{wsid}/specs/{slug}/reconcile
func (r *Router) reconcileSpecStub(w http.ResponseWriter, req *http.Request) {
	specsNotImplemented(w, req)
}

// watchSpecStub: GET /api/v1/workspaces/{wsid}/specs/{slug}/watch (SSE)
//
// Real implementation will set Content-Type: text/event-stream and
// emit `bead.status`, `spec.drift`, and `reconcile.complete` events
// as the spec reconciles. Stub returns 501 with a JSON envelope.
func (r *Router) watchSpecStub(w http.ResponseWriter, req *http.Request) {
	specsNotImplemented(w, req)
}

// snapshotSpecStub: POST /api/v1/workspaces/{wsid}/specs/{slug}/snapshot
func (r *Router) snapshotSpecStub(w http.ResponseWriter, req *http.Request) {
	specsNotImplemented(w, req)
}

// adoptSpecStub: POST /api/v1/workspaces/{wsid}/specs/{slug}/adopt
func (r *Router) adoptSpecStub(w http.ResponseWriter, req *http.Request) {
	specsNotImplemented(w, req)
}

// patchSpecStub: PATCH /api/v1/workspaces/{wsid}/specs/{slug}
// Used to freeze/unfreeze (status=archived/active).
func (r *Router) patchSpecStub(w http.ResponseWriter, req *http.Request) {
	specsNotImplemented(w, req)
}
