package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/audit"
	"github.com/GembaCore/gemba-core/internal/config"
)

// TestCreateWorkItem_EmitsBeadCreateAudit covers the producer wiring
// (gm-o9t8.3.8, Workstream B): a successful POST /api/work-items must
// land a bead.create event in the bound Auditor. The handler is the
// single proof-of-life producer in v1 — the vault + microVM auditors
// arrive in later beads.
func TestCreateWorkItem_EmitsBeadCreateAudit(t *testing.T) {
	host, _ := newCreateHost(t)
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	dir := t.TempDir()
	a, err := audit.New(audit.Options{
		Dir: dir,
		Key: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a) })
	h.AttachAuditor(a)

	body := map[string]any{
		"item": map[string]any{
			"title":          "first audited bead",
			"kind":           "task",
			"status":         "open",
			"state_category": "backlog",
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, postCreateReq(t, "nonce-AUD", body))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%q", w.Code, w.Body.String())
	}

	events, err := a.Query(context.Background(), audit.Query{Type: audit.EventBeadCreate})
	if err != nil {
		t.Fatalf("audit Query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 bead.create event, got %d", len(events))
	}
	ev := events[0]
	if ev.Resource != "workitem:gm-new" {
		t.Fatalf("unexpected resource %q", ev.Resource)
	}
	if ev.Outcome != audit.OutcomeOK {
		t.Fatalf("unexpected outcome %q", ev.Outcome)
	}
	if ev.Signature == "" {
		t.Fatalf("signature not populated")
	}
}

// TestAuditEndpoint_503WhenUnattached confirms the /api/v1/audit
// endpoint returns 503 adaptor_not_configured when no Auditor is
// bound. Mirrors the project's convention for unwired adaptors.
func TestAuditEndpoint_503WhenUnattached(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "adaptor_not_configured") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// TestAuditEndpoint_ListAndFilter covers the read API surface: events
// are returned, the type filter narrows results, and the timestamp
// filter parses RFC3339.
func TestAuditEndpoint_ListAndFilter(t *testing.T) {
	dir := t.TempDir()
	a, err := audit.New(audit.Options{
		Dir: dir,
		Key: []byte(strings.Repeat("k", 32)),
	})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a) })
	ctx := context.Background()
	for _, ev := range []audit.Event{
		{Type: audit.EventBeadCreate, Actor: "u", Action: "create"},
		{Type: audit.EventBeadUpdate, Actor: "u", Action: "update"},
		{Type: audit.EventBeadCreate, Actor: "u", Action: "create"},
	} {
		if err := a.Append(ctx, ev); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	h.AttachAuditor(a)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", w.Code, w.Body.String())
	}
	var env struct {
		Events []audit.Event `json:"events"`
		Total  int           `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 3 || len(env.Events) != 3 {
		t.Fatalf("expected 3 events, got %d (events=%d)", env.Total, len(env.Events))
	}

	// Filter by type.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?type="+audit.EventBeadCreate, nil)
	h.ServeHTTP(w, req)
	env = struct {
		Events []audit.Event `json:"events"`
		Total  int           `json:"total"`
	}{}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 2 {
		t.Fatalf("expected 2 bead.create events, got %d", env.Total)
	}

	// Bad since value → 400.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit?since=not-a-timestamp", nil)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad since, got %d", w.Code)
	}
}

// TestAuditTailEndpoint_StreamsSSE confirms the SSE endpoint emits a
// data frame for every event appended after subscription.
func TestAuditTailEndpoint_StreamsSSE(t *testing.T) {
	dir := t.TempDir()
	a, err := audit.New(audit.Options{Dir: dir, Key: []byte(strings.Repeat("z", 32))})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close(a) })

	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	h.AttachAuditor(a)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/audit/tail", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET tail: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	// Push an event after subscribing.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = a.Append(context.Background(), audit.Event{
			Type: audit.EventBeadCreate, Actor: "u", Action: "create",
		})
	}()

	buf := make([]byte, 4096)
	var accum bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				accum.Write(buf[:n])
				if strings.Contains(accum.String(), "event: audit") {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
		if !strings.Contains(accum.String(), "event: audit") {
			t.Fatalf("did not receive SSE audit frame; got %q", accum.String())
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatalf("timeout waiting for SSE audit frame; got %q", accum.String())
	}
}
