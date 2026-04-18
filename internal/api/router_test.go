package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/YOUR_ORG/gemba/internal/config"
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
