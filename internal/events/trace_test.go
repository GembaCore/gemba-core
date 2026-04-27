package events

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceIDFromContext_NoSpan(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty trace id, got %q", got)
	}
}

func TestTraceIDFromContext_NilCtx(t *testing.T) {
	//nolint:staticcheck // exercising the nil guard path
	if got := TraceIDFromContext(nil); got != "" {
		t.Fatalf("expected empty trace id from nil ctx, got %q", got)
	}
}

func TestWithTraceID_StampsFromContext(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("trace id parse: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("span id parse: %v", err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	ev := GembaEvent{ID: "x"}
	out := WithTraceID(ctx, ev)
	if out.TraceID != traceID.String() {
		t.Fatalf("trace id = %q, want %q", out.TraceID, traceID.String())
	}
}

func TestWithTraceID_PreservesExisting(t *testing.T) {
	ev := GembaEvent{ID: "x", TraceID: "preset"}
	out := WithTraceID(context.Background(), ev)
	if out.TraceID != "preset" {
		t.Fatalf("expected preset preserved, got %q", out.TraceID)
	}
}

func TestWithTraceID_NoSpanLeavesEmpty(t *testing.T) {
	ev := GembaEvent{ID: "x"}
	out := WithTraceID(context.Background(), ev)
	if out.TraceID != "" {
		t.Fatalf("expected empty trace id, got %q", out.TraceID)
	}
}
