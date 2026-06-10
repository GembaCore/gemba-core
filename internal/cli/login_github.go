// gm-o9t8.3.5.2 — `gemba login --github` device-flow implementation.
//
// The flow has three phases:
//
//  1. Device code: POST https://github.com/login/device/code with
//     client_id + scope=repo,user:email. GitHub returns a user code
//     (typed at the verification URL), a device code (held server-
//     side), an interval (poll cadence), and a verification URL.
//
//  2. Poll: every <interval> seconds POST
//     https://github.com/login/oauth/access_token with
//     client_id + device_code + grant_type. Three pending states:
//     authorization_pending (keep polling), slow_down (bump the
//     interval), expired_token (stop). Success → access_token in
//     the body.
//
//  3. Exchange: POST <gemba-server>/api/v1/auth/oauth/github/login
//     with the GitHub access token. The server validates it,
//     provisions a tenant on first login, and returns a gemba
//     bearer token. We persist that bearer in the credstore so
//     subsequent CLI calls reuse it.
//
// On Ctrl+C the polling loop exits cleanly (no token is persisted,
// the user-facing message says "cancelled").
//
// The exchange happens through an injectable HTTP client / GitHub
// base URL so tests can run the full sequence against httptest.Server
// without any network traffic.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
)

// githubDeviceFlowConfig carries the operator + endpoint config the
// device flow needs. Production uses the real GitHub URLs; tests
// override Endpoints to point at an httptest.Server.
type githubDeviceFlowConfig struct {
	ClientID     string
	Scopes       string // comma-separated; default "repo,user:email"
	GitHubBase   string // default "https://github.com" — device + token endpoints
	GembaServer  string // gemba server base URL for the exchange POST
	HTTPClient   *http.Client
	PollInterval time.Duration // optional override for tests; default = GitHub's interval
}

// deviceCodeResponse mirrors the JSON GitHub returns for POST
// /login/device/code.
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// pollResponse mirrors the JSON GitHub returns for POST
// /login/oauth/access_token. Either AccessToken or Error is set.
type pollResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// gembaExchangeResponse mirrors the gemba server's OAuth login wire
// shape (see internal/server/oauth.go oauthLoginResponse).
type gembaExchangeResponse struct {
	Token       string `json:"token"`
	TenantID    string `json:"tenant_id"`
	Subject     string `json:"subject"`
	DeviceLabel string `json:"device_label"`
}

