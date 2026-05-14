package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
)

// mockGitHubServer hosts both /login/device/code and
// /login/oauth/access_token. The accessTokenHandler closure controls
// the per-poll response, so a single test can sequence
// authorization_pending → slow_down → success.
type mockGitHubServer struct {
	server      *httptest.Server
	deviceCode  string
	pollHits    int32
	pollHandler func(hit int32) any // returns the body to encode
}

func newMockGitHub(t *testing.T, deviceCode string, ph func(hit int32) any) *mockGitHubServer {
	t.Helper()
	m := &mockGitHubServer{deviceCode: deviceCode, pollHandler: ph}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      deviceCode,
				"user_code":        "ABCD-1234",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
				"interval":         5,
			})
		case "/login/oauth/access_token":
			hit := atomic.AddInt32(&m.pollHits, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(m.pollHandler(hit))
		default:
			http.NotFound(w, r)
		}
	}))
	return m
}

// mockGembaServer hosts /api/v1/auth/oauth/github/login. The exchange
// closure returns the response body so each test can simulate first-
// login (new tenant) vs subsequent-login (existing tenant).
func newMockGemba(t *testing.T, exchange func(githubToken string) (map[string]any, int)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/oauth/github/login" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			GitHubAccessToken string `json:"github_access_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		body, status := exchange(req.GitHubAccessToken)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// TestRunGitHubLogin_HappyPath drives the full sequence against
// mocked GitHub + mocked gemba server. Polling returns the token on
// the first hit; the gemba server returns a bearer + tenant id.
func TestRunGitHubLogin_HappyPath(t *testing.T) {
	gh := newMockGitHub(t, "DEVCODE", func(int32) any {
		return map[string]any{"access_token": "gho_test", "token_type": "bearer"}
	})
	defer gh.server.Close()
	gm := newMockGemba(t, func(ghTok string) (map[string]any, int) {
		if ghTok != "gho_test" {
			return map[string]any{"error": "bad"}, http.StatusUnauthorized
		}
		return map[string]any{
			"token":        "gembatoken123",
			"tenant_id":    "t-abc12345",
			"subject":      "octocat",
			"device_label": "github-deadbeef",
		}, http.StatusOK
	})
	defer gm.Close()

	credPath := filepath.Join(t.TempDir(), "credentials.json")
	var out, errBuf bytes.Buffer
	cfg := githubDeviceFlowConfig{
		ClientID:     "Iv1.test",
		GitHubBase:   gh.server.URL,
		GembaServer:  gm.URL,
		HTTPClient:   gh.server.Client(),
		PollInterval: 1 * time.Millisecond,
	}
	if err := runGitHubLogin(context.Background(), &out, &errBuf, cfg, credPath); err != nil {
		t.Fatalf("runGitHubLogin: %v; stderr=%s", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "ABCD-1234") {
		t.Fatalf("user code not printed; got %q", out.String())
	}
	if !strings.Contains(out.String(), "octocat") {
		t.Fatalf("subject not printed; got %q", out.String())
	}
	// Credential persisted.
	store, err := credstore.Load(credPath)
	if err != nil {
		t.Fatalf("Load creds: %v", err)
	}
	c, ok := store.Get(gm.URL)
	if !ok || c.Token != "gembatoken123" || c.Subject != "octocat" {
		t.Fatalf("stored credential mismatch: %+v ok=%v", c, ok)
	}
}

// TestPollForGitHubToken_AuthorizationPending verifies the polling
// loop retries on authorization_pending (200, retry) and eventually
// succeeds.
func TestPollForGitHubToken_AuthorizationPending(t *testing.T) {
	gh := newMockGitHub(t, "DEV", func(hit int32) any {
		if hit < 3 {
			return map[string]any{"error": "authorization_pending"}
		}
		return map[string]any{"access_token": "gho_ok"}
	})
	defer gh.server.Close()
	tok, err := pollForGitHubToken(context.Background(), gh.server.Client(),
		gh.server.URL, "id", "DEV", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if tok != "gho_ok" {
		t.Fatalf("token: got %q want gho_ok", tok)
	}
	if atomic.LoadInt32(&gh.pollHits) < 3 {
		t.Fatalf("expected >=3 poll hits, got %d", gh.pollHits)
	}
}

// TestPollForGitHubToken_SlowDown verifies the slow_down response
// extends the poll interval. We measure that two consecutive
// slow_down responses cause noticeably more elapsed time than a
// straight success.
func TestPollForGitHubToken_SlowDown(t *testing.T) {
	gh := newMockGitHub(t, "DEV", func(hit int32) any {
		if hit == 1 {
			return map[string]any{"error": "slow_down"}
		}
		return map[string]any{"access_token": "gho_slow"}
	})
	defer gh.server.Close()
	// Start with a very small interval so slow_down's +5s bump is
	// the dominant cost. We then assert the elapsed time exceeds
	// ~4s (lower than the +5s GitHub-spec bump to absorb scheduler
	// jitter on CI).
	start := time.Now()
	// Use a tighter +bump via direct call: pollForGitHubToken adds
	// 5s on slow_down regardless of the supplied initial interval.
	// To avoid making the test 5s long we monkey-patch nothing —
	// instead we only assert the polling sequence completes and
	// that >=2 hits occurred. Real-time bump correctness is
	// covered by the implementation's literal +5*time.Second
	// addition.
	tok, err := pollForGitHubToken(context.Background(), gh.server.Client(),
		gh.server.URL, "id", "DEV", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if tok != "gho_slow" {
		t.Fatalf("token mismatch: %q", tok)
	}
	if gh.pollHits < 2 {
		t.Fatalf("expected >=2 polls, got %d", gh.pollHits)
	}
	// The total elapsed must include at least one +5s bump from
	// slow_down (the second poll waits 1ms+5s after the first).
	if time.Since(start) < 4*time.Second {
		t.Fatalf("slow_down should have extended interval; elapsed=%v", time.Since(start))
	}
}

// TestPollForGitHubToken_Cancel verifies a cancelled context exits
// the poll loop cleanly with a wrapped context.Canceled.
func TestPollForGitHubToken_Cancel(t *testing.T) {
	gh := newMockGitHub(t, "DEV", func(int32) any {
		return map[string]any{"error": "authorization_pending"}
	})
	defer gh.server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel up-front so the first poll observes ctx.Err() deterministically.
	// The previous version raced a sleep-then-cancel goroutine against the
	// poll loop and was flaky under loaded CI runners.
	cancel()
	_, err := pollForGitHubToken(ctx, gh.server.Client(),
		gh.server.URL, "id", "DEV", 1*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error on cancel, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error should wrap context.Canceled; got %v", err)
	}
}

// TestExchangeGitHubTokenWithGemba_FirstLogin verifies a fresh
// GitHub identity gets a new tenant id back. (The "create tenant on
// first login" semantics live server-side; the CLI test confirms
// the call shape + response decoding.)
func TestExchangeGitHubTokenWithGemba_FirstLogin(t *testing.T) {
	gm := newMockGemba(t, func(ghTok string) (map[string]any, int) {
		return map[string]any{
			"token":        "geofresh",
			"tenant_id":    "t-new00001",
			"subject":      "newuser",
			"device_label": "github-aabbccdd",
		}, http.StatusOK
	})
	defer gm.Close()
	resp, err := exchangeGitHubTokenWithGemba(context.Background(),
		gm.Client(), gm.URL, "gho_fresh")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.TenantID != "t-new00001" || resp.Token != "geofresh" {
		t.Fatalf("resp: %+v", resp)
	}
}

// TestExchangeGitHubTokenWithGemba_Existing simulates a subsequent
// login from the same GitHub identity: the server returns the SAME
// tenant id (it found the existing row). The CLI side cares only
// that the bearer round-trips; the server-side find-or-create is
// covered in oauth_server_test.go.
func TestExchangeGitHubTokenWithGemba_Existing(t *testing.T) {
	gm := newMockGemba(t, func(ghTok string) (map[string]any, int) {
		return map[string]any{
			"token":        "geo-second",
			"tenant_id":    "t-existing",
			"subject":      "octocat",
			"device_label": "github-existing",
		}, http.StatusOK
	})
	defer gm.Close()
	resp, err := exchangeGitHubTokenWithGemba(context.Background(),
		gm.Client(), gm.URL, "gho_second")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.TenantID != "t-existing" {
		t.Fatalf("tenant: %q", resp.TenantID)
	}
}
