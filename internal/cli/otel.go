// gm-e3.6.1: OpenTelemetry exporter wiring for `gemba serve`.
//
// Convention: env-var-driven, with the OTLP HTTP exporter as the
// default protocol. We pick HTTP (not gRPC) because it has fewer
// dependencies, plays nicely with reverse proxies, and matches the
// most common collector deployment pattern (otelcol-contrib running
// on localhost:4318). The full set of OTEL_EXPORTER_OTLP_* env vars
// the upstream library understands is honored automatically — the
// exporter constructor reads them itself; we just trigger init when
// the endpoint is set.
//
// Behavior:
//
//   - OTEL_EXPORTER_OTLP_ENDPOINT unset → no-op. The trace middleware
//     still produces valid SpanContexts (the global no-op
//     TracerProvider hands out random trace ids), but nothing exports
//     them. GembaEvents stamped via events.WithTraceID will carry the
//     fresh trace_id, which is the property the e2e test asserts;
//     local dev stays clean (no socket attempts, no warning logs).
//
//   - OTEL_EXPORTER_OTLP_ENDPOINT set → init an OTLP HTTP exporter,
//     register a batching TracerProvider as the global, and register
//     the W3C trace-context propagator. The returned shutdown fn must
//     run on server exit so in-flight spans flush.
//
// The propagator is registered unconditionally — even with no
// exporter, the middleware needs propagation.TraceContext{} to extract
// incoming traceparent headers. The default propagator is a no-op that
// drops everything, which is what causes the "trace_id silently
// disappears" anti-pattern. Calling SetTextMapPropagator at boot is
// effectively the seam this whole bead exists to install.

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// otelEndpointEnv is the standard upstream env var. Listing it here
// (rather than reading os.Getenv inline) makes the surface easy to
// grep for and, in the future, easy to override with a flag.
const otelEndpointEnv = "OTEL_EXPORTER_OTLP_ENDPOINT"

// otelShutdown is the function the caller invokes on server exit to
// flush pending spans. When the exporter is not configured, returns a
// no-op so call sites can `defer shutdown(ctx)` unconditionally.
type otelShutdown func(context.Context) error

// initOTEL bootstraps tracing. Always registers the W3C
// trace-context propagator. Optionally configures an OTLP HTTP
// exporter when OTEL_EXPORTER_OTLP_ENDPOINT is set. Returns a
// shutdown function the caller must invoke on exit.
func initOTEL(ctx context.Context, b BuildInfo) (otelShutdown, error) {
	// Propagator first — required for incoming traceparent extraction
	// regardless of whether we export anything outbound. See file
	// header for why this matters.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	endpoint := os.Getenv(otelEndpointEnv)
	if endpoint == "" {
		// No exporter configured — return a no-op shutdown so callers
		// can defer it unconditionally.
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("init otlp http exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("gemba"),
			semconv.ServiceVersion(b.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	slog.Info("otel: tracing exporter configured",
		"endpoint", endpoint, "protocol", "otlp/http")

	return func(shutdownCtx context.Context) error {
		// 5s budget keeps a borked collector from blocking server
		// shutdown indefinitely. Spans pending past the budget are
		// dropped, which is the right tradeoff for a dev-targeted
		// exporter.
		ctx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}
