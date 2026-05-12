// Tenant middleware: extracts a bearer-bound tenant from the
// Authorization header and stores it on the request context for
// downstream handlers.
//
// gm-o9t8.3.9 ships the FOUNDATION: a single-user resolver that always
// answers DefaultTenant. The OAuth callback in gm-o9t8.3.5 will
// replace the resolver with one that maps bearer → tenant via the
// `tenants` table. The middleware contract is stable across both
// modes: handlers always read tenant.FromContext.

package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// TenantResolver maps a presented bearer (or empty string for a
// cookie-authenticated request) to a tenant id. The auth middleware
// runs first, so by the time WithTenant calls the resolver the
// credential is already known-valid; the resolver only translates it.
//
// Returns ok=false when the credential cannot be mapped to a tenant
// (e.g. a bearer that authenticates but has no `tenants` row yet).
// The middleware turns ok=false into a 401 — the caller is
// authenticated for the API surface but not yet provisioned in the
// tenancy table.
type TenantResolver func(bearer string) (tenant.ID, bool)

// SingleUserResolver always returns DefaultTenant. This is the
// gm-o9t8.3.9 default: single-user installs and the M1 upgrade path
// land here; the bearer credential is checked by the auth middleware
// upstream and every authenticated request resolves to t-default.
func SingleUserResolver() TenantResolver {
	return func(_ string) (tenant.ID, bool) { return tenant.DefaultTenant, true }
}

// WithTenant returns middleware that resolves the bearer-bound tenant
// (via resolve) and stores it on the request context. The auth
// middleware must run BEFORE this one — WithTenant assumes credentials
// have already been verified and only handles the bearer → tenant
// translation step.
//
// resolve == nil falls back to SingleUserResolver — convenient for
// tests and the M1 upgrade path before OAuth lands.
func WithTenant(resolve TenantResolver) func(http.Handler) http.Handler {
	if resolve == nil {
		resolve = SingleUserResolver()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := bearerFromRequest(r)
			tid, ok := resolve(bearer)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "unauthorized",
					"code":  "tenant_not_provisioned",
				})
				return
			}
			next.ServeHTTP(w, r.WithContext(tenant.WithContext(r.Context(), tid)))
		})
	}
}

// RequireWSIDTenantMatch is a route-scoped middleware that asserts
// the {wsid} URL parameter's tenant prefix matches the request's
// bearer-bound tenant. Returns 403 on mismatch, 400 when the wsid is
// malformed. The middleware is a no-op when no tenant is attached to
// the context — upstream auth + WithTenant should run first; if they
// did not, the handler will see no wsid match either and a 401 will
// be issued by the auth layer instead.
//
// Backwards compat (gm-o9t8.3.9 M1 carry-over): a bare wsid (no
// colon) parses as DefaultTenant. Single-user installs whose bearer
// also resolves to DefaultTenant therefore pass straight through —
// the historical M1 routes keep working byte-for-byte.
func RequireWSIDTenantMatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := chi.URLParam(r, "wsid")
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		wsid, err := tenant.ParseWSID(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "bad_request",
				"code":  "invalid_wsid",
			})
			return
		}
		bearerTID, ok := tenant.FromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
				"code":  "no_tenant",
			})
			return
		}
		if bearerTID.Prefix() != string(wsid.Tenant) {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "forbidden",
				"code":  "cross_tenant_access",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerFromRequest(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(h, prefix))
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
