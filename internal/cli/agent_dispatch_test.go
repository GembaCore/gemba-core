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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"wrapper_id": "gm-abc.1.2",
			"dispatched": []map[string]any{},
		})
	}))
	defer srv.Close()

	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"agent", "dispatch", "gm-abc.1.2", "--base-url", srv.URL})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v (out=%s)", err, out.String())
	}
	if got := strings.TrimSpace(out.String()); got != "gm-abc.1.2" {
		t.Errorf("stdout: want %q got %q", "gm-abc.1.2", got)
	}
	if gotPath != "/api/work-items/gm-abc.1.2/cascade-dispatch" {
		t.Errorf("path: %q", gotPath)
	}
	_ = gotBody
}

func TestAgentDispatch_SurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
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
