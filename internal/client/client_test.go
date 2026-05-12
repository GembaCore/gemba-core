// client_test.go: behavioral tests for the gemba typed HTTP client.
//
// Each test stands up a single-route httptest.Server, points the
// wrapper at it, and inspects the headers the wrapper sent or the
// error it returned. The tests stay narrow on purpose — codegen
// correctness is the generator's responsibility; this file only
// asserts the wrapper-level concerns (auth, confirm header, error
// mapping, credstore plumbing).
//
// gm-o9t8.1.1.4

package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
	"github.com/GembaCore/gemba-core/internal/client"
)

// seededCredStore writes a single credential for server and returns
// the file path the wrapper should load.
func seededCredStore(t *testing.T, server, token string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	store, err := credstore.Load(path)
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}
	if err := store.Put(server, credstore.Credential{
		Token:    token,
		Subject:  "alice",
		StoredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed credstore: %v", err)
	}
	return path
}

// whoamiHandler builds a /api/whoami handler that records request
// headers and replies with status and body.
type recordedRequest struct {
	auth    string
	confirm string
	ua      string
	method  string
}

func newRecorder(status int, body string) (*recordedRequest, http.Handler) {
	rec := &recordedRequest{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.auth = r.Header.Get("Authorization")
		rec.confirm = r.Header.Get("X-GEMBA-Confirm")
		rec.ua = r.Header.Get("User-Agent")
		rec.method = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return rec, h
}

// TestBearerInjectedFromCredstore proves AC: "auth headers injected
// from credential store."
func TestBearerInjectedFromCredstore(t *testing.T) {
	rec, h := newRecorder(http.StatusOK, `{"subject":"alice","server_version":"test"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()

	credPath := seededCredStore(t, ts.URL, "TOK-1234")
	c, err := client.New(ts.URL, client.WithCredentialsPath(credPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Whoami(context.Background()); err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got, want := rec.auth, "Bearer TOK-1234"; got != want {
		t.Fatalf("auth header = %q; want %q", got, want)
	}
}

// TestConfirmHeaderOnPOST proves the X-GEMBA-Confirm nonce is set on
// mutating calls and is a valid UUID.
func TestConfirmHeaderOnPOST(t *testing.T) {
	rec, h := newRecorder(http.StatusAccepted, `{}`)
	ts := httptest.NewServer(h)
	defer ts.Close()

	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Drive a generated POST so the wrapper's request editors run.
	// StartNewProject is a zero-arg POST.
	if _, err := c.StartNewProjectWithResponse(context.Background()); err != nil {
		t.Fatalf("StartNewProjectWithResponse: %v", err)
	}
	if rec.method != http.MethodPost {
		t.Fatalf("server saw method %q; want POST", rec.method)
	}
	if rec.confirm == "" {
		t.Fatal("X-GEMBA-Confirm not set on POST")
	}
	if _, err := uuid.Parse(rec.confirm); err != nil {
		t.Fatalf("X-GEMBA-Confirm %q is not a UUID: %v", rec.confirm, err)
	}
}

// TestNoConfirmHeaderOnGET proves the nonce is GET-immune so cache
// behavior stays predictable.
func TestNoConfirmHeaderOnGET(t *testing.T) {
	rec, h := newRecorder(http.StatusOK, `{"subject":"alice","server_version":"t"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()

	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Whoami(context.Background()); err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if rec.confirm != "" {
		t.Fatalf("X-GEMBA-Confirm leaked onto GET: %q", rec.confirm)
	}
}

// TestUserAgentDefaultAndOverride covers the User-Agent header.
func TestUserAgentDefaultAndOverride(t *testing.T) {
	rec, h := newRecorder(http.StatusOK, `{"subject":"a","server_version":"v"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()

	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Whoami(context.Background()); err != nil {
		t.Fatalf("Whoami default: %v", err)
	}
	if rec.ua != client.DefaultUserAgent {
		t.Fatalf("User-Agent = %q; want %q", rec.ua, client.DefaultUserAgent)
	}

	rec2, h2 := newRecorder(http.StatusOK, `{"subject":"a","server_version":"v"}`)
	ts2 := httptest.NewServer(h2)
	defer ts2.Close()
	c2, err := client.New(ts2.URL, client.WithToken("X"), client.WithUserAgent("gemba-cli/9.9.9"))
	if err != nil {
		t.Fatalf("New (override): %v", err)
	}
	if _, err := c2.Whoami(context.Background()); err != nil {
		t.Fatalf("Whoami override: %v", err)
	}
	if rec2.ua != "gemba-cli/9.9.9" {
		t.Fatalf("override UA = %q; want gemba-cli/9.9.9", rec2.ua)
	}
}

// TestErrorMapping_401 proves AC: "error envelope surfaces actionable
// text" + sentinel mapping for unauthorized.
func TestErrorMapping_401(t *testing.T) {
	_, h := newRecorder(http.StatusUnauthorized, `{"error":"missing token","code":"unauthorized"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()
	c, err := client.New(ts.URL, client.WithToken("BAD"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Whoami(context.Background())
	if !errors.Is(err, client.ErrUnauthorized) {
		t.Fatalf("err = %v; want ErrUnauthorized", err)
	}
	if got := err.Error(); got == "" || !contains(got, "missing token") {
		t.Fatalf("error text = %q; want it to include server message", got)
	}
}

func TestErrorMapping_404(t *testing.T) {
	_, h := newRecorder(http.StatusNotFound, `{"error":"not_found","message":"no such workspace"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()
	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Whoami(context.Background())
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("err = %v; want ErrNotFound", err)
	}
}

func TestErrorMapping_501(t *testing.T) {
	_, h := newRecorder(http.StatusNotImplemented, `{"error":"not_implemented","message":"M3"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()
	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Whoami(context.Background())
	if !errors.Is(err, client.ErrNotImplemented) {
		t.Fatalf("err = %v; want ErrNotImplemented", err)
	}
}

// TestErrorMapping_500 proves the generic *ServerError fallback.
func TestErrorMapping_500(t *testing.T) {
	_, h := newRecorder(http.StatusInternalServerError, `{"error":"X","message":"M"}`)
	ts := httptest.NewServer(h)
	defer ts.Close()
	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Whoami(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var se *client.ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T); want *ServerError", err, err)
	}
	if se.Code != "X" || se.Message != "M" || se.Status != 500 {
		t.Fatalf("ServerError fields = %+v; want {Code:X Message:M Status:500}", se)
	}
}

// TestWhoamiReturnsTypedResponse proves the wrapper hands back the
// typed generated struct (typed Go client AC).
func TestWhoamiReturnsTypedResponse(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"subject":        "alice",
		"server_version": "0.1.2",
	})
	_, h := newRecorder(http.StatusOK, string(body))
	ts := httptest.NewServer(h)
	defer ts.Close()
	c, err := client.New(ts.URL, client.WithToken("X"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got == nil || got.Subject != "alice" {
		t.Fatalf("Subject = %+v; want alice", got)
	}
}

// TestNewFailsWithoutCredential proves the credstore-default path
// returns a friendly error rather than panicking.
func TestNewFailsWithoutCredential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	_, err := client.New("http://nope.example", client.WithCredentialsPath(path))
	if err == nil {
		t.Fatal("expected error for empty credstore")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

