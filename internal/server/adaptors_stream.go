package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/registry"
)

// sseHeartbeatInterval bounds the silence between bus events so
// reverse proxies with idle timeouts (nginx default 60s,
// Cloudflare 100s) don't drop the stream. A heartbeat is a comment
// line (`:keepalive\n\n`) — the EventSource parser ignores it but
// the bytes keep the TCP connection warm.
const sseHeartbeatInterval = 25 * time.Second

// adaptorsStream is the SSE endpoint that fans out explicit
// AdaptorStatus refresh transitions from the registry HealthBus
// (gm-root.7). First frame on subscribe is the current snapshot so a
// reconnecting client renders immediately; subsequent frames fire only
// when another request or operator health action refreshes the bus.
//
// Wire contract per SSE: Content-Type text/event-stream, no caching,
// chunked transfer. Each frame is a JSON object matching the shape
// served by /api/adaptors:
//
//	{
//	  "adaptors": [
//	    {"name": "beads", "plane": "work", "healthy": true},
//	    ...
//	  ]
//	}
//
// On disconnect (client close, server shutdown) the subscription is
// cancelled and the goroutine returns.
func (r *Router) adaptorsStream(w http.ResponseWriter, req *http.Request) {
	if r.healthBus == nil {
		// Zero-value router bypass; tests should use NewRouter.
		http.Error(w, "health bus not configured", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Ensure the bus is initialized before we subscribe. With the
	// production zero interval, Start is a no-op; Subscribe/Snapshot
	// still gives the stream an initial frame.
	r.healthBus.Start()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := r.healthBus.Subscribe()
	defer cancel()

	ctx := req.Context()
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if err := writeAdaptorsFrame(w, r.instanceID, frame); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			// SSE comment line; parsed by EventSource as "ignore".
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeAdaptorsFrame emits a single SSE `data:` frame encoding the
// same envelope /api/adaptors serves synchronously — the SPA can
// share its response type across both transports. instance_id is the
// per-process boot stamp the SPA uses as a server-restart sentinel
// (gm-6m60); it appears on every frame so a reconnecting client
// catches the new id without waiting for a /api/capabilities refetch.
func writeAdaptorsFrame(w http.ResponseWriter, instanceID string, adaptors []registry.AdaptorStatus) error {
	body, err := json.Marshal(map[string]any{
		"instance_id": instanceID,
		"adaptors":    adaptors,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}
