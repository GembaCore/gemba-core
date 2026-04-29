// metrics_query_test.go covers the time-series Prometheus proxy
// (gm-e9m0). The happy paths and every error class hit a httptest
// fake Prometheus so the suite has zero dependency on a real
// upstream. The single round-trip integration test is gated on
// PROM_URL being set in the environment — see
// TestMetricsSeries_Integration_Live.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/config"
)

// promSuccessBody builds a Prometheus query_range success response
// envelope around the given samples. Each sample is (unix_seconds, v).
func promSuccessBody(samples ...[2]float64) string {
	values := make([]string, 0, len(samples))
	for _, s := range samples {
		values = append(values, fmt.Sprintf("[%v, %q]", s[0], fmt.Sprintf("%g", s[1])))
	}
	return `{"status":"success","data":{"resultType":"matrix","result":[` +
		`{"metric":{},"values":[` + strings.Join(values, ",") + `]}` +
		`]}}`
}

func newRouterWithProm(prom *httptest.Server) *Router {
	return NewRouter(
		config.ServeConfig{PromURL: prom.URL},
		fakeSPA(),
		nil,
	)
}

func TestMetricsSeries_HappyPath_Counter(t *testing.T) {
	body := promSuccessBody(
		[2]float64{1713139200, 0},
		[2]float64{1713142800, 3},
	)
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("want /api/v1/query_range, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("query"); !strings.Contains(got, "rate(gemba_session_spawn_total") {
			t.Errorf("expected rate() PromQL for counter; query=%s", got)
		}
		if got := r.URL.Query().Get("step"); got != "1h" {
			t.Errorf("step: want 1h, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer prom.Close()

	h := newRouterWithProm(prom)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
	var resp metricsSeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if resp.Metric != "session_spawn_total" {
		t.Errorf("metric: %q", resp.Metric)
	}
	if resp.Bucket != "1h" {
		t.Errorf("bucket: %q", resp.Bucket)
	}
	if len(resp.Samples) != 2 {
		t.Fatalf("samples: want 2, got %d (%+v)", len(resp.Samples), resp.Samples)
	}
	if resp.Samples[1].V != 3 {
		t.Errorf("second sample value: want 3, got %v", resp.Samples[1].V)
	}
}

func TestMetricsSeries_HappyPath_Gauge_WithLabels(t *testing.T) {
	body := promSuccessBody([2]float64{1713139200, 5})
	var capturedQuery string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer prom.Close()

	h := newRouterWithProm(prom)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=escalation_open_count"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h"+
			"&labels=source_kind:bd",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(capturedQuery, `gemba_escalation_open_count{source_kind="bd"}`) {
		t.Errorf("query: %q (want gauge expression with source_kind matcher)", capturedQuery)
	}
	var resp metricsSeriesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Labels["source_kind"] != "bd" {
		t.Errorf("labels echo: %+v", resp.Labels)
	}
}

func TestMetricsSeries_HappyPath_Histogram(t *testing.T) {
	body := promSuccessBody([2]float64{1713139200, 0.05})
	var capturedQuery string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer prom.Close()

	h := newRouterWithProm(prom)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=workspace_acquire_secs"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=5m",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
	// Histogram → rate(_sum)/rate(_count) translation.
	if !strings.Contains(capturedQuery, "_sum") || !strings.Contains(capturedQuery, "_count") {
		t.Errorf("expected histogram rate-of-sum/rate-of-count translation; got %q", capturedQuery)
	}
}

func TestMetricsSeries_AllFiveMetricsCovered(t *testing.T) {
	// Keep the suite cheap — single fake Prom for every name.
	body := promSuccessBody([2]float64{1713139200, 1})
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer prom.Close()
	h := newRouterWithProm(prom)

	for _, m := range []string{
		"workitem_state_duration",
		"assignment_cost",
		"escalation_open_count",
		"session_spawn_total",
		"workspace_acquire_secs",
	} {
		t.Run(m, func(t *testing.T) {
			u := "/api/v1/metrics/series?metric=" + m +
				"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h"
			req := httptest.NewRequest(http.MethodGet, u, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("metric=%s want 200, got %d; body=%s", m, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMetricsSeries_Validation_MissingMetric(t *testing.T) {
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"validation"`) {
		t.Errorf("envelope: %s", rec.Body.String())
	}
}

func TestMetricsSeries_Validation_UnknownMetric(t *testing.T) {
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=bogus_metric"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown metric") {
		t.Errorf("expected unknown-metric message; got %s", rec.Body.String())
	}
}

func TestMetricsSeries_Validation_BadTime(t *testing.T) {
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=yesterday&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestMetricsSeries_Validation_InvertedRange(t *testing.T) {
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-29T00:00:00Z&until=2026-04-15T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestMetricsSeries_Validation_BadBucket(t *testing.T) {
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=fortnight",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestMetricsSeries_Validation_UnknownLabel(t *testing.T) {
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h"+
			"&labels=evil:label",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown label, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown label key") {
		t.Errorf("body: %s", rec.Body.String())
	}
}

func TestMetricsSeries_Validation_LabelOnNoLabelMetric(t *testing.T) {
	// session_spawn_total has no labels — any label is unknown.
	h := NewRouter(config.ServeConfig{PromURL: "http://example.invalid"}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h"+
			"&labels=adaptor:bd",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestMetricsSeries_Validation_AllowedLabel(t *testing.T) {
	// assignment_cost permits adaptor + assignment.
	body := promSuccessBody([2]float64{1713139200, 1.5})
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer prom.Close()

	h := newRouterWithProm(prom)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=assignment_cost"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h"+
			"&labels=adaptor:bd,assignment:plan",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsSeries_AdaptorDegraded_PromUnconfigured(t *testing.T) {
	// PromURL empty → 503 with KindAdaptorDegraded.
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Prometheus is not configured") {
		t.Errorf("body: %s", rec.Body.String())
	}
}

func TestMetricsSeries_AdaptorDegraded_Prom5xx(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream broken"))
	}))
	defer prom.Close()
	h := newRouterWithProm(prom)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 for 5xx upstream, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"adaptor_degraded"`) {
		t.Errorf("envelope: %s", rec.Body.String())
	}
}

func TestMetricsSeries_AdaptorDegraded_PromMalformedJSON(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer prom.Close()
	h := newRouterWithProm(prom)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 for malformed JSON, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "malformed JSON") {
		t.Errorf("body: %s", rec.Body.String())
	}
}

func TestMetricsSeries_AdaptorDegraded_PromUnreachable(t *testing.T) {
	// Closed server → connection refused.
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	prom.Close()
	h := newRouterWithProm(prom)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 for unreachable upstream, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsSeries_AdaptorDegraded_PromQueryError(t *testing.T) {
	// Prometheus 4xx with status:error envelope (e.g. bad query).
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	}))
	defer prom.Close()
	h := newRouterWithProm(prom)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since=2026-04-15T00:00:00Z&until=2026-04-29T00:00:00Z&bucket=1h",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 for prom query error, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bad_data") {
		t.Errorf("expected to surface upstream errorType; got %s", rec.Body.String())
	}
}

// TestMetricsSeries_Integration_Live exercises the proxy against a
// real Prometheus instance when PROM_URL is set in the environment.
// Skipped (with a clear message) when unset, so CI without a TSDB
// dependency stays green.
func TestMetricsSeries_Integration_Live(t *testing.T) {
	upstream := os.Getenv("PROM_URL")
	if upstream == "" {
		t.Skip("PROM_URL not set; skipping live-Prometheus integration test")
	}
	h := NewRouter(config.ServeConfig{PromURL: upstream}, fakeSPA(), nil)

	// 1h window ending now — even on a freshly-started Prometheus
	// the request should at least round-trip cleanly.
	now := time.Now().UTC().Truncate(time.Hour)
	since := now.Add(-1 * time.Hour).Format(time.RFC3339)
	until := now.Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/series?metric=session_spawn_total"+
			"&since="+since+"&until="+until+"&bucket=5m",
		nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("live PROM_URL=%s returned %d; body=%s", upstream, rec.Code, rec.Body.String())
	}
	var resp metricsSeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Metric != "session_spawn_total" {
		t.Fatalf("metric echo: %q", resp.Metric)
	}
}
