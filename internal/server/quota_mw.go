// gm-o9t8.4.1 — per-tenant token-bucket quota middleware.
//
// Mirrors the tenant-resolution stance of tenants_admin.go (reads the
// bearer-bound tenant from the request context via tenant.FromContext).
// On allow the request flows through to next; on deny the middleware
// short-circuits with a 429 envelope and a Retry-After header.
//
// Wiring: cmd/gemba serve attaches the limiter via Router.AttachQuotaLimiter
// then wraps the dispatch and bead-create routes via Router.QuotaMiddleware.
// The middleware is a no-op when no limiter is attached so existing tests
// don't need to plumb one in.

package server

import (
	"net/http"
	"strconv"

	"github.com/GembaCore/gemba-core/internal/quota"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// AttachQuotaLimiter binds the per-tenant token-bucket limiter consumed
// by QuotaMiddleware. Passing nil disables quota enforcement.
func (r *Router) AttachQuotaLimiter(l *quota.Limiter) { r.quotaLimiter = l }

// QuotaMiddleware returns an http.Handler middleware that consumes one
// token from the bearer-bound tenant's bucket on every request. When
// no limiter is attached the middleware is a passthrough.
//
// On deny: emits 429 with Retry-After (delta-seconds, RFC 9110) and
// the standard httperr envelope code "rate_limited".
func (r *Router) QuotaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		l := r.quotaLimiter
		if l == nil {
			next.ServeHTTP(w, req)
			return
		}
		id, ok := tenant.FromContext(req.Context())
		if !ok {
			// No tenant context — admin/auth-less surface. Skip
			// rather than 401 so this middleware composes cleanly
			// with paths that opt out of WithTenant.
			next.ServeHTTP(w, req)
			return
		}
		allowed, retry := l.Allow(id)
		if allowed {
			next.ServeHTTP(w, req)
			return
		}
		secs := int(retry.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		httperr.Write(w, http.StatusTooManyRequests, "rate_limited",
			"per-tenant quota exhausted; retry after "+strconv.Itoa(secs)+"s")
	})
}
