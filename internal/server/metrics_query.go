// metrics_query.go implements the time-series query proxy at
// GET /api/v1/metrics/series (gm-e9m0). The endpoint is the read side
// of the Insights panel (gm-e12.17 / gm-e12.17.1) and exists because
// the existing /metrics surface is point-in-time only — useless for
// "render me 14 days of counter values."
//
// Per D4 (gm-sf51, ratified 2026-04-29) the chosen storage path is
// "Path A — Prometheus proxy": Gemba assumes the operator runs a
// Prometheus instance scraping our /metrics endpoint, and translates
// /api/v1/metrics/series requests into Prometheus
// /api/v1/query_range calls. No new storage code lands inside Gemba.
//
// Wire shape (locked early — frontend mocks against this):
//
//	GET /api/v1/metrics/series
//	  ?metric=session_spawn_total
//	  &since=2026-04-15T00:00:00Z
//	  &until=2026-04-29T00:00:00Z
//	  &bucket=1h
//	  &labels=adaptor:bd,assignment:plan
//
// Response:
//
//	{
//	  "metric": "session_spawn_total",
//	  "labels": {"adaptor": "bd", "assignment": "plan"},
//	  "samples": [
//	    {"t": "2026-04-15T00:00:00Z", "v": 0},
//	    ...
//	  ],
//	  "bucket": "1h"
//	}
//
// # Cardinality cap
//
// Labels are allowlisted per metric — the request cannot pass a label
// key the underlying collector does not emit. Unknown keys fail 400
// (KindValidation) before any Prometheus call goes out. The fixed
// allowlist matches what internal/server/metrics/collector.go emits
// today; expand metricSpecs below when the collector does.
//
// # Errors
//
// All error paths return the typed core.AdaptorError envelope so the
// SPA's existing httperr chain can render them:
//
//	400 KindValidation       — bad metric name, malformed time, unknown label
//	502 KindAdaptorDegraded  — Prometheus returned malformed JSON
//	503 KindAdaptorDegraded  — Prometheus unreachable / 5xx upstream / not configured

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// metricSpec describes one user-facing metric: which Prometheus query
// the proxy issues, which labels we accept on the request, and what
// kind of post-processing the response needs.
//
// kind values:
//
//	"counter"   — translate to rate(<promName>[<bucket>])
//	"gauge"     — translate to <promName>{...} (point-in-time)
//	"histogram" — translate to rate(<promName>_sum[<bucket>])
//	              / rate(<promName>_count[<bucket>]), giving an average
//	              latency per bucket. Histograms are tricky over a wire
//	              like this; the average gets the SPA an actionable
//	              chart without piping every bucket boundary across.
type metricSpec struct {
	// kind classifies how to build the PromQL expression.
	kind string
	// promName is the Prometheus metric name (post-namespace/subsystem
	// prefixing applied by the collector).
	promName string
	// labels is the allowlist of label keys the request may pass via
	// ?labels=. An empty slice means "metric has no labels."
	labels []string
}

// metricSpecs is the public allowlist. Keys are the user-facing metric
// names the bead spec names. Values are the wire shape we'd normally
// hide from the operator. Add a row when the collector grows a new
// metric — keep keys snake_case, lowercased.
var metricSpecs = map[string]metricSpec{
	"workitem_state_duration": {
		kind:     "histogram",
		promName: "gemba_workitem_state_duration_seconds",
		labels:   []string{"adaptor", "state_category"},
	},
	"assignment_cost": {
		kind:     "counter",
		promName: "gemba_assignment_total_cost_dollars",
		// NOTE: the bead description says "axis" but the collector
		// emits "assignment" today. Frontend (gm-e12.17.1) should mock
		// against "assignment" until/unless the collector grows an
		// axis label. Documented loudly in the gm-e9m0 commit message.
		labels: []string{"adaptor", "assignment"},
	},
	"escalation_open_count": {
		kind:     "gauge",
		promName: "gemba_escalation_open_count",
		labels:   []string{"source_kind"},
	},
	"session_spawn_total": {
		kind:     "counter",
		promName: "gemba_session_spawn_total",
		labels:   nil,
	},
	"workspace_acquire_secs": {
		kind:     "histogram",
		promName: "gemba_workspace_acquire_latency_seconds",
		labels:   nil,
	},
}

