// gm-e3.6.1 end-to-end test: a SPA mutation's W3C traceparent flows
// through the WorkPlane / OrchestrationPlane call into the resulting
// GembaEvent on /events SSE.
//
// Verifies the property the bead's DoD calls out:
//
//   - Trace middleware extracts the inbound traceparent into the
//     handler's request context.
//   - publishWalkEvent stamps event.TraceID via events.WithTraceID
//     before publishing.
//   - The resulting event lands on /events SSE with the same trace_id
//     intact.
//   - The OTEL collector stub (a tracetest.SpanRecorder hooked into
//     the server-process TracerProvider) records the matching
//     server-kind span with the inbound trace_id, proving the
//     exporter pipeline is wired.

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/events"
	"github.com/GembaCore/gemba-core/internal/walk"
)

// installTestTracerProvider mirrors what initOTEL does at server boot
// but with an in-memory SpanRecorder standing in for the OTLP exporter.
// This is the "OTEL collector stub" the bead's DoD references — every
// span the server-process TracerProvider would have shipped lands in
// the recorder where the test can assert on it.
func installTestTracerProvider(t *testing.T) *tracetest.SpanRecorder {
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

// TestTraceEndToEnd_SPAMutation_to_SSE wires up the real Router with
// the walk surface, opens an SSE subscription on /events, fires a
// POST /api/v1/walks:start with a known traceparent, and asserts the
// resulting SSE frame carries the inbound trace_id.
func TestTraceEndToEnd_SPAMutation_to_SSE(t *testing.T) {
	rec := installTestTracerProvider(t)

	r := NewRouter(config.ServeConfig{Town: "trace-town"}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	r.AttachWalk(walk.NewMemoryStore(), walk.Sources{})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const traceparent = "00-" + traceID + "-00f067aa0ba902b7-01"

	// Subscribe to /events filtered to walk.started so the test ignores
	// the heartbeat-only frames the SSE handler may emit while the
	// mutation is in flight. Filtering on Kind keeps the assertion
	// narrow.
	sseCtx, sseCancel := context.WithCancel(context.Background())
	t.Cleanup(sseCancel)

	sseReq, err := http.NewRequestWithContext(sseCtx, http.MethodGet,
		srv.URL+"/events?topics=walk.started", nil)
	if err != nil {
		t.Fatalf("build sse request: %v", err)
	}
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("open sse: %v", err)
	}
	t.Cleanup(func() { _ = sseResp.Body.Close() })
	if sseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(sseResp.Body)
		t.Fatalf("sse status = %d body=%q", sseResp.StatusCode, body)
	}

	// Wait briefly so the SSE goroutine has Subscribe()'d on the hub
	// before we Publish via the mutation. Without this the hub's
	// snapshot-under-lock could miss our event. The /events handler
	// flushes its 200 OK before returning to its select loop, but the
	// Subscribe call happens just before that flush, so as soon as we
	// have the response headers we're guaranteed to be subscribed.

	// Capture frames in a goroutine so we don't race the publish.
	frames := make(chan map[string]any, 4)
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		reader := bufio.NewReader(sseResp.Body)
		var dataBuf bytes.Buffer
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if dataBuf.Len() == 0 {
					continue
				}
				var ev map[string]any
				if jerr := json.Unmarshal(dataBuf.Bytes(), &ev); jerr == nil {
					select {
					case frames <- ev:
					case <-sseCtx.Done():
						return
					}
				}
				dataBuf.Reset()
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
			}
		}
	}()

	// Fire the mutation with the known traceparent.
	mutationBody := strings.NewReader(`{"label":"trace-test"}`)
	mutationReq, err := http.NewRequest(http.MethodPost,
		srv.URL+"/api/v1/walks:start", mutationBody)
	if err != nil {
		t.Fatalf("build mutation request: %v", err)
	}
	mutationReq.Header.Set("Content-Type", "application/json")
	mutationReq.Header.Set("traceparent", traceparent)

	mutationResp, err := http.DefaultClient.Do(mutationReq)
	if err != nil {
		t.Fatalf("issue mutation: %v", err)
	}
	defer mutationResp.Body.Close()
	if mutationResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(mutationResp.Body)
		t.Fatalf("mutation status = %d body=%q", mutationResp.StatusCode, body)
	}

	// Wait for the SSE frame.
	var ev map[string]any
	select {
	case ev = <-frames:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for walk.started SSE frame")
	}

	gotTraceID, _ := ev["trace_id"].(string)
	if gotTraceID != traceID {
		t.Fatalf("SSE event trace_id = %q, want %q (full event=%v)",
			gotTraceID, traceID, ev)
	}
	if kind, _ := ev["kind"].(string); kind != string(kindWalkStarted) {
		t.Fatalf("SSE event kind = %q, want %q", kind, kindWalkStarted)
	}

	// Verify the OTEL collector stub captured the matching server span.
	// This proves the exporter pipeline is wired — the trace middleware
	// started a span on the extracted parent context, and the
	// TracerProvider's span processor (the recorder) saw it end.
	if err := waitForSpanWithTrace(rec, traceID, 2*time.Second); err != nil {
		t.Fatalf("collector stub: %v", err)
	}

	// Tidy: tear down the SSE reader so the test exits cleanly.
	sseCancel()
	_ = sseResp.Body.Close()
	readerWG.Wait()
}

// waitForSpanWithTrace polls the recorder for any ended span carrying
// traceID. The /events SSE span won't end until the test cancels the
// reader, but the mutation's POST span ends as soon as the handler
// returns — that's the one we wait on.
func waitForSpanWithTrace(rec *tracetest.SpanRecorder, traceID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, s := range rec.Ended() {
			if s.SpanContext().TraceID().String() == traceID {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("no span with trace_id=%s within %s", traceID, timeout)
}

// Confirm the helper signatures we depend on are visible — guards
// against accidental unexport during refactors. Compile-time check
// only; no runtime cost.
var _ = events.WithTraceID
