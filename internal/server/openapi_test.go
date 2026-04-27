package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

// TestOpenAPISpec_Embedded — sanity check on the bytes baked into the
// binary. Fails fast if a future PR commits broken JSON or strips a
// load-bearing top-level field. Cheap belt-and-braces under the
// runtime handler's lazy-validate.
func TestOpenAPISpec_Embedded(t *testing.T) {
	if len(OpenAPISpec()) == 0 {
		t.Fatal("OpenAPISpec() returned empty bytes")
	}
	var doc map[string]any
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("OpenAPISpec did not parse as JSON: %v", err)
	}
	if doc["openapi"] == nil {
		t.Errorf("missing top-level openapi field")
	}
	if doc["paths"] == nil {
		t.Errorf("missing top-level paths field")
	}
	if doc["components"] == nil {
		t.Errorf("missing top-level components field")
	}
}

// TestOpenAPISpec_DocsCopyMatches — the canonical spec lives next to
// the embed (internal/server/openapi/openapi.json); docs/api/openapi.json
// is a published mirror. Drift between the two would mean a developer
// updated one and forgot the other; this test catches it at PR time.
//
// `make codegen` regenerates the docs/ copy from the embed source, so a
// failure here points at the obvious fix.
func TestOpenAPISpec_DocsCopyMatches(t *testing.T) {
	docsPath := filepath.Join("..", "..", "docs", "api", "openapi.json")
	got, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read %s: %v", docsPath, err)
	}
	if !bytes.Equal(bytes.TrimRight(got, "\n"), bytes.TrimRight(OpenAPISpec(), "\n")) {
		t.Fatalf(
			"%s and the embedded spec differ.\n"+
				"Run `make codegen` to regenerate the docs/ copy from the source.\n"+
				"docs len=%d, embed len=%d",
			docsPath, len(got), len(OpenAPISpec()))
	}
}

// TestOpenAPIHandler_Serves200 — the runtime route returns the
// embedded bytes verbatim with the right content-type header.
func TestOpenAPIHandler_Serves200(t *testing.T) {
	r := NewRouter(config.ServeConfig{Town: "test-town"}, fakeSPA(), nil)
	t.Cleanup(r.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), OpenAPISpec()) {
		t.Errorf("response body != OpenAPISpec()")
	}
}

// TestOpenAPI_DocumentsKeyEndpoints — the MVP slice (gm-e4.2) commits
// to documenting the read endpoints the SPA depends on. A drift here
// (route removed but spec not updated, or vice versa) would silently
// break the codegen client; pin the contract.
func TestOpenAPI_DocumentsKeyEndpoints(t *testing.T) {
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{
		"/api/health",
		"/api/version",
		"/api/config",
		"/api/repositories",
		"/api/work-items",
		"/api/work-items/{id}",
		"/api/sprints",
		"/api/agents",
		"/api/sessions",
		"/api/escalations",
		"/api/capabilities",
		"/api/openapi.json",
	}
	for _, p := range want {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("paths is missing %q", p)
		}
	}
}
