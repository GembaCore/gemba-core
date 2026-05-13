package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseTestServer routes:
//
//	POST /api/work-items/{id}/cascade-dispatch  -> {wrapper_id}
//	GET  /events                                -> emits N frames then closes
func sseTestServer(t *testing.T, wrapperID string, frames []string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/work-items/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"wrapper_id": wrapperID,
			"dispatched": []map[string]any{},
		})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		fl.Flush()
		for _, f := range frames {
			_, _ = fmt.Fprint(w, f)
			fl.Flush()
		}
		// Returning closes the body so the client sees EOF.
	})
	return httptest.NewServer(mux)
}

func TestRun_NoAttachPrintsRunID(t *testing.T) {
	srv := sseTestServer(t, "gm-r.1", nil)
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"run", "gm-x.1", "--base-url", srv.URL, "--no-attach"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "gm-r.1") {
		t.Errorf("want run_id in output, got %q", out.String())
	}
}

func TestRun_AttachStreamsEvents(t *testing.T) {
	frames := []string{
		"event: session.transition\ndata: {\"kind\":\"session.transition\",\"summary\":\"hello\"}\n\n",
		"event: workitem.created\ndata: {\"kind\":\"workitem.created\",\"summary\":\"world\"}\n\n",
	}
	srv := sseTestServer(t, "gm-r.2", frames)
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()

	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"run", "gm-x.2", "--base-url", srv.URL})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute: %v out=%s", err, out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("run never completed; out=%s", out.String())
	}
	s := out.String()
	if !strings.Contains(s, "hello") || !strings.Contains(s, "world") {
		t.Errorf("want both events in output, got %q", s)
	}
}

func TestRun_JSONModeEmitsRawData(t *testing.T) {
	frames := []string{"data: {\"x\":1}\n\n"}
	srv := sseTestServer(t, "gm-r.3", frames)
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"run", "gm-x.3", "--base-url", srv.URL, "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if !strings.Contains(out.String(), `{"x":1}`) {
		t.Errorf("json mode should emit raw payload, got %q", out.String())
	}
}

// TestRun_ServerFlag verifies the canonical --server flag works on
// `gemba run` (gm-o9t8.1.13).
func TestRun_ServerFlag(t *testing.T) {
	srv := sseTestServer(t, "gm-r.s", nil)
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"run", "gm-x", "--server", srv.URL, "--no-attach"})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "gm-r.s") {
		t.Errorf("want run_id in output, got %q", out.String())
	}
	if strings.Contains(errOut.String(), "deprecated") {
		t.Errorf("--server should not emit deprecation warning; stderr=%q", errOut.String())
	}
}

// TestRun_BaseURLDeprecationWarning verifies --base-url emits a stderr
// warning when used on `gemba run` (gm-o9t8.1.13).
func TestRun_BaseURLDeprecationWarning(t *testing.T) {
	srv := sseTestServer(t, "gm-r.b", nil)
	defer srv.Close()
	defer withRunClientFactory(httpClientFactoryForTests(srv.URL))()
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"run", "gm-x", "--base-url", srv.URL, "--no-attach"})
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
