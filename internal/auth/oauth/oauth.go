// Package oauth implements the GitHub OAuth device-flow integration
// for the gemba control plane (gm-o9t8.3.5).
//
// The server-side surface has three pieces:
//
//   - Config — operator-supplied GitHub app credentials (client id +
//     client secret). The CLI flags / env vars are bound in
//     internal/config/bind.go; the runtime config is passed to New
//     which validates it.
//
//   - OAuth — the live handle held by the server. It knows the
//     callback URL and exposes a GitHub-API client the login handler
//     uses to translate a GitHub access_token into the user identity
//     used by the tenant provisioner.
//
//   - CallbackPath — the canonical mount point. cmd/gemba serve
//     registers this constant against the OAuth handler; when OAuth
//     is disabled the same path returns 503 with a clear envelope so
//     CLIs hitting an unconfigured server fail loudly.
//
// The full device flow itself lives client-side (internal/cli/login.go);
// the server only owns the (a) callback / status route placeholder and
// (b) the token-exchange endpoint that converts the GitHub OAuth token
// into a gemba bearer + tenant id. Both endpoints land in gm-o9t8.3.5.2.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CallbackPath is the canonical mount for the GitHub OAuth callback
// surface. Held as a package constant so the GitHub app config and the
// router agree on a single value.
//
// The device flow does not require a working redirect URI (GitHub's
// device endpoint instructs the user to visit a separate verification
// URL), but we still register the path so:
//
//   - operators can configure the GitHub app with a stable Authorization
//     callback URL during setup, and
//   - future web-app OAuth (gm-o9t8.3.5.5+) drops in here unchanged.
const CallbackPath = "/api/v1/auth/oauth/github/callback"

// LoginPath is the gemba-server endpoint the CLI hits after the GitHub
// device flow returns a GitHub OAuth access token. The handler
// validates the token against the GitHub API, provisions a tenant if
// none exists for the GitHub id, mints a gemba bearer, and returns the
// pair to the caller. Lives under /api/v1/ for consistency with the
// rest of the versioned surface.
const LoginPath = "/api/v1/auth/oauth/github/login"

// Config carries the GitHub OAuth app credentials needed to operate the
// server-side half of the device flow. Operator passes these in via
// flag / env (see internal/config/bind.go).
//
// CallbackBaseURL is the externally-reachable base URL of this gemba
// server (e.g. https://gemba.example.com). It is concatenated with
// CallbackPath to form the redirect URI registered on the GitHub OAuth
// app side. Empty is permitted in tests; production deployments set it
// to the real public origin so the GitHub app config matches.
type Config struct {
	GitHubClientID     string
	GitHubClientSecret string
	CallbackBaseURL    string
}

// ErrUnconfigured is returned by New when the supplied Config is empty
// (neither client id nor client secret set). Server wiring uses this
// to gate the 503 "oauth not configured" branch.
var ErrUnconfigured = errors.New("oauth: not configured")

// ErrPartialConfig is returned by New when exactly one of client id /
// client secret is set. Half-configured OAuth is always operator error
// — refuse to boot so they fix it before the first user tries to login.
var ErrPartialConfig = errors.New("oauth: client id and client secret must both be set")

// OAuth is the live server-side handle. Construct via New; pass nil to
// the router when OAuth is unconfigured and the router will mount the
// 503-returning handler instead.
type OAuth struct {
	cfg    Config
	client *http.Client
}

