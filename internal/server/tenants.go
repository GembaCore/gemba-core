// gm-o9t8.3.9 — tenant metadata surface.
//
// This file pins the read-only edge of the tenancy contract:
//
//   - GET /api/v1/tenants/{tid} — returns tenant metadata for the
//     bearer-bound tenant. The handler is auth-gated by the shared
//     /api middleware AND additionally checks that the requested tid
//     matches the bearer-bound tenant (so a t-abcd bearer cannot read
//     t-xyz). The "admin-only" qualifier in the brief reduces to
//     "owner of the tenant in single-user mode"; full RBAC lands with
//     the OAuth story (gm-o9t8.3.5).
//
// CRUD beyond GET is explicitly out of scope (see gm-o9t8.3.1
// follow-ups).

package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/tenant"
)

// AttachTenantStore wires a tenant.Store into the Router. cmd/gemba
// serve constructs the store (SQLStore against the Dolt pool, or
// MemStore for the no-dolt fallback) and attaches it after the router
// is built. A nil store turns the /api/v1/tenants/{tid} handler into
// a 503 — same convention as the metrics / workflow attach points.
func (r *Router) AttachTenantStore(s tenant.Store) {
	r.tenantStore = s
}

// tenantResponse is the wire shape for GET /api/v1/tenants/{tid}.
// Optional fields use omitempty so the DefaultTenant row (which has
// no GitHub identity) renders without nulls.
type tenantResponse struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	GitHubLogin string `json:"github_login,omitempty"`
	GitHubID    int64  `json:"github_id,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// getTenant handles GET /api/v1/tenants/{tid}. Returns 403 when the
// requested tid does not match the bearer-bound tenant, 404 when the
// tid is well-formed but absent from the store, 503 when no store is
// attached (boot order / unconfigured).
func (r *Router) getTenant(w http.ResponseWriter, req *http.Request) {
	if r.tenantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code":    "adaptor_not_configured",
				"message": "tenant store not attached",
			},
		})
		return
	}
	requested, err := tenant.ParseID(chi.URLParam(req, "tid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"code":    "invalid_tenant_id",
				"message": err.Error(),
			},
		})
		return
	}
	bearerTID, ok := tenant.FromContext(req.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized",
			"code":  "no_tenant",
		})
		return
	}
	if bearerTID != requested {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "forbidden",
			"code":  "cross_tenant_access",
		})
		return
	}
	t, err := r.tenantStore.Get(req.Context(), requested)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]string{
					"code": "tenant_not_found",
				},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error"},
		})
		return
	}
	resp := tenantResponse{
		ID:        string(t.ID),
		Kind:      string(t.Kind),
		CreatedAt: t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if t.GitHubLogin != nil {
		resp.GitHubLogin = *t.GitHubLogin
	}
	if t.GitHubID != nil {
		resp.GitHubID = *t.GitHubID
	}
	writeJSON(w, http.StatusOK, resp)
}
