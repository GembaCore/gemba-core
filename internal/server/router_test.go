package server

import (
	"encoding/json"
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

// gm-b2 / gm-xke: API 404s must be a JSON envelope, not chi's default
// text/plain body and not the SPA shell. Frontend code using fetch().then(r=>r.json())
// will throw SyntaxError on anything else.
func TestAPIFallbackReturnsJSON404NotSPA(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d; body=%q", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want application/json, got %q; body=%q", ct, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html>") {
		t.Fatalf("api/* must not return HTML; got %q", rec.Body.String())
	}

	var env map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must parse as JSON: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] == "" {
		t.Fatalf("expected non-empty error key; got %v", env)
	}
	if env["path"] != "/api/v1/nonexistent" {
		t.Fatalf("expected path=/api/v1/nonexistent, got %q", env["path"])
	}
	if env["method"] != http.MethodGet {
		t.Fatalf("expected method=GET, got %q", env["method"])
	}
}

// gm-b2 / gm-xke: /events/* unknown paths must also return the JSON envelope.
func TestEventsFallbackReturnsJSON404(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/events/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d; body=%q", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want application/json, got %q; body=%q", ct, rec.Body.String())
	}
	var env map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body must parse as JSON: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] == "" {
		t.Fatalf("expected non-empty error key; got %v", env)
	}
}

// gm-b2 DoD: unknown non-API paths still serve the SPA shell so client-side
// routing works.
func TestSPAFallback_UnknownPageServesSPA(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA())
	req := httptest.NewRequest(http.MethodGet, "/not-a-real-page", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("want text/html, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "SPA") {
		t.Fatalf("expected SPA shell body, got %q", rec.Body.String())
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
