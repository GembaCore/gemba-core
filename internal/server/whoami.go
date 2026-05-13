// gm-o9t8.1.1.2: GET /api/whoami — minimal identity probe used by the
// CLI's `gemba login` validation step and `gemba whoami` read.
//
// The auth middleware (auth.BearerOrCookieAuth) has already gated this
// handler, so a request that reaches the body is authenticated. The
// middleware does not currently populate a per-request subject; for v1
// the single-token model means every authenticated request is the
// workspace operator. Returning a static "operator" subject is the
// honest answer until a multi-identity story (gm-o9t8 line, M3 OAuth)
// lands.
//
// Response shape mirrors the typed CLI client (see internal/cli/auth.go):
//
//	{ "subject": "<name>", "server_version": "<version>" }
//
// 401s come from the surrounding middleware, never from this handler.

package server

import (
	"net/http"
)

// whoamiResponse is the wire shape of GET /api/whoami.
type whoamiResponse struct {
	Subject       string `json:"subject"`
	ServerVersion string `json:"server_version"`
}

// whoami is the GET /api/whoami handler.
func (r *Router) whoami(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, whoamiResponse{
		Subject:       resolveSubject(r),
		ServerVersion: resolveServerVersion(r),
	})
}

// resolveSubject returns the identity to report at /api/whoami. In v1
// the only authentication mechanism is a single primary bearer token,
// so every authenticated request belongs to the workspace "operator".
// Multi-identity support is deferred to M3 (full OAuth).
func resolveSubject(_ *Router) string {
	return "operator"
}

// resolveServerVersion returns a best-effort version string. The
// gemba binary stamps build info into cmd/gemba; the Router does not
// currently see it (cf. /api/version handler's placeholder). Returning
// the same placeholder here keeps the two endpoints consistent;
// follow-up work (gm-e2.6) wires real build info into the Router.
func resolveServerVersion(_ *Router) string {
	return "pre-alpha"
}
