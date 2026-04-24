package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/config"
)

// gm-root.7: subscribing to /api/adaptors/stream must yield the
// current status as the first frame so a reconnecting SPA renders
// without waiting for the next probe tick.
func TestAdaptorsStream_FirstFrameIsCurrentSnapshot(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)
	registry.Register(registry.Adaptor{
		Name:  "beads",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult {
			return registry.DetectResult{Ok: false, Reason: "Dolt unreachable"}
		},
	})

	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(h.Close)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/adaptors/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("want text/event-stream, got %q", ct)
	}

	buf := make([]byte, 4096)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := resp.Body.Read(buf)
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		if r.err != nil && r.n == 0 {
			t.Fatalf("read first frame: %v", r.err)
		}
		frame := string(buf[:r.n])
		if !strings.HasPrefix(frame, "data: ") {
			t.Fatalf("first frame should begin with `data: `, got %q", frame)
		}
		// Extract the JSON between "data: " and the trailing \n\n.
		payload := strings.TrimPrefix(frame, "data: ")
		payload = strings.SplitN(payload, "\n\n", 2)[0]
		var env struct {
			Adaptors []registry.AdaptorStatus `json:"adaptors"`
		}
		if err := json.Unmarshal([]byte(payload), &env); err != nil {
			t.Fatalf("first frame must be JSON: %v; payload=%q", err, payload)
		}
		if len(env.Adaptors) != 1 || env.Adaptors[0].Name != "beads" {
			t.Fatalf("unexpected snapshot: %+v", env.Adaptors)
		}
		if env.Adaptors[0].Healthy {
			t.Fatalf("beads should have been probed as unhealthy")
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for first SSE frame")
	}
}

// gm-root.7: the snapshot endpoint (/api/adaptors) reads from the
// HealthBus cache once Start has been called, so repeated hits don't
// re-probe adaptors.
func TestAdaptorsEndpoint_ReadsFromCacheAfterStart(t *testing.T) {
	registry.Reset()
	t.Cleanup(registry.Reset)

	calls := 0
	registry.Register(registry.Adaptor{
		Name:  "beads",
		Plane: registry.WorkPlane,
		Probe: func() registry.DetectResult {
			calls++
			return registry.DetectResult{Ok: true}
		},
	})

	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(h.Close)
	// Prime the cache without starting the ticker so call-count
	// assertions are stable.
	h.healthBus.Snapshot()
	startCalls := calls

	// Three GETs; if each triggered a live probe, calls would grow.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/adaptors", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
	}
	if calls != startCalls {
		t.Fatalf("snapshot endpoint must read from cache; probe ran %d extra times",
			calls-startCalls)
	}
}
