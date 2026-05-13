// gm-o9t8.3.1.1 — admin CRUD endpoints for tenants.
//
// Surface (mounted on the shared /api/* block, behind apiAuth):
//
//	POST   /api/v1/tenants            — create a tenant; returns generated id
//	PATCH  /api/v1/tenants/{tid}      — update mutable fields (github_login)
//	DELETE /api/v1/tenants/{tid}      — soft-delete; refuses with workspaces
//	                                    unless ?force=1
//	GET    /api/v1/tenants            — paginated list (?limit=&after=)
//
// All four routes are admin-only. The v1 stub treats any valid bearer
// as admin — the same stance the audit surface takes (gm-o9t8.3.4)
// while the OAuth-bound role story (gm-o9t8.3.5.3) is unwritten.
// Tightening the gate is a single-line change once that bead lands.
//
// Audit: each mutation appends to the configured Auditor with kind
// audit.EventTenant{Create,Update,Delete}. Tests inject MemAuditor +
// query through Auditor.Query.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/server/audit"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// tenantWorkspaceCountFn is the hook signature consumed by the DELETE
// handler to refuse deletes that would orphan workspaces. cmd/gemba
// serve wires the production implementation against the workspace
// catalogue once that lands; until then nil is the default and the
// hook is treated as "no workspaces, delete is safe".
type tenantWorkspaceCountFn func(ctx context.Context, tid tenant.ID) (int, error)

// AttachAuditor wires a in the admin-plane audit sink. Pass nil to
// disable audit emission (the handlers behave normally otherwise).
func (r *Router) AttachAuditor(a audit.Auditor) { r.auditor = a }

// AttachTenantWorkspaceCount wires the per-tenant workspace-count
// probe consulted by DELETE /api/v1/tenants/{tid}. Passing nil clears
// the hook (defaulting back to "assume zero").
func (r *Router) AttachTenantWorkspaceCount(fn tenantWorkspaceCountFn) {
	r.tenantWorkspaceCount = fn
}

// createTenantRequest is the wire shape POST /api/v1/tenants accepts.
// kind is restricted to user|org; "system" is reserved for the seeded
// DefaultTenant row and is rejected here to prevent operators from
// minting additional system-kind tenants by accident.
type createTenantRequest struct {
	GitHubLogin string `json:"github_login"`
	GitHubID    int64  `json:"github_id"`
	Kind        string `json:"kind"`
}

// updateTenantRequest is the wire shape PATCH /api/v1/tenants/{tid}
// accepts. Only github_login is mutable today. We use a pointer so
// the handler can distinguish "field omitted" (leave unchanged) from
// "field present and empty" (explicit clear).
type updateTenantRequest struct {
	GitHubLogin *string `json:"github_login,omitempty"`
	// Kind is accepted on the wire so we can return a precise 400
	// when callers mistakenly try to mutate it. Always rejected.
	Kind *string `json:"kind,omitempty"`
}

// listTenantsResponse is the GET /api/v1/tenants envelope. NextAfter
// is the cursor a caller should pass as ?after= for the next page;
// the empty string signals "no more pages."
type listTenantsResponse struct {
	Tenants   []tenantResponse `json:"tenants"`
	NextAfter string           `json:"next_after,omitempty"`
}

// createTenant handles POST /api/v1/tenants. Generates a fresh id via
// tenant.NewID and inserts the row through the configured store; emits
// audit.EventTenantCreate on success.
func (r *Router) createTenant(w http.ResponseWriter, req *http.Request) {
	if r.tenantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"code": "adaptor_not_configured", "message": "tenant store not attached"},
		})
		return
	}
	var body createTenantRequest
	if err := decodeTenantJSONBody(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "bad_request", "message": err.Error()},
		})
		return
	}
	kind, err := parseCreateKind(body.Kind)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "invalid_kind", "message": err.Error()},
		})
		return
	}
	t := tenant.Tenant{
		ID:   tenant.NewID(),
		Kind: kind,
	}
	if body.GitHubLogin != "" {
		v := body.GitHubLogin
		t.GitHubLogin = &v
	}
	if body.GitHubID != 0 {
		v := body.GitHubID
		t.GitHubID = &v
	}
	if err := r.tenantStore.Create(req.Context(), t); err != nil {
		status := http.StatusInternalServerError
		code := "store_error"
		if errors.Is(err, tenant.ErrTenantExists) {
			status, code = http.StatusConflict, "tenant_exists"
		}
		writeJSON(w, status, map[string]any{
			"error": map[string]string{"code": code, "message": err.Error()},
		})
		return
	}
	got, err := r.tenantStore.Get(req.Context(), t.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error", "message": err.Error()},
		})
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventTenantCreate, map[string]any{
		"tenant_id": string(got.ID),
		"kind":      string(got.Kind),
	})
	writeJSON(w, http.StatusCreated, marshalTenant(got))
}

