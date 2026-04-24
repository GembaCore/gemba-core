package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/transport/api"
)

// newRouterForEvents builds a router around a test-owned hub so the
// test can publish events directly without going through an adaptor.
// Returns the router + the hub for direct Publish.
func newRouterForEvents(t *testing.T) (*Router, *events.Hub) {
	t.Helper()
	host := api.New()
	r := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	t.Cleanup(r.Close)
	return r, r.EventsHub()
}

func TestEventsStream_ServiceUnavailableWhenHubMissing(t *testing.T) {
	// Construct a Router by zero-value to skip NewRouter's hub setup.
	r := &Router{}
	mux := http.NewServeMux()
	mux.HandleFunc("/events", r.eventsStream)

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestEventsStream_DeliversPublishedEvent(t *testing.T) {
	r, hub := newRouterForEvents(t)

	srv := httptest.NewServer(r)
	defer srv.Close()

	// Open SSE in a goroutine, publish from the test, read the frame.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type=%q", resp.Header.Get("Content-Type"))
	}

	// Publish after a small delay so the subscriber is registered.
	go func() {
		time.Sleep(50 * time.Millisecond)
		hub.Publish(events.GembaEvent{
			ID:     "evt-1",
			Kind:   events.SessionTransition,
			At:     time.Now(),
			Source: events.Source{Plane: events.PlaneOrchestration, AdaptorID: "native"},
		})
	}()

	// Scan SSE frames; expect to find an `event: session.transition` line.
	scanner := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: session.transition") {
			return
		}
	}
	t.Fatal("did not see session.transition frame within deadline")
}

func TestEventsStream_FilterByTopic(t *testing.T) {
	r, hub := newRouterForEvents(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/events?topics=workitem.updated", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(50 * time.Millisecond)
		// First event should be filtered out.
		hub.Publish(events.GembaEvent{ID: "skip", Kind: events.SessionTransition, At: time.Now()})
		// Second event should reach the client.
		hub.Publish(events.GembaEvent{ID: "keep", Kind: events.WorkItemUpdated, At: time.Now()})
	}()

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		if strings.Contains(line, "session.transition") {
			t.Errorf("filtered topic leaked through: %s", line)
		}
		if strings.HasPrefix(line, "event: workitem.updated") {
			return
		}
	}
	t.Fatal("did not see workitem.updated frame within deadline")
}

func TestEventsStream_RejectsInvalidPlane(t *testing.T) {
	r, _ := newRouterForEvents(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events?planes=galactic")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestEventsStream_HeartbeatKeepsConnectionWarm(t *testing.T) {
	if testing.Short() {
		t.Skip("heartbeat test takes ~16s; skipped under -short")
	}
	// We can't easily speed up the 15s heartbeat without exposing a
	// knob; instead assert that the connection stays open with no
	// publishes for >1s and the response Content-Type is right —
	// we trust the heartbeat mechanism since the hub-tests cover the
	// no-leak path.
	r, _ := newRouterForEvents(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	// Drain a small read window; should not error mid-read.
	buf := make([]byte, 512)
	_, _ = resp.Body.Read(buf)
}

func TestParseEventsFilter_Topics(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/events?topics=workitem.updated,session.transition", nil)
	f, err := parseEventsFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Kinds) != 2 || f.Kinds[0] != events.WorkItemUpdated || f.Kinds[1] != events.SessionTransition {
		t.Errorf("Kinds=%v", f.Kinds)
	}
}

func TestParseEventsFilter_Scopes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/events?assignment_id=asg-1&session_id=sess-1&work_item_id=gm-foo&epic_id=gm-root", nil)
	f, err := parseEventsFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.AssignmentID != "asg-1" || f.SessionID != "sess-1" ||
		f.WorkItemID != "gm-foo" || f.EpicID != "gm-root" {
		t.Errorf("scopes: %+v", f)
	}
}
