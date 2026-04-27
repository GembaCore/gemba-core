package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// withTestTracerProvider installs a fresh in-memory TracerProvider +
// TextMapPropagator for the duration of one test, restoring globals
// on cleanup so parallel test packages don't see leaked state.
func withTestTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prevTP := otel.GetTracerProvider()
	prevPR := otel.GetTextMapPropagator()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevPR)
	})
	return rec
}

func TestTrace_ExtractsTraceparent(t *testing.T) {
	withTestTracerProvider(t)

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const traceparent = "00-" + traceID + "-00f067aa0ba902b7-01"

	var observed string
	h := Trace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := trace.SpanContextFromContext(r.Context())
		if sc.IsValid() {
			observed = sc.TraceID().String()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.Header.Set("traceparent", traceparent)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if observed != traceID {
		t.Fatalf("trace id in handler ctx = %q, want %q", observed, traceID)
	}
}

func TestTrace_StartsFreshTraceWhenAbsent(t *testing.T) {
	withTestTracerProvider(t)

	var observed string
	h := Trace()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := trace.SpanContextFromContext(r.Context())
		if sc.IsValid() {
			observed = sc.TraceID().String()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if observed == "" {
		t.Fatalf("expected fresh trace id when no traceparent present")
	}
	if len(observed) != 32 {
		t.Fatalf("expected 32-char trace id, got %q", observed)
	}
}
