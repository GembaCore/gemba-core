package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentDispatch_PrintsRunID(t *testing.T) {
	var gotPath string
	var gotBody []byte
	var gotAuth, gotConfirm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotConfirm = r.Header.Get("X-GEMBA-Confirm")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"wrapper_id": "gm-abc.1.2",
			"dispatched": []map[string]any{},
		})
	}))
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()

	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"agent", "dispatch", "gm-abc.1.2", "--base-url", srv.URL})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v (out=%s err=%s)", err, out.String(), errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "gm-abc.1.2" {
		t.Errorf("stdout: want %q got %q", "gm-abc.1.2", got)
	}
	if gotPath != "/api/work-items/gm-abc.1.2/cascade-dispatch" {
		t.Errorf("path: %q", gotPath)
	}
	if gotAuth != "Bearer TESTPAT" {
		t.Errorf("auth header missing/wrong: %q", gotAuth)
	}
	if gotConfirm == "" {
		t.Errorf("X-GEMBA-Confirm header missing")
	}
	_ = gotBody
}

func TestAgentDispatch_SurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"agent", "dispatch", "gm-x", "--base-url", srv.URL})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestAgentDispatch_RequiresArg(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"agent", "dispatch"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when bead id missing")
	}
}

// TestAgentDispatch_ServerFlag verifies the canonical --server flag
// works alongside the deprecated --base-url alias (gm-o9t8.1.13).
func TestAgentDispatch_ServerFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"wrapper_id": "gm-srv.1"})
	}))
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"agent", "dispatch", "gm-x", "--server", srv.URL})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v (err=%s)", err, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "gm-srv.1" {
		t.Errorf("stdout: want %q got %q", "gm-srv.1", got)
	}
	if strings.Contains(errOut.String(), "deprecated") {
		t.Errorf("--server should NOT emit deprecation warning; stderr=%q", errOut.String())
	}
}

// TestAgentDispatch_BaseURLDeprecationWarning verifies --base-url
// still works but emits a one-line stderr warning (gm-o9t8.1.13).
func TestAgentDispatch_BaseURLDeprecationWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"wrapper_id": "gm-old.1"})
	}))
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"agent", "dispatch", "gm-x", "--base-url", srv.URL})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(errOut.String(), "--base-url is deprecated") {
		t.Errorf("expected deprecation warning on stderr, got %q", errOut.String())
	}
}
