// InsightsPage (gm-e12.17.1).
//
// Replaces the placeholder export from web/src/pages/placeholders.tsx.
// Renders the first viewport of the Insights dashboard:
//
//   1. Spawn rate (sessions / day) — derived from
//      `session_spawn_total` counter via per-bucket rate.
//   2. Escalation backlog — gauge of `escalation_open_count` over
//      time (line, no per-source faceting yet — the contract supports
//      labels, we just don't use them in the MVP).
//   3. Stuck-session minutes — derived from `workitem_state_duration`
//      histogram with state filter (prompting / stalled).
//   4. Token cost — stub awaiting gm-e11.2 (CostMeter rollups).
//
// Chrome:
//   - 4-up CSS grid (collapses to 1-col on narrow viewports).
//   - Time-window selector top-right (default 14d).
//   - 60s React Query refetch (no SSE, by design).
//
// Time window state is local component state; we don't persist it to
// the URL yet — once /insights gets sub-pages we can lift it. The
// selected preset feeds (since, until, bucket) into all three live
// tiles via useMetricsSeries.

import { useMemo, useState } from 'react';
import { LineChart as LineChartIcon, Coins } from 'lucide-react';
import {
  computeRange,
  DEFAULT_WINDOW,
  presetFor,
  TimeWindowSelector,
  type TimeWindowKey,
} from '@/components/insights/TimeWindowSelector';
import { MetricCard } from '@/components/insights/MetricCard';
import { useMetricsSeries } from '@/hooks/useMetricsSeries';

// Stable wall-clock anchor per render — without this, the (since,
// until) range walks forward every render and we'd churn React Query
// keys constantly.
function useNow(windowKey: TimeWindowKey): { since: string; until: string; bucket: string } {
  // Re-evaluate when the window changes (so the chart redraws), but
  // freeze `now` for a 60s window aligned to the refetch cadence so
  // background polls don't cause a key change every render.
  return useMemo(() => {
    const aligned = new Date(Math.floor(Date.now() / 60_000) * 60_000);
    return computeRange(windowKey, aligned);
  }, [windowKey]);
}

export function InsightsPage(): JSX.Element {
  const [windowKey, setWindowKey] = useState<TimeWindowKey>(DEFAULT_WINDOW);
  const range = useNow(windowKey);
  const preset = presetFor(windowKey);

  // Bucket size for converting raw counter deltas into "per day".
  // The backend reports `session_spawn_total` as the count within
  // each bucket; we scale by (1 day / bucket length) for the rate.
  const bucketMs = parseDurationMs(preset.bucket);
  const bucketsPerDay = bucketMs > 0 ? (24 * 60 * 60_000) / bucketMs : 1;

  const spawnQ = useMetricsSeries({
    metric: 'session_spawn_total',
    since: range.since,
    until: range.until,
    bucket: range.bucket,
  });

  const escalationQ = useMetricsSeries({
    metric: 'escalation_open_count',
    since: range.since,
    until: range.until,
    bucket: range.bucket,
  });

  const stuckQ = useMetricsSeries({
    metric: 'workitem_state_duration',
    since: range.since,
    until: range.until,
    bucket: range.bucket,
    labels: { state_category: 'started' },
  });

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="insights-page">
      <header className="flex items-center justify-between border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
            <LineChartIcon className="h-5 w-5" aria-hidden />
            Insights
          </h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Operator-glance telemetry. Refreshes every 60s; cost rollups
            land with gm-e11.2.
          </p>
        </div>
        <TimeWindowSelector value={windowKey} onChange={setWindowKey} />
      </header>

      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div
          data-testid="insights-grid"
          className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4"
        >
          <MetricCard
            testid="insights-tile-spawn-rate"
            title="Spawn rate"
            subtitle="Sessions per day (counter rate)"
            unit="/ day"
            kind="line"
            series={spawnQ.data}
            isLoading={spawnQ.isLoading}
            error={spawnQ.error}
            emptyHint="No sessions spawned in this window."
            transformValue={(v) => Math.round(v * bucketsPerDay * 100) / 100}
          />

          <MetricCard
            testid="insights-tile-escalation-backlog"
            title="Escalation backlog"
            subtitle="Open escalations over time"
            unit="open"
            kind="area"
            series={escalationQ.data}
            isLoading={escalationQ.isLoading}
            error={escalationQ.error}
            emptyHint="No backlog samples in this window."
          />

          <MetricCard
            testid="insights-tile-stuck-minutes"
            title="Stuck-session minutes"
            subtitle="workitem_state_duration · state=prompting"
            unit="min"
            kind="line"
            series={stuckQ.data}
            isLoading={stuckQ.isLoading}
            error={stuckQ.error}
            emptyHint="No stuck-session samples in this window."
            transformValue={(v) => Math.round((v / 60) * 100) / 100}
          />

          {/* Deferred-cost stub — chart slot is replaced with a
              static body, no fetch. Lands as a real tile when the
              CostMeter rollup endpoint exists (gm-e11.2). */}
          <MetricCard
            testid="insights-tile-cost-deferred"
            title="Token cost"
            subtitle="Agent vs advisor split"
            series={undefined}
            body={
              <div
                data-testid="insights-tile-cost-deferred-stub"
                className="flex h-full flex-col items-center justify-center gap-2 px-2 text-center"
              >
                <Coins className="h-6 w-6 text-neutral-400" aria-hidden />
                <p className="text-xs font-medium text-neutral-700 dark:text-neutral-200">
                  Awaiting gm-e11.2
                </p>
                <p className="text-[11px] text-neutral-500 dark:text-neutral-400">
                  CostMeter rollups land before this tile lights up.
                </p>
              </div>
            }
          />
        </div>
      </div>
    </div>
  );
}

// parseDurationMs parses the small subset of duration strings the
// backend uses for bucket sizes (`15m`, `1h`, `6h`, `1d`). Returns 0
// for anything unrecognised so callers can defend against bad input.
function parseDurationMs(d: string): number {
  const match = /^(\d+)([smhd])$/.exec(d);
  if (!match) return 0;
  const n = parseInt(match[1], 10);
  switch (match[2]) {
    case 's':
      return n * 1000;
    case 'm':
      return n * 60_000;
    case 'h':
      return n * 60 * 60_000;
    case 'd':
      return n * 24 * 60 * 60_000;
    default:
      return 0;
  }
}

export default InsightsPage;
