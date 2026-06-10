// HTTP surface for the workspace secrets vault (gm-o9t8.3.7).
//
// Wire shape:
//
//   - GET    /api/v1/workspaces/{wsid}/secrets        → list keys (no values)
//   - POST   /api/v1/workspaces/{wsid}/secrets/{key}  → store value (nonce-gated)
//   - DELETE /api/v1/workspaces/{wsid}/secrets/{key}  → remove value (nonce-gated)
//
// There is intentionally NO endpoint that returns a plaintext value:
// the only path that ever decrypts a secret is the in-process injector
// that splats env into a spawned session. Operators who need to inspect
// a value rotate it; they don't read it.

package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/internal/vault"
)

// AttachVault binds the vault implementation to the router. Until
// called, all three secrets endpoints return 503 adaptor_not_configured.
// cmd/gemba serve calls this once at boot. Tests inject a fixture
// vault; a nil argument is the documented way to detach (e.g. between
// table-driven cases).
func (r *Router) AttachVault(v vault.Vault) { r.vault = v }

// listWorkspaceSecrets — GET /api/v1/workspaces/{wsid}/secrets.
func (r *Router) listWorkspaceSecrets(w http.ResponseWriter, req *http.Request) {
	if r.vault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_not_configured",
			"message": "vault not attached",
		})
		return
	}
	wsid := chi.URLParam(req, "wsid")
	keys, err := r.vault.List(req.Context(), wsid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// putWorkspaceSecret — POST /api/v1/workspaces/{wsid}/secrets/{key}.
// Nonce-gated upstream of this handler by requireConfirmNonce.
func (r *Router) putWorkspaceSecret(w http.ResponseWriter, req *http.Request) {
	if r.vault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_not_configured",
			"message": "vault not attached",
		})
		return
	}
	wsid := chi.URLParam(req, "wsid")
	key := chi.URLParam(req, "key")

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation",
			"message": "invalid JSON body: " + err.Error(),
		})
		return
	}
	if body.Value == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation",
			"message": "value is required",
		})
		return
	}
	if err := r.vault.Put(req.Context(), wsid, key, []byte(body.Value)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteWorkspaceSecret — DELETE /api/v1/workspaces/{wsid}/secrets/{key}.
// Idempotent: deleting an absent key still returns 204.
func (r *Router) deleteWorkspaceSecret(w http.ResponseWriter, req *http.Request) {
	if r.vault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_not_configured",
			"message": "vault not attached",
		})
		return
	}
	wsid := chi.URLParam(req, "wsid")
	key := chi.URLParam(req, "key")
	if err := r.vault.Delete(req.Context(), wsid, key); err != nil {
		// Errors here are either validation (empty wsid/key) or
		// storage faults. ErrNotFound is not raised by Delete by
		// design, but guard against it for completeness.
		if errors.Is(err, vault.ErrNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_request",
			"message": err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
