// events_test.go: behavioral tests for WorkItemEvents — the SSE
// transport helper that opens GET /events with the wrapper's auth +
// user-agent editor chain (gm-o9t8.1.11).
//
// The tests stand up an httptest.Server that records request headers,
// confirm the wrapper sends Authorization + Last-Event-Id correctly,
// and verify the 401 path maps to ErrUnauthorized.

package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/client"
)

// TestWorkItemEvents_InjectsBearer verifies the Authorization header
// from credstore is sent on the SSE GET request.
func TestWorkItemEvents_InjectsBearer(t *testing.T) {
	var gotAuth, gotLastID, gotAccept, gotPath, gotQuery string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotLastID = r.Header.Get("Last-Event-Id")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("work_item_id")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: hello\n\n")
	})
	ts := httptest.NewServer(h)
	defer ts.Close()

	credPath := seededCredStore(t, ts.URL, "TOK-EVT")
	c, err := client.New(ts.URL, client.WithCredentialsPath(credPath))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	body, err := c.WorkItemEvents(context.Background(), "gm-x.1", "")
	if err != nil {
		t.Fatalf("WorkItemEvents: %v", err)
	}
	defer body.Close()
	// Drain a bit so the server-side handler fully runs.
	buf := make([]byte, 64)
	_, _ = body.Read(buf)

	if gotAuth != "Bearer TOK-EVT" {
		t.Errorf("auth header: want %q, got %q", "Bearer TOK-EVT", gotAuth)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("accept header: want text/event-stream, got %q", gotAccept)
	}
	if gotPath != "/events" {
		t.Errorf("path: want /events, got %q", gotPath)
	}
	if gotQuery != "gm-x.1" {
		t.Errorf("work_item_id query: want gm-x.1, got %q", gotQuery)
	}
	if gotLastID != "" {
		t.Errorf("Last-Event-Id should be empty when none supplied, got %q", gotLastID)
	}
}

// TestWorkItemEvents_LastEventIDHeader verifies the Last-Event-Id
// header is forwarded when supplied.
func TestWorkItemEvents_LastEventIDHeader(t *testing.T) {
	var gotLastID string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLastID = r.Header.Get("Last-Event-Id")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(h)
	defer ts.Close()
	credPath := seededCredStore(t, ts.URL, "TOK-2")
	c, err := client.New(ts.URL, client.WithCredentialsPath(credPath))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	body, err := c.WorkItemEvents(context.Background(), "gm-x.2", "evt-99")
	if err != nil {
		t.Fatalf("WorkItemEvents: %v", err)
	}
	defer body.Close()
	if gotLastID != "evt-99" {
		t.Errorf("Last-Event-Id: want %q got %q", "evt-99", gotLastID)
	}
}

// TestWorkItemEvents_UnauthorizedMapping verifies a 401 from the
// server maps to ErrUnauthorized via the wrapper's typed error path.
func TestWorkItemEvents_UnauthorizedMapping(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid","code":"unauthenticated"}`))
	})
	ts := httptest.NewServer(h)
	defer ts.Close()
	credPath := seededCredStore(t, ts.URL, "TOK-3")
	c, err := client.New(ts.URL, client.WithCredentialsPath(credPath))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = c.WorkItemEvents(context.Background(), "gm-x.3", "")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !errors.Is(err, client.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

// TestWorkItemEvents_RejectsEmptyRunID guards the precondition.
func TestWorkItemEvents_RejectsEmptyRunID(t *testing.T) {
	// We can use any seeded URL; the func returns before any dial.
	dir := t.TempDir()
	_ = dir
	c, err := client.New("http://example.invalid", client.WithToken("x"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = c.WorkItemEvents(context.Background(), "  ", "")
	if err == nil || !strings.Contains(err.Error(), "runID is required") {
		t.Errorf("expected runID required error, got %v", err)
	}
}

// silence unused-import warning when httputil is not used elsewhere.
var _ = httputil.DumpRequest
