// Typed data-access for /api/v1/metrics/series (gm-e9m0 / gm-e12.17.1).
//
// The backend handler is being built in parallel under gm-e9m0. This
// client is shaped to the agreed contract:
//
//   GET /api/v1/metrics/series
//     ?metric=<name>
//     &since=<RFC3339>
//     &until=<RFC3339>
//     &bucket=<duration>           // e.g. "1h", "5m", "1d"
//     &labels=k:v,k:v              // optional comma-separated key:value
//
//   200 → MetricSeries
//   503 → adaptor_degraded / adaptor_not_configured (caller treats as
//         empty so the panel renders the empty-state per tile rather
//         than a hard error banner — same pattern as /api/drift while
//         that endpoint is stubbed).

import { apiFetch, ApiError } from './client';

export interface MetricSample {
  // RFC3339 timestamp (start of bucket).
  t: string;
  // Bucket value. Counters surface their delta over the bucket, gauges
  // surface their last observed value, histograms surface the chosen
  // aggregation (avg / p95 — see backend rules).
  v: number;
}

export interface MetricSeries {
  metric: string;
  // Echoes the request labels so multi-series tiles can disambiguate.
  // Backend may add additional labels (e.g. resolved instance) — we
  // pass-through anything string-valued.
  labels: Record<string, string>;
  samples: MetricSample[];
  // Echo of the requested bucket so the renderer can space the X axis
  // even when the series is empty.
  bucket: string;
}

export interface MetricsSeriesQuery {
  metric: string;
  // RFC3339 strings. Caller computes the window from the
  // TimeWindowSelector value.
  since: string;
  until: string;
  // Duration string. Caller picks based on the window length so the
  // chart has ~50–200 points at the chosen granularity.
  bucket: string;
  // Optional label filters. Encoded as `k:v,k:v`.
  labels?: Record<string, string>;
}

// Build the query string for GET /api/v1/metrics/series. Exported so
// tests can pin the encoding without invoking fetch.
export function buildSeriesQuery(q: MetricsSeriesQuery): string {
  const params = new URLSearchParams();
  params.set('metric', q.metric);
  params.set('since', q.since);
  params.set('until', q.until);
  params.set('bucket', q.bucket);
  if (q.labels && Object.keys(q.labels).length > 0) {
    const pairs = Object.entries(q.labels).map(
      ([k, v]) => `${k}:${v}`,
    );
    params.set('labels', pairs.join(','));
  }
  return params.toString();
}

// fetchMetricSeries is the typed entry point used by useMetricsSeries.
// 503 from the backend collapses into an "empty series" snapshot so the
// individual tile shows its empty-state rather than the page-level
// error banner — the panel-level 503 is meant to mean "telemetry not
// configured", which per-tile is more useful than a global red bar.
export async function fetchMetricSeries(
  q: MetricsSeriesQuery,
): Promise<MetricSeries> {
  const qs = buildSeriesQuery(q);
  try {
    return await apiFetch<MetricSeries>(`/v1/metrics/series?${qs}`);
  } catch (e) {
    if (e instanceof ApiError && e.isAdaptorDegraded) {
      return {
        metric: q.metric,
        labels: q.labels ?? {},
        samples: [],
        bucket: q.bucket,
      };
    }
    throw e;
  }
}

// emptyMetricSeries is the deterministic zero value tile renderers can
// fall back to (e.g. when the hook hasn't fetched yet).
export function emptyMetricSeries(
  metric: string,
  bucket: string,
): MetricSeries {
  return { metric, labels: {}, samples: [], bucket };
}
