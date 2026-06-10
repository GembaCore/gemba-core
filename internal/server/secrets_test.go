// Tests for the workspace secrets HTTP surface (gm-o9t8.3.7).
//
// Coverage:
//   - PUT (POST + nonce) stores the value
//   - List exposes the key (and only the key — no value field)
//   - DELETE is idempotent and removes the key
//   - There is no GET endpoint that returns plaintext values

package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/vault"
)

// newSecretsRouter builds a Router with an on-disk vault rooted at
// t.TempDir(). Auth is left unset so tests don't have to thread a
// bearer through every call.
func newSecretsRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	kek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, kek); err != nil {
		t.Fatalf("seed kek: %v", err)
	}
	v, err := vault.New(vault.Options{
		Path: filepath.Join(dir, "vault.db"),
		KEK:  kek,
	})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	r.AttachVault(v)
	t.Cleanup(r.Close)
	t.Cleanup(func() {
		if c, ok := v.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
	return r
}

// doSecrets issues req with a fresh nonce when method requires one.
func doSecrets(t *testing.T, r *Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodDelete {
		req.Header.Set(ConfirmHeader, "aaaaaaaa-bbbb-4ccc-8ddd-"+randHex12(t))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func randHex12(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 12)
	for i, by := range b {
		out[2*i] = hex[by>>4]
		out[2*i+1] = hex[by&0x0f]
	}
	return string(out)
}

func TestSecrets_PutListDelete(t *testing.T) {
	r := newSecretsRouter(t)

	// PUT
	rec := doSecrets(t, r, http.MethodPost,
		"/api/v1/workspaces/ws1/secrets/API_KEY",
		`{"value":"hunter2"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("put = %d, want 204; body=%q", rec.Code, rec.Body.String())
	}

	// LIST returns the key but no value.
	rec = doSecrets(t, r, http.MethodGet, "/api/v1/workspaces/ws1/secrets", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var listed struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v; body=%q", err, rec.Body.String())
	}
	if len(listed.Keys) != 1 || listed.Keys[0] != "API_KEY" {
		t.Fatalf("keys = %v, want [API_KEY]", listed.Keys)
	}
	// Critical: the list payload must NOT contain the value.
	if bytes.Contains(rec.Body.Bytes(), []byte("hunter2")) {
		t.Fatalf("list response leaked the plaintext value: %q", rec.Body.String())
	}

	// DELETE
	rec = doSecrets(t, r, http.MethodDelete,
		"/api/v1/workspaces/ws1/secrets/API_KEY", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204; body=%q", rec.Code, rec.Body.String())
	}

	// Idempotent delete.
	rec = doSecrets(t, r, http.MethodDelete,
		"/api/v1/workspaces/ws1/secrets/API_KEY", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete (idempotent) = %d, want 204; body=%q", rec.Code, rec.Body.String())
	}

	// List now empty.
	rec = doSecrets(t, r, http.MethodGet, "/api/v1/workspaces/ws1/secrets", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list-after-delete: %v", err)
	}
	if len(listed.Keys) != 0 {
		t.Fatalf("keys after delete = %v, want empty", listed.Keys)
	}
}

func TestSecrets_NoPlaintextGetEndpoint(t *testing.T) {
	// There must be no route that returns the plaintext value. We
	// confirm this two ways:
	//   1. GET /secrets/{key} routes to 404 (chi never matched).
	//   2. The embedded OpenAPI document declares no operation that
	//      returns a value-bearing schema for the per-key path.
	r := newSecretsRouter(t)

	// First, populate a secret so that any accidental read path would
	// have something to surface.
	if rec := doSecrets(t, r, http.MethodPost,
		"/api/v1/workspaces/ws1/secrets/TOKEN",
		`{"value":"plaintext-leak-canary"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("seed put: %d, body=%q", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/workspaces/ws1/secrets/TOKEN", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("GET /secrets/{key} returned 200 — that route must not exist; body=%q", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("plaintext-leak-canary")) {
		t.Fatalf("response leaked the value despite non-200 status: %q", rec.Body.String())
	}

	// And the OpenAPI document agrees.
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("parse OpenAPISpec: %v", err)
	}
	if ops, ok := doc.Paths["/api/v1/workspaces/{wsid}/secrets/{key}"]; ok {
		if _, hasGet := ops["get"]; hasGet {
			t.Fatalf("OpenAPI declares a GET on /secrets/{key} — plaintext-leak surface")
		}
	}
}

func TestSecrets_VaultNotAttached_503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/workspaces/ws1/secrets", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("list without vault = %d, want 503; body=%q", rec.Code, rec.Body.String())
	}
}

func TestSecrets_OpenAPI_DocumentsEndpoints(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("parse OpenAPISpec: %v", err)
	}
	if _, ok := doc.Paths["/api/v1/workspaces/{wsid}/secrets"]; !ok {
		t.Fatal("openapi missing /api/v1/workspaces/{wsid}/secrets")
	}
	ops, ok := doc.Paths["/api/v1/workspaces/{wsid}/secrets/{key}"]
	if !ok {
		t.Fatal("openapi missing /api/v1/workspaces/{wsid}/secrets/{key}")
	}
	if _, ok := ops["post"]; !ok {
		t.Error("openapi missing POST on /secrets/{key}")
	}
	if _, ok := ops["delete"]; !ok {
		t.Error("openapi missing DELETE on /secrets/{key}")
	}
}
