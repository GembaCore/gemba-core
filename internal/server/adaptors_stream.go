package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
)

// sseHeartbeatInterval bounds the silence between bus events so
// reverse proxies with idle timeouts (nginx default 60s,
// Cloudflare 100s) don't drop the stream. A heartbeat is a comment
// line (`:keepalive\n\n`) — the EventSource parser ignores it but
// the bytes keep the TCP connection warm.
const sseHeartbeatInterval = 25 * time.Second

// adaptorsStream is the SSE endpoint that fans out AdaptorStatus
// transitions from the registry HealthBus (gm-root.7). First frame on
// subscribe is the current snapshot so a reconnecting client renders
// immediately; subsequent frames fire only when the bus detects a
// status transition.
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

	// Ensure the bus ticker is running before we subscribe. Under the
	// production boot path StartHealthBus has already been called by
	// cmd/gemba serve — this is a safety net for ad-hoc test harnesses
	// that wire the router without calling Start themselves.
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
			if err := writeAdaptorsFrame(w, frame); err != nil {
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
// share its response type across both transports.
func writeAdaptorsFrame(w http.ResponseWriter, adaptors []registry.AdaptorStatus) error {
	body, err := json.Marshal(map[string]any{"adaptors": adaptors})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}
