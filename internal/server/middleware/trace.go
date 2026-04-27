// Package middleware holds reusable HTTP middleware that the server
// router composes. Today this package is small — most middleware lives
// either in chi's own middleware package (RequestID, RealIP, Logger,
// Recoverer, Timeout) or as router methods next to the route they
// guard (nonce.go's requireConfirmNonce). The standalone package
// exists so cross-cutting concerns that don't need access to *Router
// state — like trace extraction — sit somewhere obvious without
// bloating router.go.
//
// gm-e3.6.1: trace extraction middleware. Reads W3C traceparent (and
// tracestate) off incoming requests and attaches the resulting span
// context to the request's context.Context, so downstream handlers
// that publish GembaEvents can stamp event.TraceID via
// events.WithTraceID. When no traceparent is present, a fresh span is
// started so dev / SPA-side calls without an upstream tracer still
// produce a stable trace_id end-to-end.

package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation library name attached to spans the
// server itself starts (i.e. when the request had no upstream
// traceparent). Per OTEL convention it identifies the package that
// emits the span, not the service.
const tracerName = "github.com/MikeBengtson/gemba/internal/server"

// Tracer returns the package's named tracer. Handlers that want to
// open child spans share this so all server-emitted spans land under
// one instrumentation library, easy to filter at the collector.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Trace returns an HTTP middleware that:
//
//  1. Extracts a SpanContext from incoming W3C traceparent / tracestate
//     headers via the globally-registered TextMapPropagator (server
//     bootstrap registers propagation.TraceContext{}). When the
//     headers are absent or malformed, the propagator returns the
//     incoming context untouched.
//  2. Starts a server-kind span named "<METHOD> <URL.Path>" rooted at
//     the extracted context (or, when nothing was extracted, a fresh
//     trace). The span is attached to the request's context so
//     downstream code can read the trace_id via
//     trace.SpanContextFromContext or events.TraceIDFromContext.
//  3. Ends the span when the handler returns.
//
// The middleware is a no-op-friendly wrapper: it works whether or not
// a TracerProvider is registered. With the global no-op provider
// (default when OTEL_EXPORTER_OTLP_ENDPOINT is unset) the spans still
// carry valid trace ids, but nothing exports them — so adaptor events
// still get a stamp and operators with no collector see no overhead.
func Trace() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(req.Context(),
				propagationHeaderCarrier(req.Header))
			ctx, span := Tracer().Start(ctx,
				req.Method+" "+req.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// propagationHeaderCarrier adapts an http.Header to the
// propagation.TextMapCarrier interface without pulling in the propagation
// package's HeaderCarrier type at the call site. Keeping the adapter
// here means the rest of the package only deals with stdlib + otel
// types.
type propagationHeaderCarrier http.Header

func (c propagationHeaderCarrier) Get(key string) string {
	return http.Header(c).Get(key)
}

func (c propagationHeaderCarrier) Set(key, value string) {
	http.Header(c).Set(key, value)
}

func (c propagationHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