// promQueryRangeResponse is the subset of Prometheus's
// /api/v1/query_range response we read. Schema reference:
// https://prometheus.io/docs/prometheus/latest/querying/api/#range-queries
type promQueryRangeResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			// Each Value is a [unix_timestamp_seconds, "string_value"] pair.
			Values [][2]any `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// metricsSeriesResponse is the wire shape we return to the SPA.
type metricsSeriesResponse struct {
	Metric  string            `json:"metric"`
	Labels  map[string]string `json:"labels"`
	Samples []sample          `json:"samples"`
	Bucket  string            `json:"bucket"`
}

type sample struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// metricsSeries is the chi handler for GET /api/v1/metrics/series.
// Validates the request, translates it into a Prometheus query_range
// call, and returns the parsed series in the documented shape. All
// error paths route through httperr.WriteError so the SPA's existing
// envelope handling renders them without per-handler branching.
func (r *Router) metricsSeries(w http.ResponseWriter, req *http.Request) {
	q, err := parseMetricsSeriesQuery(req.URL.Query())
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	if r.cfg.PromURL == "" {
		httperr.WriteError(w, core.NewAdaptorError(core.KindAdaptorDegraded,
			"upstream Prometheus is not configured; "+
				"set PROM_URL in the environment or [metrics].prom_url in ~/.gemba/config.toml"))
		return
	}

	client := r.metricsHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := queryPrometheusRange(req.Context(), client, r.cfg.PromURL, q)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// metricsSeriesQuery is the parsed-and-validated form of the request.
type metricsSeriesQuery struct {
	metric string
	spec   metricSpec
	since  time.Time
	until  time.Time
	bucket time.Duration
	// bucketRaw is the original ?bucket= string, echoed back in the
	// response so the SPA can keep its axis labels stable across
	// requests where it has rounded.
	bucketRaw string
	// labels are the validated label key→value pairs (allowlist
	// already enforced by parseMetricsSeriesQuery).
	labels map[string]string
}

// parseMetricsSeriesQuery validates the URL query string and returns
// the structured form. Every error returned is a *core.AdaptorError
// of KindValidation so the caller can hand it straight to
// httperr.WriteError and get a 400 envelope.
func parseMetricsSeriesQuery(values url.Values) (metricsSeriesQuery, error) {
	var q metricsSeriesQuery

	q.metric = values.Get("metric")
	if q.metric == "" {
		return q, core.NewAdaptorError(core.KindValidation,
			"missing required query parameter 'metric'")
	}
	spec, ok := metricSpecs[q.metric]
	if !ok {
		return q, core.NewAdaptorError(core.KindValidation,
			"unknown metric %q; supported: %s",
			q.metric, supportedMetricNames())
	}
	q.spec = spec

	since := values.Get("since")
	if since == "" {
		return q, core.NewAdaptorError(core.KindValidation,
			"missing required query parameter 'since' (RFC3339)")
	}
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return q, core.NewAdaptorError(core.KindValidation,
			"invalid 'since' %q: must be RFC3339 (e.g. 2026-04-15T00:00:00Z)", since)
	}
	q.since = t

	until := values.Get("until")
	if until == "" {
		return q, core.NewAdaptorError(core.KindValidation,
			"missing required query parameter 'until' (RFC3339)")
	}
	t, err = time.Parse(time.RFC3339, until)
	if err != nil {
		return q, core.NewAdaptorError(core.KindValidation,
			"invalid 'until' %q: must be RFC3339 (e.g. 2026-04-29T00:00:00Z)", until)
	}
	q.until = t
	if !q.until.After(q.since) {
		return q, core.NewAdaptorError(core.KindValidation,
			"'until' (%s) must be after 'since' (%s)",
			q.until.Format(time.RFC3339), q.since.Format(time.RFC3339))
	}

	bucket := values.Get("bucket")
	if bucket == "" {
		return q, core.NewAdaptorError(core.KindValidation,
			"missing required query parameter 'bucket' (e.g. 1h, 30m, 5m)")
	}
	d, err := time.ParseDuration(bucket)
	if err != nil || d <= 0 {
		return q, core.NewAdaptorError(core.KindValidation,
			"invalid 'bucket' %q: must be a Go duration (1h, 30m, 5m)", bucket)
	}
	q.bucket = d
	q.bucketRaw = bucket

	q.labels = map[string]string{}
	if raw := values.Get("labels"); raw != "" {
		allowed := map[string]struct{}{}
		for _, k := range spec.labels {
			allowed[k] = struct{}{}
		}
		for _, pair := range strings.Split(raw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			k, v, ok := strings.Cut(pair, ":")
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if !ok || k == "" || v == "" {
				return q, core.NewAdaptorError(core.KindValidation,
					"invalid 'labels' entry %q: expected key:value", pair)
			}
			if _, allow := allowed[k]; !allow {
				return q, core.NewAdaptorError(core.KindValidation,
					"unknown label key %q for metric %q; allowed: %s",
					k, q.metric, strings.Join(spec.labels, ","))
			}
			q.labels[k] = v
		}
	}

	return q, nil
}

func supportedMetricNames() string {
	names := make([]string, 0, len(metricSpecs))
	for k := range metricSpecs {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// promQLFor builds the PromQL expression issued to Prometheus for the
// given query. The kind switch matches metricSpec.kind values; an
// unknown kind is a programming bug (metricSpecs is fixed in code), so
// the branch returns a plain instant lookup as a defensive fallback.
func promQLFor(q metricsSeriesQuery) string {
	matcher := promLabelMatcher(q.labels)
	switch q.spec.kind {
	case "counter":
		return fmt.Sprintf("rate(%s%s[%s])", q.spec.promName, matcher, q.bucketRaw)
	case "histogram":
		// Average per bucket: rate(_sum) / rate(_count). Avoids
		// piping every histogram bucket boundary across the wire.
		return fmt.Sprintf("rate(%s_sum%s[%s]) / rate(%s_count%s[%s])",
			q.spec.promName, matcher, q.bucketRaw,
			q.spec.promName, matcher, q.bucketRaw)
	case "gauge":
		return q.spec.promName + matcher
	default:
		return q.spec.promName + matcher
	}
}

// promLabelMatcher renders the label-set as Prometheus matcher syntax
// (`{k="v",k2="v2"}`) with deterministic ordering so test assertions
// don't have to anticipate map iteration order. An empty label set
// returns "".
func promLabelMatcher(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		// Prometheus matcher value is double-quoted with backslash
		// escaping; we only see allowlisted ASCII keys + whatever
		// the operator typed for the value. Keep it strict.
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// queryPrometheusRange issues the /api/v1/query_range call and
// translates the response into the documented metricsSeriesResponse.
// All upstream-facing failures wrap into typed core.AdaptorError so
// the caller's httperr.WriteError lands on the right status:
//
//	ctx cancel / dial / 5xx     → KindAdaptorDegraded (503)
//	malformed JSON / empty data → KindAdaptorDegraded (502 via Detail)
//
// 502 vs 503 is currently coarse — both ride the same Kind because
// httperr.StatusFor maps KindAdaptorDegraded to 503. Keeping the
// distinction in the message lets ops differentiate without a
// schema change here. When we want true 502 we'd add a new ErrorKind.
func queryPrometheusRange(
	ctx context.Context,
	client *http.Client,
	base string,
	q metricsSeriesQuery,
) (metricsSeriesResponse, error) {
	target, err := buildPromURL(base, q)
	if err != nil {
		return metricsSeriesResponse{}, core.NewAdaptorError(core.KindValidation,
			"invalid Prometheus base URL: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return metricsSeriesResponse{}, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"build prometheus request")
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return metricsSeriesResponse{}, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"prometheus unreachable at %s", base)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return metricsSeriesResponse{}, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"read prometheus response")
	}

	if httpResp.StatusCode >= 500 {
		return metricsSeriesResponse{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"prometheus upstream returned %d: %s",
			httpResp.StatusCode, truncate(string(body), 200))
	}

	var prom promQueryRangeResponse
	if err := json.Unmarshal(body, &prom); err != nil {
		return metricsSeriesResponse{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"prometheus returned malformed JSON: %v (body=%s)",
			err, truncate(string(body), 200))
	}

	// Prometheus signals query-level errors (bad PromQL, missing
	// metric) with 400 + status:"error". A 4xx that ISN'T a Prom
	// error envelope is unexpected — surface as adaptor_degraded.
	if httpResp.StatusCode >= 400 {
		if prom.Status == "error" {
			return metricsSeriesResponse{}, core.NewAdaptorError(core.KindAdaptorDegraded,
				"prometheus rejected query: %s: %s",
				prom.ErrorType, prom.Error)
		}
		return metricsSeriesResponse{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"prometheus returned %d: %s",
			httpResp.StatusCode, truncate(string(body), 200))
	}

	if prom.Status != "success" {
		return metricsSeriesResponse{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"prometheus query failed: %s: %s",
			prom.ErrorType, prom.Error)
	}

	out := metricsSeriesResponse{
		Metric:  q.metric,
		Labels:  q.labels,
		Bucket:  q.bucketRaw,
		Samples: []sample{},
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}

	// Multiple result series may come back when the operator's
	// label set didn't fully constrain the metric. We collapse them
	// into one timeline by summing values per timestamp — the SPA
	// is rendering "this many things happened in this bucket"
	// regardless of which sub-series contributed. Operators wanting
	// per-sub-series breakdowns should pass tighter labels.
	merged := map[float64]float64{}
	for _, series := range prom.Data.Result {
		for _, pair := range series.Values {
			ts, v, ok := decodePromSamplePair(pair)
			if !ok {
				return metricsSeriesResponse{}, core.NewAdaptorError(core.KindAdaptorDegraded,
					"prometheus sample malformed: %v", pair)
			}
			merged[ts] += v
		}
	}

	tss := make([]float64, 0, len(merged))
	for ts := range merged {
		tss = append(tss, ts)
	}
	sort.Float64s(tss)
	for _, ts := range tss {
		v := merged[ts]
		out.Samples = append(out.Samples, sample{
			T: time.Unix(int64(ts), 0).UTC(),
			V: v,
		})
	}

	return out, nil
}

// decodePromSamplePair extracts the (timestamp, value) pair from one
// entry of a Prometheus query_range result series. The wire shape is
// [<float seconds>, "<float as string>"]; the second element is a
// string because Prometheus encodes NaN / +Inf / -Inf that way.
func decodePromSamplePair(pair [2]any) (float64, float64, bool) {
	ts, ok := pair[0].(float64)
	if !ok {
		return 0, 0, false
	}
	s, ok := pair[1].(string)
	if !ok {
		return 0, 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// NaN means "no observations in this bucket" for histogram
		// rate(_sum)/rate(_count). Surface as 0 so the chart doesn't
		// have a hole.
		if s == "NaN" || s == "+Inf" || s == "-Inf" {
			return ts, 0, true
		}
		return 0, 0, false
	}
	return ts, v, true
}

// buildPromURL composes the /api/v1/query_range URL from base + q.
// Returns an error only if base is unparseable; once parsed every
// query parameter is well-formed by construction.
func buildPromURL(base string, q metricsSeriesQuery) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("missing scheme or host")
	}
	// Append the canonical Prometheus query_range path. The base
	// MAY already include a path prefix (e.g. an ingress mounting
	// Prometheus under /prometheus); join carefully.
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/query_range"

	vals := url.Values{}
	vals.Set("query", promQLFor(q))
	vals.Set("start", strconv.FormatInt(q.since.Unix(), 10))
	vals.Set("end", strconv.FormatInt(q.until.Unix(), 10))
	vals.Set("step", q.bucketRaw)
	u.RawQuery = vals.Encode()
	return u.String(), nil
}

// truncate trims s to n bytes and appends an ellipsis when it exceeds
// the cap. Used to keep error envelopes short — we don't want a
// runaway 1MB upstream body in our log lines.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
