package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/auth"
	"github.com/GembaCore/gemba-core/internal/auth/oauth"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// newTestRouterWithOAuth builds a fully-wired OAuth router using the
// in-memory stores and a stub GitHub fetcher. Mirror of the inline
// wiring cmd/gemba serve does at boot.
func newTestRouterWithOAuth(t *testing.T, ghUser oauth.GitHubUser) (*Router, *MemTokenStore, tenant.Store) {
	t.Helper()
	r := NewRouter(config.ServeConfig{}, fs.FS(nil), nil)
	o, err := oauth.New(oauth.Config{GitHubClientID: "id", GitHubClientSecret: "sec"})
	if err != nil {
		t.Fatalf("oauth.New: %v", err)
	}
	r.AttachOAuth(o)
	ts := NewMemTokenStore()
	r.AttachOAuthTokenStore(ts)
	tStore := tenant.NewMemStore()
	if err := tStore.Migrate(context.Background()); err != nil {
		t.Fatalf("tenant migrate: %v", err)
	}
	r.AttachTenantStore(tStore)
	r.SetGitHubUserFetcherForTest(func(ctx context.Context, token string) (oauth.GitHubUser, error) {
		if token == "" {
			return oauth.GitHubUser{}, http.ErrAbortHandler
		}
		return ghUser, nil
	})
	return r, ts, tStore
}

// TestOAuthLogin_FirstLogin verifies the device-flow exchange creates
// a fresh tenant when no row exists for this GitHub id.
func TestOAuthLogin_FirstLogin(t *testing.T) {
	r, tokens, tStore := newTestRouterWithOAuth(t, oauth.GitHubUser{
		ID: 12345, Login: "octocat",
	})

	req := httptest.NewRequest(http.MethodPost, oauth.LoginPath,
		bytes.NewReader([]byte(`{"github_access_token":"gho_x"}`)))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Token       string `json:"token"`
		TenantID    string `json:"tenant_id"`
		Subject     string `json:"subject"`
		DeviceLabel string `json:"device_label"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" || len(resp.Token) != 64 {
		t.Fatalf("token bad: %q", resp.Token)
	}
	if resp.TenantID == "" || resp.TenantID == string(tenant.DefaultTenant) {
		t.Fatalf("tenant id: %q (expected fresh tenant)", resp.TenantID)
	}
	if resp.Subject != "octocat" {
		t.Fatalf("subject: %q", resp.Subject)
	}
	// Tenant row exists.
	if _, err := tStore.GetByGitHub(context.Background(), 12345); err != nil {
		t.Fatalf("tenant not provisioned: %v", err)
	}
	// Token hash persisted; plaintext verifies.
	tid, ok, err := tokens.VerifyToken(context.Background(), resp.Token)
	if err != nil || !ok {
		t.Fatalf("VerifyToken: ok=%v err=%v", ok, err)
	}
	if string(tid) != resp.TenantID {
		t.Fatalf("verified tenant mismatch: got %s want %s", tid, resp.TenantID)
	}
	// And no plaintext should equal the stored phc.
	for _, rec := range tokens.Records() {
		if rec.HashPHC == resp.Token {
			t.Fatalf("plaintext token leaked into hash field")
		}
		ok, err := auth.VerifyHash(resp.Token, rec.HashPHC)
		if err != nil || !ok {
			t.Fatalf("auth.VerifyHash mismatch: ok=%v err=%v", ok, err)
		}
	}
}

// TestOAuthLogin_SubsequentLogin verifies a second login from the
// same GitHub id resolves to the existing tenant (no new tenant row).
func TestOAuthLogin_SubsequentLogin(t *testing.T) {
	r, _, tStore := newTestRouterWithOAuth(t, oauth.GitHubUser{
		ID: 999, Login: "octocat",
	})
	// First login.
	req1 := httptest.NewRequest(http.MethodPost, oauth.LoginPath,
		bytes.NewReader([]byte(`{"github_access_token":"gho_a"}`)))
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first login status: %d", rr1.Code)
	}
	var resp1 struct {
		TenantID string `json:"tenant_id"`
	}
	_ = json.Unmarshal(rr1.Body.Bytes(), &resp1)

	// Second login from the same GitHub id.
	req2 := httptest.NewRequest(http.MethodPost, oauth.LoginPath,
		bytes.NewReader([]byte(`{"github_access_token":"gho_b"}`)))
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second login status: %d", rr2.Code)
	}
	var resp2 struct {
		TenantID string `json:"tenant_id"`
	}
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp2)

	if resp1.TenantID != resp2.TenantID {
		t.Fatalf("tenant id changed across logins: %s -> %s", resp1.TenantID, resp2.TenantID)
	}
	// Tenant list has exactly one row past DefaultTenant.
	all, _ := tStore.List(context.Background(), tenant.ListOptions{})
	count := 0
	for _, tt := range all {
		if tt.ID != tenant.DefaultTenant {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 OAuth-provisioned tenant, got %d", count)
	}
}

// TestOAuthLogin_Disabled verifies the 503 envelope when OAuth is
// not configured.
func TestOAuthLogin_Disabled(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fs.FS(nil), nil)
	req := httptest.NewRequest(http.MethodPost, oauth.LoginPath,
		bytes.NewReader([]byte(`{"github_access_token":"x"}`)))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d (want 503)", rr.Code)
	}
	var body struct {
		Error map[string]string `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Error["code"] != "oauth_not_configured" {
		t.Fatalf("code: %q", body.Error["code"])
	}
}

// TestOAuthCallback_Disabled verifies the GET callback also returns
// 503 when OAuth is unconfigured.
func TestOAuthCallback_Disabled(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fs.FS(nil), nil)
	req := httptest.NewRequest(http.MethodGet, oauth.CallbackPath, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d (want 503)", rr.Code)
	}
}

// TestOAuthCallback_Enabled returns the device-flow status when OAuth
// is configured.
func TestOAuthCallback_Enabled(t *testing.T) {
	r, _, _ := newTestRouterWithOAuth(t, oauth.GitHubUser{ID: 1, Login: "x"})
	req := httptest.NewRequest(http.MethodGet, oauth.CallbackPath, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Fatalf("body: %v", body)
	}
}
