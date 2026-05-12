// Govern verbs — spec family stub registration tests (gm-o9t8.14).
//
// Each test asserts three things per route in the family:
//   - the route is registered (501 with the not_implemented envelope,
//     not chi's 404),
//   - it requires auth when the server runs in auth=token mode (401
//     without a bearer),
//   - the OpenAPI document declares the path so the typed client can
//     codegen against the same surface.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// govStubRoute is one row in the per-family table.
type govStubRoute struct {
	method     string
	path       string
	openapiKey string // path key in openapi.json (the canonical form with {wsid} etc.)
	writes     bool   // true means PATCH/POST that requires X-GEMBA-Confirm
}

// runGovStubTable exercises the three invariants for a table of
// stubbed routes. Auth-mode is token; the matching bearer drives a
// 501; an anonymous request must 401.
func runGovStubTable(t *testing.T, rows []govStubRoute) {
	t.Helper()
	cfg := config.ServeConfig{AuthMode: "token", AuthToken: "testpat"}
	h := NewRouter(cfg, fakeSPA(), nil)

	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("parse OpenAPISpec: %v", err)
	}

	for _, row := range rows {
		row := row
		t.Run(row.method+" "+row.path, func(t *testing.T) {
			// 1. OpenAPI declares the route.
			if _, ok := doc.Paths[row.openapiKey]; !ok {
				t.Errorf("openapi.json missing path %q", row.openapiKey)
			}
			// 2. Anonymous request → 401.
			req := httptest.NewRequest(row.method, row.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("anon %s %s = %d, want 401; body=%q", row.method, row.path, rec.Code, rec.Body.String())
			}
			// 3. Authed request → 501 (not 404).
			req = httptest.NewRequest(row.method, row.path, strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer testpat")
			req.Header.Set("Content-Type", "application/json")
			if row.writes {
				req.Header.Set(ConfirmHeader, "11111111-2222-4333-8444-555555555555")
			}
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("authed %s %s = %d, want 501; body=%q", row.method, row.path, rec.Code, rec.Body.String())
			}
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode stub envelope: %v; body=%q", err, rec.Body.String())
			}
			if env.Error.Code != "not_implemented" {
				t.Errorf("error.code = %q, want not_implemented; body=%q", env.Error.Code, rec.Body.String())
			}
		})
	}
}

// TestSpecsStubs_RegisteredAndAuthed covers every route in the spec
// family — list, patch, reconcile, watch, snapshot, adopt.
func TestSpecsStubs_RegisteredAndAuthed(t *testing.T) {
	runGovStubTable(t, []govStubRoute{
		{"GET", "/api/v1/workspaces/dummy/specs", "/api/v1/workspaces/{wsid}/specs", false},
		{"PATCH", "/api/v1/workspaces/dummy/specs/feature-x", "/api/v1/workspaces/{wsid}/specs/{slug}", true},
		{"POST", "/api/v1/workspaces/dummy/specs/feature-x/reconcile", "/api/v1/workspaces/{wsid}/specs/{slug}/reconcile", true},
		{"GET", "/api/v1/workspaces/dummy/specs/feature-x/watch", "/api/v1/workspaces/{wsid}/specs/{slug}/watch", false},
		{"POST", "/api/v1/workspaces/dummy/specs/feature-x/snapshot", "/api/v1/workspaces/{wsid}/specs/{slug}/snapshot", true},
		{"POST", "/api/v1/workspaces/dummy/specs/feature-x/adopt", "/api/v1/workspaces/{wsid}/specs/{slug}/adopt", true},
	})
}

// TestSpecsReconcile_NonceRequired pins the X-GEMBA-Confirm gate on
// the reconcile route so a future refactor can't accidentally drop
// the nonce requirement from a write endpoint.
func TestSpecsReconcile_NonceRequired(t *testing.T) {
	cfg := config.ServeConfig{AuthMode: "token", AuthToken: "testpat"}
	h := NewRouter(cfg, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/dummy/specs/feature-x/reconcile?apply=1", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer testpat")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reconcile without nonce = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
}
