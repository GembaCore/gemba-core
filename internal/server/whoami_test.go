package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// gm-o9t8.1.1.2: GET /api/whoami with a valid bearer returns the
// subject + server_version envelope.
func TestWhoami_AuthenticatedSuccess(t *testing.T) {
	cfg := config.ServeConfig{
		Listen:    "127.0.0.1",
		AuthMode:  "token",
		AuthToken: "good-token",
	}
	h := NewRouter(cfg, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var resp whoamiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if resp.Subject == "" {
		t.Fatalf("subject empty: %+v", resp)
	}
	if resp.ServerVersion == "" {
		t.Fatalf("server_version empty: %+v", resp)
	}
}

// gm-o9t8.1.1.2: GET /api/whoami without a bearer returns 401 via the
// upstream auth middleware.
func TestWhoami_MissingBearerReturns401(t *testing.T) {
	cfg := config.ServeConfig{
		Listen:    "127.0.0.1",
		AuthMode:  "token",
		AuthToken: "good-token",
	}
	h := NewRouter(cfg, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// gm-o9t8.1.1.2: an invalid bearer returns 401, not 200.
func TestWhoami_InvalidBearerReturns401(t *testing.T) {
	cfg := config.ServeConfig{
		Listen:    "127.0.0.1",
		AuthMode:  "token",
		AuthToken: "good-token",
	}
	h := NewRouter(cfg, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%q", rec.Code, rec.Body.String())
	}
}
