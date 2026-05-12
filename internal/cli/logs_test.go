package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogs_WithoutFollow_NotImplemented(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"logs", "gm-r.1"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for non-follow mode")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error should mention not implemented; got %v", err)
	}
}

func TestLogs_FollowStreamsEvents(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl.Flush()
		fmt.Fprint(w, "id: 1\nevent: hello\ndata: world\n\n")
		fl.Flush()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"logs", "-f", "gm-r.1", "--base-url", srv.URL, "--max-retries", "0"})
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
		t.Fatalf("timeout; out=%s", out.String())
	}
	if !strings.Contains(out.String(), "world") {
		t.Errorf("missing event payload; got %q", out.String())
	}
}

func TestLogs_FollowReconnectsWithLastEventID(t *testing.T) {
	var calls atomic.Int32
	var lastIDHeader atomic.Value
	lastIDHeader.Store("")

	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		lastIDHeader.Store(r.Header.Get("Last-Event-ID"))
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl.Flush()
		if n == 1 {
			// First attempt: emit one event with an id, then close
			// the connection (panic forces hijacker to drop).
			fmt.Fprint(w, "id: evt-42\nevent: tick\ndata: first\n\n")
			fl.Flush()
			// Close immediately by returning -> client sees EOF -> nil error -> stop.
			// To simulate a *drop* (retryable), we hijack and close abruptly.
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		// Second attempt: should carry Last-Event-ID; emit one more event.
		fmt.Fprint(w, "id: evt-43\nevent: tick\ndata: second\n\n")
		fl.Flush()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"logs", "-f", "gm-r.1", "--base-url", srv.URL, "--max-retries", "3"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("timeout; calls=%d out=%s", calls.Load(), out.String())
	}
	if calls.Load() < 2 {
		t.Errorf("expected at least 2 attempts (reconnect), got %d", calls.Load())
	}
	if got, _ := lastIDHeader.Load().(string); got != "evt-42" {
		t.Errorf("expected Last-Event-ID=evt-42 on reconnect, got %q", got)
	}
	if !strings.Contains(out.String(), "first") || !strings.Contains(out.String(), "second") {
		t.Errorf("missing events in output: %q", out.String())
	}
}
