// gm-o9t8.3.5.5 — OAuth token management surface.
//
// Three routes, all auth-gated (so the tenant is on the request
// context by the time we run) and scoped to the bearer's tenant_id
// so a user can never read or mutate another tenant's tokens:
//
//   - GET    /api/v1/auth/tokens                 → []TokenInfo
//   - DELETE /api/v1/auth/tokens/{token_id}      → 204 / 404 (nonce-gated)
//   - POST   /api/v1/auth/tokens/{token_id}/rotate → {new_bearer,…} (nonce-gated)
//
// The handlers do NOT log the plaintext or argon2id hash anywhere —
// rotate returns the new bearer ONCE in the response body, the same
// once-and-only-once contract that OAuth login uses.

package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/server/audit"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// rotateResponse is the wire shape for POST /tokens/{id}/rotate.
type rotateResponse struct {
	NewBearer   string `json:"new_bearer"`
	TokenID     string `json:"token_id"`
	DeviceLabel string `json:"device_label"`
}

// listAuthTokens handles GET /api/v1/auth/tokens. Returns the bearer's
// own non-revoked tokens. Filters by the tenant id stamped on the
// request context by the WithTenant middleware.
func (r *Router) listAuthTokens(w http.ResponseWriter, req *http.Request) {
	if r.oauthTokenStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code":    "adaptor_not_configured",
				"message": "oauth token store not attached",
			},
		})
		return
	}
	tid, ok := tenant.FromContext(req.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized", "code": "no_tenant",
		})
		return
	}
	infos, err := r.oauthTokenStore.List(req.Context(), string(tid))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error"},
		})
		return
	}
	if infos == nil {
		infos = []TokenInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": infos})
}

// revokeAuthToken handles DELETE /api/v1/auth/tokens/{token_id}.
func (r *Router) revokeAuthToken(w http.ResponseWriter, req *http.Request) {
	if r.oauthTokenStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code": "adaptor_not_configured",
			},
		})
		return
	}
	tid, ok := tenant.FromContext(req.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized", "code": "no_tenant",
		})
		return
	}
	tokenID := chi.URLParam(req, "token_id")
	if tokenID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "bad_request", "code": "missing_token_id",
		})
		return
	}
	if err := r.oauthTokenStore.Revoke(req.Context(), string(tid), tokenID); err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]string{"code": "token_not_found"},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error"},
		})
		return
	}
	if r.auditEmit != nil {
		_ = r.auditEmit(req.Context(), string(audit.EventAuthTokenRevoke), map[string]any{
			"tenant_id": string(tid),
			"token_id":  tokenID,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateAuthToken handles POST /api/v1/auth/tokens/{token_id}/rotate.
func (r *Router) rotateAuthToken(w http.ResponseWriter, req *http.Request) {
	if r.oauthTokenStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code": "adaptor_not_configured",
			},
		})
		return
	}
	tid, ok := tenant.FromContext(req.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized", "code": "no_tenant",
		})
		return
	}
	tokenID := chi.URLParam(req, "token_id")
	if tokenID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "bad_request", "code": "missing_token_id",
		})
		return
	}
	newBearer, newID, label, err := r.oauthTokenStore.Rotate(req.Context(), string(tid), tokenID)
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": map[string]string{"code": "token_not_found"},
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "store_error"},
		})
		return
	}
	if r.auditEmit != nil {
		_ = r.auditEmit(req.Context(), string(audit.EventAuthTokenRotate), map[string]any{
			"tenant_id":    string(tid),
			"prior_token":  tokenID,
			"new_token_id": newID,
			"device_label": label,
		})
	}
	writeJSON(w, http.StatusOK, rotateResponse{
		NewBearer:   newBearer,
		TokenID:     newID,
		DeviceLabel: label,
	})
}
