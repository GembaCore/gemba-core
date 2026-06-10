// /events SSE endpoint (gm-e4.3). One stream per client; per-client
// Filter parsed from query params:
//
//	?topics=workitem.created,session.transition  → Filter.Kinds
//	?planes=workplane,orchestration              → Filter.Planes
//	?assignment_id=...                           → Filter.AssignmentID
//	?session_id=...                              → Filter.SessionID
//	?work_item_id=...                            → Filter.WorkItemID
//	?epic_id=...                                 → Filter.EpicID
//
// Heartbeats fire every 15s (per gm-e4.3 DoD). Last-Event-ID isn't
// honored yet because the hub doesn't replay history — a reconnecting
// client picks up the stream from the moment of reconnect. Adding
// replay is a follow-up that needs a bounded ring buffer in the hub.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/events"
)

// eventsHeartbeatInterval is the cadence between SSE keepalive lines
// when the hub is otherwise quiet. 15s is the gm-e4.3 spec; the
// adaptors-stream endpoint uses 25s to match nginx's idle defaults,
// but the events stream's spec is tighter so client reconnect
// detection is faster.
const eventsHeartbeatInterval = 15 * time.Second

// eventsStream handles GET /events. The router wires this in front of
// the events.Hub the api.Host owns; if no hub is configured (degraded
// mode) the endpoint returns 503 so the SPA's reconnect logic kicks
// in instead of blocking forever.
func (r *Router) eventsStream(w http.ResponseWriter, req *http.Request) {
	if r.eventsHub == nil {
		http.Error(w, "events hub not configured", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	filter, err := parseEventsFilter(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_filter",
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	ch := r.eventsHub.Subscribe(ctx, filter)

	heartbeat := time.NewTicker(eventsHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				// Hub dropped us — slow client or hub close. Letting
				// the handler return tears the SSE down so the SPA's
				// EventSource fires `error` and reconnects.
				return
			}
			if err := writeEventsFrame(w, ev); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeEventsFrame emits a single SSE `data:` frame with the
// GembaEvent JSON. The SSE `event:` field carries the canonical Kind
// so EventSource consumers can register typed listeners
// (es.addEventListener('session.transition', …)) without parsing the
// body.
func writeEventsFrame(w http.ResponseWriter, ev events.GembaEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Kind, body); err != nil {
		return err
	}
	return nil
}

// parseEventsFilter extracts an events.Filter from the request query
// string. Returns a non-nil error only on a malformed filter; an
// entirely empty query yields the zero Filter (matches everything).
func parseEventsFilter(req *http.Request) (events.Filter, error) {
	q := req.URL.Query()
	f := events.Filter{}
	if v := q.Get("topics"); v != "" {
		for _, t := range strings.Split(v, ",") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			f.Kinds = append(f.Kinds, events.Kind(t))
		}
	}
	if v := q.Get("planes"); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			switch events.Plane(p) {
			case events.PlaneWorkPlane, events.PlaneOrchestration:
				f.Planes = append(f.Planes, events.Plane(p))
			default:
				return events.Filter{}, fmt.Errorf("unknown plane %q (want workplane | orchestration)", p)
			}
		}
	}
	f.AssignmentID = q.Get("assignment_id")
	f.SessionID = q.Get("session_id")
	f.WorkItemID = q.Get("work_item_id")
	f.EpicID = q.Get("epic_id")
	return f, nil
}
