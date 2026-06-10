// gm-e3.6.1: trace propagation helpers. The GembaEvent envelope already
// carries a TraceID field (gm-e3.6); this file is the seam adaptors and
// HTTP handlers use to stamp the W3C trace_id off the call context onto
// the event at emission time. Keeping the helper here (vs. inside each
// handler) means a single place to update if/when we switch from W3C
// trace-context to a different propagation header.

package events

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// TraceIDFromContext returns the W3C trace_id (32 hex chars) of the
// span attached to ctx, or "" when no span context is present or the
// span context is not valid. The returned string is the bare trace_id —
// not the full traceparent — because that's what GembaEvent.TraceID
// stores. Subscribers that need the full propagation header re-build it
// from trace_id + span_id when they re-export.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// WithTraceID returns ev with TraceID populated from ctx's span
// context. When ev.TraceID is already set, it's left untouched — the
// caller wins, which lets adaptors that already know the trace_id
// (e.g. they read it off an upstream signal) override the
// context-derived value. When ctx carries no valid span context, the
// event is returned unchanged.
//
// Usage at a publish site:
//
//	ev := events.GembaEvent{...}
//	ev = events.WithTraceID(ctx, ev)
//	hub.Publish(ev)
func WithTraceID(ctx context.Context, ev GembaEvent) GembaEvent {
	if ev.TraceID != "" {
		return ev
	}
	id := TraceIDFromContext(ctx)
	if id == "" {
		return ev
	}
	ev.TraceID = id
	return ev
}
