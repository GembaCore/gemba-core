package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/MikeBengtson/gemba/internal/config"
)

func fakeSPA() fstest.MapFS {
	return fstest.MapFS{
		"index.html":     {Data: []byte("<html>SPA</html>")},
		"assets/app.js":  {Data: []byte("console.log('hi')")},
		"assets/app.css": {Data: []byte("body{}")},
	}
}

func TestSPAFallback_ServesIndexForUnknownRoutes(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	// Unknown deep link — should return index.html, not 404.
	req := httptest.NewRequest(http.MethodGet, "/convoys/gm-abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected SPA index body, got: %q", rec.Body.String())
	}
}

func TestSPAFallback_ServesAssets(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("expected asset body, got: %q", rec.Body.String())
	}
}

// gm-b2: SPA fallback must not shadow API 404s. If it did, client fetches
// to unknown API paths would return HTML instead of JSON, breaking error
// handling in the frontend.
func TestAPIFallbackReturnsJSON404NotSPA(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d; body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html>") {
		t.Fatalf("api/* must not return HTML; got %q", rec.Body.String())
	}
}

func TestHealthEndpoint(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("want status:ok, got %q", rec.Body.String())
	}
}

// gm-b3 / gm-99g: token auth must enforce on every API request, including
// on a loopback bind. A missing or wrong bearer must 401 *before* route
// lookup — otherwise an unauth'd client can probe the route surface via
// 404-vs-401 oracles.
func TestTokenAuth_MissingBearer_Returns401(t *testing.T) {
	cfg := config.ServeConfig{Listen: "127.0.0.1", AuthMode: "token", AuthToken: "s3cret"}
	h := NewRouter(cfg, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

func TestTokenAuth_WrongBearer_Returns401(t *testing.T) {
	cfg := config.ServeConfig{Listen: "127.0.0.1", AuthMode: "token", AuthToken: "s3cret"}
	h := NewRouter(cfg, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

func TestTokenAuth_ValidBearer_Routes(t *testing.T) {
	cfg := config.ServeConfig{Listen: "127.0.0.1", AuthMode: "token", AuthToken: "s3cret"}
	h := NewRouter(cfg, fakeSPA())

	// Known route: /api/health → 200 with auth.
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 on /api/health, got %d; body=%q", rec.Code, rec.Body.String())
	}

	// Unknown route with valid bearer: routed through → 404 (not 401).
	req = httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown route with valid auth, got %d", rec.Code)
	}
}

// Regression guard: auth=none (default) must not accidentally start
// rejecting unauthenticated requests.
func TestAuthNone_NoBearerRequired(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with auth=none, got %d", rec.Code)
	}
}

func TestUnbuiltSPAShowsHint(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "make build") {
		t.Fatalf("expected build hint, got %q", rec.Body.String())
	}
}