// runGitHubLogin executes the end-to-end device flow + gemba exchange.
// It writes user-facing progress to out and persists the resulting
// gemba bearer to the credstore at credPath under the gemba server
// URL key.
func runGitHubLogin(ctx context.Context, out, stderr io.Writer, cfg githubDeviceFlowConfig, credPath string) error {
	if cfg.ClientID == "" {
		return errors.New("login --github: client id required (set GEMBA_OAUTH_GITHUB_CLIENT_ID or pass --github-client-id)")
	}
	if cfg.GembaServer == "" {
		return errors.New("login --github: gemba server URL is empty")
	}
	if cfg.GitHubBase == "" {
		cfg.GitHubBase = "https://github.com"
	}
	if cfg.Scopes == "" {
		cfg.Scopes = "repo,user:email"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	// Phase 1: device code.
	dc, err := requestDeviceCode(ctx, httpClient, cfg.GitHubBase, cfg.ClientID, cfg.Scopes)
	if err != nil {
		return fmt.Errorf("login --github: device code: %w", err)
	}
	verify := dc.VerificationURI
	if verify == "" {
		verify = "https://github.com/login/device"
	}
	fmt.Fprintf(out, "Open %s and enter the code: %s\n", verify, dc.UserCode)
	fmt.Fprintln(out, "Waiting for you to authorize the application...")

	// Phase 2: poll.
	interval := time.Duration(dc.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if cfg.PollInterval > 0 {
		interval = cfg.PollInterval
	}
	ghToken, err := pollForGitHubToken(ctx, httpClient, cfg.GitHubBase, cfg.ClientID, dc.DeviceCode, interval)
	if err != nil {
		return fmt.Errorf("login --github: %w", err)
	}

	// Phase 3: exchange with gemba server.
	exch, err := exchangeGitHubTokenWithGemba(ctx, httpClient, cfg.GembaServer, ghToken)
	if err != nil {
		return fmt.Errorf("login --github: exchange: %w", err)
	}

	// Persist via credstore.
	store, err := credstore.Load(credPath)
	if err != nil {
		return fmt.Errorf("login --github: load credentials: %w", err)
	}
	cred := credstore.Credential{
		Token:    exch.Token,
		Subject:  exch.Subject,
		StoredAt: time.Now().UTC(),
	}
	if err := store.Put(cfg.GembaServer, cred); err != nil {
		return fmt.Errorf("login --github: persist credentials: %w", err)
	}
	fmt.Fprintf(out, "Logged in to %s as %s (tenant %s, device %s).\n",
		cfg.GembaServer, exch.Subject, exch.TenantID, exch.DeviceLabel)
	return nil
}

// requestDeviceCode performs the POST /login/device/code call.
func requestDeviceCode(ctx context.Context, c *http.Client, base, clientID, scopes string) (deviceCodeResponse, error) {
	endpoint := strings.TrimRight(base, "/") + "/login/device/code"
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", scopes)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return deviceCodeResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return deviceCodeResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceCodeResponse{}, fmt.Errorf("github returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out deviceCodeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return deviceCodeResponse{}, fmt.Errorf("decode device code: %w", err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return deviceCodeResponse{}, fmt.Errorf("github returned empty device/user code")
	}
	return out, nil
}

// pollForGitHubToken loops POST /login/oauth/access_token until GitHub
// either returns an access_token, an unrecoverable error
// (expired_token, access_denied, etc), or the context is cancelled.
//
// The two recoverable states:
//   - authorization_pending: keep polling at the current interval
//   - slow_down: bump the interval by +5s (per RFC 8628 §3.5)
func pollForGitHubToken(ctx context.Context, c *http.Client, base, clientID, deviceCode string, interval time.Duration) (string, error) {
	endpoint := strings.TrimRight(base, "/") + "/login/oauth/access_token"
	cur := interval
	if cur <= 0 {
		cur = 5 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("cancelled: %w", ctx.Err())
		case <-time.After(cur):
		}
		form := url.Values{}
		form.Set("client_id", clientID)
		form.Set("device_code", deviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := c.Do(req)
		if err != nil {
			return "", err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var pr pollResponse
		if err := json.Unmarshal(body, &pr); err != nil {
			return "", fmt.Errorf("decode poll: %w (body=%s)", err, string(body))
		}
		if pr.AccessToken != "" {
			return pr.AccessToken, nil
		}
		switch pr.Error {
		case "authorization_pending":
			// keep polling at the current interval
		case "slow_down":
			// RFC 8628: extend the interval. GitHub's convention is +5s.
			cur += 5 * time.Second
		case "":
			return "", fmt.Errorf("unexpected response: %s", string(body))
		default:
			msg := pr.Error
			if pr.ErrorDesc != "" {
				msg += ": " + pr.ErrorDesc
			}
			return "", fmt.Errorf("%s", msg)
		}
	}
}

// exchangeGitHubTokenWithGemba POSTs the GitHub OAuth token to the
// gemba server's /api/v1/auth/oauth/github/login endpoint and returns
// the minted gemba bearer + tenant id.
func exchangeGitHubTokenWithGemba(ctx context.Context, c *http.Client, gembaServer, ghToken string) (gembaExchangeResponse, error) {
	endpoint := strings.TrimRight(gembaServer, "/") + "/api/v1/auth/oauth/github/login"
	body, _ := json.Marshal(map[string]string{"github_access_token": ghToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return gembaExchangeResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return gembaExchangeResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gembaExchangeResponse{}, fmt.Errorf("gemba returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out gembaExchangeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return gembaExchangeResponse{}, fmt.Errorf("decode exchange: %w", err)
	}
	if out.Token == "" || out.TenantID == "" {
		return gembaExchangeResponse{}, fmt.Errorf("gemba returned incomplete response")
	}
	return out, nil
}
