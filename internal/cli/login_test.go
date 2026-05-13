package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
)

// gm-o9t8.1.1.2: a valid token validates against /api/whoami and is
// persisted to the credentials file. The output line matches the AC
// "Logged in to <server> as <subject>."
func TestLogin_PersistsTokenOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/whoami" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer TESTPAT" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized", "code": "invalid_bearer",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"subject": "operator", "server_version": "test",
		})
	}))
	defer srv.Close()

	credPath := filepath.Join(t.TempDir(), "credentials.json")
	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(srv.Client())

	if err := runLogin(context.Background(), &out, &errBuf,
		srv.URL, "TESTPAT", credPath, caller); err != nil {
		t.Fatalf("runLogin: %v; stderr=%s", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "Logged in to "+srv.URL+" as operator.") {
		t.Fatalf("login output: got %q", out.String())
	}

	// File is 0600.
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode: want 0600, got %#o", mode)
	}

	// Credential round-trips.
	store, err := credstore.Load(credPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, ok := store.Get(srv.URL)
	if !ok || c.Token != "TESTPAT" || c.Subject != "operator" {
		t.Fatalf("stored cred: %+v ok=%v", c, ok)
	}
}

// gm-o9t8.1.1.2: a server-rejected token does NOT persist to the
// credentials file. Server error envelope flows to stderr; runLogin
// returns a non-nil error.
func TestLogin_DoesNotPersistOnRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "unauthorized", "code": "invalid_bearer",
		})
	}))
	defer srv.Close()

	credPath := filepath.Join(t.TempDir(), "credentials.json")
	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(srv.Client())

	err := runLogin(context.Background(), &out, &errBuf,
		srv.URL, "BAD", credPath, caller)
	if err == nil {
		t.Fatalf("runLogin: want error, got nil")
	}
	if !strings.Contains(errBuf.String(), "invalid_bearer") {
		t.Fatalf("stderr should contain server error envelope; got %q", errBuf.String())
	}
	// Credentials file must not have been created.
	if _, err := os.Stat(credPath); !os.IsNotExist(err) {
		t.Fatalf("credentials file should not exist after rejection; err=%v", err)
	}
}

// gm-o9t8.1.1.2: whoami reads the stored credential, calls /api/whoami
// to confirm, and prints "<subject> @ <server>".
func TestWhoami_HumanOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TESTPAT" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"subject": "operator"})
	}))
	defer srv.Close()

	credPath := filepath.Join(t.TempDir(), "credentials.json")
	store, _ := credstore.Load(credPath)
	_ = store.Put(srv.URL, credstore.Credential{Token: "TESTPAT", Subject: "operator"})

	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(srv.Client())
	if err := runWhoami(context.Background(), &out, &errBuf, "", credPath, false, caller); err != nil {
		t.Fatalf("runWhoami: %v; stderr=%s", err, errBuf.String())
	}
	want := "operator @ " + srv.URL
	if !strings.Contains(out.String(), want) {
		t.Fatalf("whoami output: got %q want substring %q", out.String(), want)
	}
}

// gm-o9t8.1.1.2: whoami --json emits the documented envelope.
func TestWhoami_JSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"subject": "operator"})
	}))
	defer srv.Close()

	credPath := filepath.Join(t.TempDir(), "credentials.json")
	store, _ := credstore.Load(credPath)
	_ = store.Put(srv.URL, credstore.Credential{Token: "TESTPAT", Subject: "operator"})

	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(srv.Client())
	if err := runWhoami(context.Background(), &out, &errBuf, srv.URL, credPath, true, caller); err != nil {
		t.Fatalf("runWhoami: %v", err)
	}
	var got whoamiJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; out=%s", err, out.String())
	}
	if !got.Authenticated || got.Subject != "operator" || got.Server != srv.URL {
		t.Fatalf("json output mismatch: %+v", got)
	}
}

// gm-o9t8.1.1.2: whoami with no credentials prints "not logged in".
func TestWhoami_NotLoggedIn(t *testing.T) {
	credPath := filepath.Join(t.TempDir(), "credentials.json")
	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(nil)
	if err := runWhoami(context.Background(), &out, &errBuf, "", credPath, false, caller); err != nil {
		t.Fatalf("runWhoami: %v", err)
	}
	if !strings.Contains(out.String(), "not logged in") {
		t.Fatalf("want 'not logged in'; got %q", out.String())
	}
}

// gm-o9t8.1.1.2: whoami with --server omitted and a single stored
// server auto-picks that server.
func TestWhoami_AutoPicksSoleServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"subject": "operator"})
	}))
	defer srv.Close()

	credPath := filepath.Join(t.TempDir(), "credentials.json")
	store, _ := credstore.Load(credPath)
	_ = store.Put(srv.URL, credstore.Credential{Token: "T", Subject: "operator"})

	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(srv.Client())
	if err := runWhoami(context.Background(), &out, &errBuf, "", credPath, false, caller); err != nil {
		t.Fatalf("runWhoami: %v", err)
	}
	if !strings.Contains(out.String(), srv.URL) {
		t.Fatalf("want server in output; got %q", out.String())
	}
}

// gm-o9t8.1.1.2: whoami with --server omitted and multiple stored
// servers requires --server explicitly.
func TestWhoami_MultipleServersRequiresFlag(t *testing.T) {
	credPath := filepath.Join(t.TempDir(), "credentials.json")
	store, _ := credstore.Load(credPath)
	_ = store.Put("http://a", credstore.Credential{Token: "1"})
	_ = store.Put("http://b", credstore.Credential{Token: "2"})

	var out, errBuf bytes.Buffer
	caller := defaultWhoamiCaller(nil)
	err := runWhoami(context.Background(), &out, &errBuf, "", credPath, false, caller)
	if err == nil {
		t.Fatalf("want error, got nil; out=%s", out.String())
	}
	if !strings.Contains(err.Error(), "multiple servers") {
		t.Fatalf("error should hint multi-server; got %v", err)
	}
}

// gm-o9t8.1.1.2: resolveLoginToken reads from stdin under --token-stdin.
func TestResolveLoginToken_FromStdin(t *testing.T) {
	in := strings.NewReader("TESTPAT\n")
	got, err := resolveLoginToken(in, &bytes.Buffer{}, "", true)
	if err != nil {
		t.Fatalf("resolveLoginToken: %v", err)
	}
	if got != "TESTPAT" {
		t.Fatalf("want TESTPAT, got %q", got)
	}
}