// New validates cfg and returns a live OAuth handle, or an error if
// the config is malformed.
//
// Validation rules:
//   - Both GitHubClientID and GitHubClientSecret empty → ErrUnconfigured.
//   - Exactly one of them empty → ErrPartialConfig.
//   - Both set → return *OAuth.
//
// CallbackBaseURL is not validated here — empty is legitimate for tests
// and the device flow does not actually redirect, so a missing public
// URL is non-fatal.
func New(cfg Config) (*OAuth, error) {
	id := strings.TrimSpace(cfg.GitHubClientID)
	secret := strings.TrimSpace(cfg.GitHubClientSecret)
	if id == "" && secret == "" {
		return nil, ErrUnconfigured
	}
	if id == "" || secret == "" {
		return nil, ErrPartialConfig
	}
	return &OAuth{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

// ClientID returns the configured GitHub OAuth client id. Safe to
// surface in operator-facing status output; the secret never is.
func (o *OAuth) ClientID() string {
	if o == nil {
		return ""
	}
	return o.cfg.GitHubClientID
}

// CallbackURL returns the fully-qualified callback URL the GitHub app
// should be configured with, or just CallbackPath if no base URL was
// supplied.
func (o *OAuth) CallbackURL() string {
	if o == nil {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(o.cfg.CallbackBaseURL), "/")
	if base == "" {
		return CallbackPath
	}
	return base + CallbackPath
}

// GitHubUser captures the subset of GET https://api.github.com/user
// fields the tenant provisioner depends on. The struct lives here (not
// in the server package) so handlers and tests can share the wire
// shape.
type GitHubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Email string `json:"email,omitempty"`
	Type  string `json:"type,omitempty"`
}

// UserFetcher abstracts the GitHub user-info call so tests can stub it
// against an httptest.Server without touching the production OAuth
// handle. Production wiring uses (*OAuth).FetchUser.
type UserFetcher interface {
	FetchUser(ctx context.Context, githubAccessToken string) (GitHubUser, error)
}

// FetchUser calls GET https://api.github.com/user with the supplied
// GitHub OAuth access token and decodes the response. Returns the
// canonical 401-equivalent error when the token is rejected so callers
// can return a clean "github_token_rejected" envelope.
func (o *OAuth) FetchUser(ctx context.Context, githubAccessToken string) (GitHubUser, error) {
	return fetchGitHubUser(ctx, o.client, "https://api.github.com/user", githubAccessToken)
}

// FetchUserAt is the test seam: same shape as FetchUser but lets the
// caller point at an httptest.Server. cmd/gemba serve never uses this.
func (o *OAuth) FetchUserAt(ctx context.Context, baseURL, githubAccessToken string) (GitHubUser, error) {
	return fetchGitHubUser(ctx, o.client, strings.TrimRight(baseURL, "/")+"/user", githubAccessToken)
}

func fetchGitHubUser(ctx context.Context, client *http.Client, url, token string) (GitHubUser, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return GitHubUser{}, fmt.Errorf("oauth: build user request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return GitHubUser{}, fmt.Errorf("oauth: call github user: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GitHubUser{}, fmt.Errorf("oauth: github user %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var u GitHubUser
	if err := json.Unmarshal(body, &u); err != nil {
		return GitHubUser{}, fmt.Errorf("oauth: decode github user: %w", err)
	}
	if u.ID == 0 || u.Login == "" {
		return GitHubUser{}, errors.New("oauth: github user response missing id/login")
	}
	return u, nil
}

// DisabledHandler returns the http.HandlerFunc the router mounts at
// the OAuth callback / login paths when OAuth is unconfigured. Returns
// 503 with a stable error envelope so CLIs distinguish "server doesn't
// speak OAuth" from "your token was rejected".
func DisabledHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code": "oauth_not_configured",
				"message": "GitHub OAuth is not configured on this gemba server; " +
					"pass --oauth-github-client-id and --oauth-github-client-secret to enable.",
			},
		})
	}
}

// CallbackHandler is a placeholder for the human-facing GitHub OAuth
// callback (e.g. the web app flow). The device flow does not need a
// real callback — GitHub instructs the user to visit a separate
// verification URL — so this handler simply 200s with a short JSON
// body confirming OAuth is wired. The full web-app flow lands later.
func (o *OAuth) CallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"flow":      "device",
			"client_id": o.ClientID(),
		})
	}
}
