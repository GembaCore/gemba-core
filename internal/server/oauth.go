// gm-o9t8.3.5 — server-side GitHub OAuth surface.
//
// This file wires two routes:
//
//   - GET  /api/v1/auth/oauth/github/callback — placeholder/status
//     endpoint. The device flow doesn't need a real redirect target,
//     so when OAuth is configured we 200 with a tiny diagnostic body
//     ({status:"ok", flow:"device"}); when OAuth is disabled we 503
//     with oauth_not_configured.
//
//   - POST /api/v1/auth/oauth/github/login — the device-flow
//     exchange. The CLI completes the device flow against
//     api.github.com on its own and POSTs the resulting GitHub
//     access_token here. We call GET https://api.github.com/user to
//     validate the token + recover the GitHub identity, find-or-
//     create a tenant in the tenant.Store, mint a 32-byte gemba
//     bearer, hash it with argon2id, persist (tenant_id, github_id,
//     hash) and return the plaintext bearer + tenant id to the
//     caller. The plaintext bearer is never persisted server-side.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/auth"
	"github.com/GembaCore/gemba-core/internal/auth/oauth"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// TokenStore is the persistence contract for OAuth-minted gemba
// bearer tokens. The plaintext is never stored — only the argon2id
// PHC hash. Records are scoped to (tenant_id, github_id) so a single
// GitHub identity can hold multiple tokens (one per device) without
// colliding.
type TokenStore interface {
	// Put records (tenantID, githubID, deviceLabel, argon2id-hash).
	// Implementations MUST upsert on (tenantID, deviceLabel) so a
	// re-login from the same device replaces the prior hash rather
	// than accumulating dead rows. If rec.ID is empty Put assigns
	// a fresh opaque token id.
	Put(ctx context.Context, rec TokenRecord) error
	// VerifyToken finds the record whose hashed value matches the
	// supplied plaintext token, returning (tenantID, true) on match.
	// Returns ("", false, nil) when no match — distinct from infra
	// errors which return ("", false, err). Implementations MUST
	// skip rows where revoked_at IS NOT NULL and SHOULD best-effort
	// touch last_used_at on a successful verify.
	VerifyToken(ctx context.Context, plaintext string) (tenant.ID, bool, error)
	// List returns metadata for non-revoked tokens belonging to
	// tenantID. Never returns plaintext or hashes.
	List(ctx context.Context, tenantID string) ([]TokenInfo, error)
	// Revoke soft-deletes (tenantID, tokenID). Returns
	// ErrTokenNotFound when the row is absent or already revoked.
	Revoke(ctx context.Context, tenantID, tokenID string) error
	// Rotate issues a fresh bearer + argon2id hash and revokes the
	// prior row in a single transaction. Returns (plaintext, newID,
	// deviceLabel). ErrTokenNotFound when (tenantID, tokenID) is
	// absent or already revoked.
	Rotate(ctx context.Context, tenantID, tokenID string) (string, string, string, error)
}