// patchTenant handles PATCH /api/v1/tenants/{tid}. Mutable fields are
// listed in updateTenantRequest; kind and github_id rotation are
// explicitly rejected (immutable in v1).
func (r *Router) patchTenant(w http.ResponseWriter, req *http.Request) {
	if r.tenantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"code": "adaptor_not_configured", "message": "tenant store not attached"},
		})
		return
	}
	id, err := tenant.ParseID(chi.URLParam(req, "tid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "invalid_tenant_id", "message": err.Error()},
		})
		return
	}
	var body updateTenantRequest
	if err := decodeTenantJSONBody(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "bad_request", "message": err.Error()},
		})
		return
	}
	if body.Kind != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "immutable_field", "message": "kind is immutable"},
		})
		return
	}
	updated, err := r.tenantStore.Update(req.Context(), id, tenant.Update{GitHubLogin: body.GitHubLogin})
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]string{"code": "tenant_not_found"},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error", "message": err.Error()},
		})
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventTenantUpdate, map[string]any{
		"tenant_id":    string(updated.ID),
		"github_login": body.GitHubLogin,
	})
	writeJSON(w, http.StatusOK, marshalTenant(updated))
}

// deleteTenant handles DELETE /api/v1/tenants/{tid}. t-default is
// permanently undeletable; other tenants require ?force=1 when any
// workspace still references them.
func (r *Router) deleteTenant(w http.ResponseWriter, req *http.Request) {
	if r.tenantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"code": "adaptor_not_configured", "message": "tenant store not attached"},
		})
		return
	}
	id, err := tenant.ParseID(chi.URLParam(req, "tid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "invalid_tenant_id", "message": err.Error()},
		})
		return
	}
	if id == tenant.DefaultTenant {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "default_tenant_undeletable", "message": "t-default is reserved and cannot be deleted"},
		})
		return
	}
	force := req.URL.Query().Get("force") == "1"
	if !force && r.tenantWorkspaceCount != nil {
		n, err := r.tenantWorkspaceCount(req.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"code": "workspace_count_failed", "message": err.Error()},
			})
			return
		}
		if n > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": map[string]string{
					"code":    "workspaces_present",
					"message": "tenant has workspaces; pass ?force=1 to delete anyway",
				},
				"workspace_count": n,
			})
			return
		}
	}
	if err := r.tenantStore.Delete(req.Context(), id); err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]string{"code": "tenant_not_found"},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error", "message": err.Error()},
		})
		return
	}
	r.appendTenantAudit(req.Context(), audit.EventTenantDelete, map[string]any{
		"tenant_id": string(id),
		"force":     force,
	})
	w.WriteHeader(http.StatusNoContent)
}

// listTenants handles GET /api/v1/tenants. Admin-only (any valid
// bearer in v1). Pagination via ?limit & ?after.
func (r *Router) listTenants(w http.ResponseWriter, req *http.Request) {
	if r.tenantStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"code": "adaptor_not_configured", "message": "tenant store not attached"},
		})
		return
	}
	q := req.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"code": "bad_request", "message": "limit must be a non-negative integer"},
			})
			return
		}
		limit = n
	}
	var after tenant.ID
	if v := q.Get("after"); v != "" {
		id, err := tenant.ParseID(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"code": "bad_request", "message": "after must be a valid tenant id"},
			})
			return
		}
		after = id
	}
	tenants, err := r.tenantStore.List(req.Context(), tenant.ListOptions{Limit: limit, After: after})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error", "message": err.Error()},
		})
		return
	}
	resp := listTenantsResponse{
		Tenants: make([]tenantResponse, 0, len(tenants)),
	}
	for _, t := range tenants {
		resp.Tenants = append(resp.Tenants, marshalTenant(t))
	}
	// next_after is non-empty when the page returned the maximum it
	// would for the configured limit — the caller passes the last id
	// back as ?after. When fewer rows came back we know we hit the
	// end and leave the cursor empty.
	if limit > 0 && len(tenants) == limit {
		resp.NextAfter = string(tenants[len(tenants)-1].ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

// marshalTenant converts a tenant.Tenant row into the wire
// tenantResponse shared with the GET /tenants/{tid} handler.
func marshalTenant(t tenant.Tenant) tenantResponse {
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
	return resp
}

// parseCreateKind validates the body's `kind` against the v1
// allow-list: user|org. system is reserved for the seeded
// DefaultTenant row.
func parseCreateKind(s string) (tenant.Kind, error) {
	switch tenant.Kind(s) {
	case tenant.KindUser, tenant.KindOrg:
		return tenant.Kind(s), nil
	case "":
		return "", errors.New("kind required (user|org)")
	case tenant.KindSystem:
		return "", errors.New("kind=system is reserved")
	default:
		return "", errors.New("kind must be one of: user, org")
	}
}

// decodeTenantJSONBody decodes req.Body into out using strict
// (unknown fields rejected) JSON. Empty bodies are decoded into the
// zero value without error so handlers can supply sensible defaults.
// Named with a tenant- prefix to avoid collision with the walks
// helper that owns the bare decodeJSONBody name.
func decodeTenantJSONBody(req *http.Request, out any) error {
	if req.Body == nil || req.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

// appendTenantAudit emits an audit record for a tenant CRUD action.
// Best-effort: a nil auditor or an underlying append error is
// logged-and-swallowed so the response shape stays stable.
func (r *Router) appendTenantAudit(ctx context.Context, kind audit.Kind, payload map[string]any) {
	if r.auditor == nil {
		return
	}
	_, _ = r.auditor.Append(ctx, "", "", kind, payload)
}
