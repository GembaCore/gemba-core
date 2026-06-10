// useMetricsSeries (gm-e12.17.1).
//
// React Query hook wrapper around fetchMetricSeries. Auto-refreshes
// every 60s — the panel is non-realtime by design (no SSE), so a
// minute is the right blend of "fresh enough for an operator-glance
// dashboard" without hammering the server with traffic for a graph
// nobody is watching most of the time.

import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import {
  fetchMetricSeries,
  type MetricSeries,
  type MetricsSeriesQuery,
} from '@/api/metrics';

export const METRICS_REFETCH_MS = 60_000;

// Stable query-key shape: [scope, metric, since, until, bucket, labels-sorted].
// `since`/`until` are part of the key so changing the window selector
// triggers a refetch with the new params (rather than reusing the cached
// payload at a stale window).
export function metricsSeriesKey(q: MetricsSeriesQuery): readonly unknown[] {
  const sortedLabels = q.labels
    ? Object.entries(q.labels)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([k, v]) => `${k}:${v}`)
        .join(',')
    : '';
  return ['metrics', 'series', q.metric, q.since, q.until, q.bucket, sortedLabels] as const;
}

export interface UseMetricsSeriesOptions {
  enabled?: boolean;
  // Override default 60s polling. Set to 0 / false to disable.
  refetchInterval?: number | false;
}

export function useMetricsSeries(
  q: MetricsSeriesQuery,
  opts: UseMetricsSeriesOptions = {},
): UseQueryResult<MetricSeries, Error> {
  const { enabled = true, refetchInterval = METRICS_REFETCH_MS } = opts;
  return useQuery<MetricSeries, Error>({
    queryKey: metricsSeriesKey(q),
    queryFn: () => fetchMetricSeries(q),
    enabled,
    refetchInterval: refetchInterval === 0 ? false : refetchInterval,
    refetchOnWindowFocus: false,
    // Keep the previous frame on screen while refetching so the chart
    // doesn't flash empty on every poll.
    staleTime: 30_000,
  });
}