// TokenRecord is one persisted row in the auth_tokens table.
type TokenRecord struct {
	ID          string // opaque 32-char hex id; the auth_tokens primary key
	TenantID    tenant.ID
	GitHubID    int64
	DeviceLabel string
	HashPHC     string // argon2id PHC string; the plaintext is never stored
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// oauthLoginRequest is the wire shape the CLI POSTs after completing
// the device flow against GitHub.
type oauthLoginRequest struct {
	GitHubAccessToken string `json:"github_access_token"`
}

// oauthLoginResponse is the gemba bearer + tenant id the CLI persists
// to its credstore. The token is plaintext — the only time the server
// emits it, returned exactly once.
type oauthLoginResponse struct {
	Token       string `json:"token"`
	TenantID    string `json:"tenant_id"`
	Subject     string `json:"subject"`
	DeviceLabel string `json:"device_label"`
}

// oauthCallback serves GET /api/v1/auth/oauth/github/callback. When
// OAuth is unconfigured returns 503 oauth_not_configured so callers
// distinguish "server doesn't speak OAuth" from auth errors.
func (r *Router) oauthCallback(w http.ResponseWriter, req *http.Request) {
	if r.oauth == nil {
		oauth.DisabledHandler().ServeHTTP(w, req)
		return
	}
	r.oauth.CallbackHandler().ServeHTTP(w, req)
}

// oauthLogin serves POST /api/v1/auth/oauth/github/login. Implements
// the device-flow exchange: GitHub OAuth token → gemba bearer +
// tenant id. See file header for the full sequence.
func (r *Router) oauthLogin(w http.ResponseWriter, req *http.Request) {
	if r.oauth == nil || r.oauthTokenStore == nil || r.tenantStore == nil {
		oauth.DisabledHandler().ServeHTTP(w, req)
		return
	}
	if req.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method_not_allowed",
		})
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 64*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "read_body", "code": "bad_request",
		})
		return
	}
	var in oauthLoginRequest
	if err := json.Unmarshal(body, &in); err != nil || strings.TrimSpace(in.GitHubAccessToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request: github_access_token required",
			"code":  "bad_request",
		})
		return
	}

	ctx := req.Context()

	// Resolve the GitHub identity. Tests inject an alternate fetcher
	// via the package-level githubUserFetcher hook — see oauth_test.go.
	user, err := r.fetchGitHubUser(ctx, in.GitHubAccessToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"code":    "github_token_rejected",
				"message": err.Error(),
			},
		})
		return
	}

	// Find-or-create the tenant. Subsequent logins from the same
	// github_id resolve to the existing tenant row; first login
	// provisions one.
	tn, err := r.tenantStore.GetByGitHub(ctx, user.ID)
	if err != nil && !errors.Is(err, tenant.ErrTenantNotFound) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "tenant_lookup"},
		})
		return
	}
	if errors.Is(err, tenant.ErrTenantNotFound) {
		login := user.Login
		gid := user.ID
		tn = tenant.Tenant{
			ID:          tenant.NewID(),
			GitHubLogin: &login,
			GitHubID:    &gid,
			Kind:        tenant.KindUser,
		}
		if err := r.tenantStore.Create(ctx, tn); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"code": "tenant_create"},
			})
			return
		}
	}

	// Mint the gemba bearer. 32 random bytes → hex (64 chars).
	plaintext, err := mintToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "token_mint"},
		})
		return
	}
	hash, err := auth.HashToken(plaintext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "token_hash"},
		})
		return
	}

	// Device label is "github-<short-sha>" of the plaintext so a
	// re-login from the same device produces a stable label and the
	// store can upsert. Full scoping lands in gm-o9t8.3.5.5.
	deviceLabel := "github-" + plaintext[:8]

	rec := TokenRecord{
		TenantID:    tn.ID,
		GitHubID:    user.ID,
		DeviceLabel: deviceLabel,
		HashPHC:     hash,
		CreatedAt:   time.Now().UTC(),
	}
	if err := r.oauthTokenStore.Put(ctx, rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"code": "token_persist"},
		})
		return
	}

	// Audit hook: best-effort emit auth.login. The audit logger is
	// optional in this slice — when nil we skip silently. Real audit
	// wiring lands alongside the audit-attach milestone.
	if r.auditEmit != nil {
		_ = r.auditEmit(ctx, "auth.login", map[string]any{
			"tenant_id":    string(tn.ID),
			"github_id":    user.ID,
			"github_login": user.Login,
			"device_label": deviceLabel,
		})
	}

	writeJSON(w, http.StatusOK, oauthLoginResponse{
		Token:       plaintext,
		TenantID:    string(tn.ID),
		Subject:     user.Login,
		DeviceLabel: deviceLabel,
	})
}

// fetchGitHubUser returns the resolved GitHub identity for token.
// Tests override r.githubUserFetcherOverride to point at an
// httptest.Server; production callers go through the OAuth handle's
// FetchUser which talks to api.github.com directly.
func (r *Router) fetchGitHubUser(ctx context.Context, token string) (oauth.GitHubUser, error) {
	if r.githubUserFetcherOverride != nil {
		return r.githubUserFetcherOverride(ctx, token)
	}
	if r.oauth == nil {
		return oauth.GitHubUser{}, fmt.Errorf("oauth not configured")
	}
	return r.oauth.FetchUser(ctx, token)
}

// mintToken produces a 32-byte hex-encoded random bearer.
func mintToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
