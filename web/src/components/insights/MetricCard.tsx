// MetricCard (gm-e12.17.1).
//
// Shared chart card for the Insights panel. One card == one tile in
// the 4-up grid. Owns the title bar, loading / error / empty branches,
// and the recharts wiring. The owning page passes a `MetricSeries`
// (already fetched via useMetricsSeries) plus presentation props.
//
// Empty-state behaviour: a series with zero samples (or all-zero
// samples for windows that haven't seen activity) renders an
// "empty for this window" card so operators see the gap rather than
// a flat baseline they could mistake for data.

import type { ReactNode } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import type { MetricSeries } from '@/api/metrics';
import { cn } from '@/lib/utils';

export type MetricChartKind = 'line' | 'area';

export interface MetricCardProps {
  title: string;
  subtitle?: string;
  testid: string;
  series: MetricSeries | undefined;
  isLoading?: boolean;
  error?: Error | null;
  // line for rate / gauge series; area for backlog / accumulation.
  kind?: MetricChartKind;
  // Y-axis label suffix (e.g. "/ day", "min", "open").
  unit?: string;
  // Override the empty state ("no samples yet" vs "no spawns in
  // window"). Tile-specific copy lands here.
  emptyHint?: string;
  // Optional value transform — e.g. counter-rate-per-day from a
  // session_spawn_total bucket value. Defaults to identity.
  transformValue?: (v: number, sample: { t: string; v: number }) => number;
  className?: string;
  // Bottom-right slot — used by the deferred-cost card to render a
  // "Awaiting gm-e11.2" hint without a chart.
  footer?: ReactNode;
  // Render this in place of the chart area (used by the deferred-cost
  // stub). When set, samples are not rendered.
  body?: ReactNode;
}

export function MetricCard({
  title,
  subtitle,
  testid,
  series,
  isLoading,
  error,
  kind = 'line',
  unit,
  emptyHint,
  transformValue,
  className,
  footer,
  body,
}: MetricCardProps): JSX.Element {
  const hasSamples = !!series && series.samples.length > 0;
  const data = hasSamples
    ? series!.samples.map((s) => ({
        t: s.t,
        // Pre-format the tick label so the X axis stays compact.
        tick: formatTick(s.t),
        v: transformValue ? transformValue(s.v, s) : s.v,
      }))
    : [];

  return (
    <section
      data-testid={testid}
      className={cn(
        'flex flex-col rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900',
        className,
      )}
    >
      <header className="mb-2">
        <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
        {subtitle && (
          <p className="mt-0.5 text-xs text-neutral-500 dark:text-neutral-400">
            {subtitle}
          </p>
        )}
      </header>

      <div className="relative h-48 min-h-0 flex-1">
        {body ? (
          body
        ) : isLoading && !hasSamples ? (
          <div
            data-testid={`${testid}-loading`}
            className="flex h-full items-center justify-center text-xs text-neutral-500"
          >
            Loading…
          </div>
        ) : error ? (
          <div
            data-testid={`${testid}-error`}
            className="flex h-full items-center justify-center px-2 text-center text-xs text-rose-600 dark:text-rose-300"
          >
            Could not load metric: {error.message}
          </div>
        ) : !hasSamples ? (
          <div
            data-testid={`${testid}-empty`}
            className="flex h-full items-center justify-center px-2 text-center text-xs text-neutral-500 dark:text-neutral-400"
          >
            {emptyHint ?? 'No samples in selected window.'}
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            {kind === 'area' ? (
              <AreaChart data={data} margin={{ top: 4, right: 8, bottom: 4, left: 0 }}>
                <CartesianGrid strokeDasharray="2 4" stroke="currentColor" strokeOpacity={0.1} />
                <XAxis
                  dataKey="tick"
                  tick={{ fontSize: 10 }}
                  stroke="currentColor"
                  strokeOpacity={0.4}
                  tickLine={false}
                  axisLine={false}
                />
                <YAxis
                  tick={{ fontSize: 10 }}
                  stroke="currentColor"
                  strokeOpacity={0.4}
                  tickLine={false}
                  axisLine={false}
                  width={32}
                />
                <Tooltip
                  formatter={(v: unknown) => {
                    const display = unit ? `${String(v)} ${unit}` : String(v);
                    return [display, title];
                  }}
                  labelFormatter={(_: unknown, payload: unknown) => {
                    const arr = payload as Array<{ payload?: { t?: unknown } }> | undefined;
                    const t = arr?.[0]?.payload?.t;
                    return t ? formatTooltipLabel(String(t)) : '';
                  }}
                  contentStyle={{ fontSize: 11 }}
                />
                <Area
                  type="monotone"
                  dataKey="v"
                  stroke="#0ea5e9"
                  fill="#0ea5e9"
                  fillOpacity={0.15}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                />
              </AreaChart>
            ) : (
              <LineChart data={data} margin={{ top: 4, right: 8, bottom: 4, left: 0 }}>
                <CartesianGrid strokeDasharray="2 4" stroke="currentColor" strokeOpacity={0.1} />
                <XAxis
                  dataKey="tick"
                  tick={{ fontSize: 10 }}
                  stroke="currentColor"
                  strokeOpacity={0.4}
                  tickLine={false}
                  axisLine={false}
                />
                <YAxis
                  tick={{ fontSize: 10 }}
                  stroke="currentColor"
                  strokeOpacity={0.4}
                  tickLine={false}
                  axisLine={false}
                  width={32}
                />
                <Tooltip
                  formatter={(v: unknown) => {
                    const display = unit ? `${String(v)} ${unit}` : String(v);
                    return [display, title];
                  }}
                  labelFormatter={(_: unknown, payload: unknown) => {
                    const arr = payload as Array<{ payload?: { t?: unknown } }> | undefined;
                    const t = arr?.[0]?.payload?.t;
                    return t ? formatTooltipLabel(String(t)) : '';
                  }}
                  contentStyle={{ fontSize: 11 }}
                />
                <Line
                  type="monotone"
                  dataKey="v"
                  stroke="#0ea5e9"
                  strokeWidth={1.5}
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            )}
          </ResponsiveContainer>
        )}
      </div>
      {footer && <div className="mt-2 text-xs text-neutral-500">{footer}</div>}
    </section>
  );
}

// formatTick renders a compact axis label. We don't try to be locale-
// aware (the Insights panel is operator-internal) — a fixed
// MM-DD HH:mm shape keeps the X axis legible across windows.
function formatTick(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const mm = String(d.getUTCMonth() + 1).padStart(2, '0');
  const dd = String(d.getUTCDate()).padStart(2, '0');
  const hh = String(d.getUTCHours()).padStart(2, '0');
  const mi = String(d.getUTCMinutes()).padStart(2, '0');
  return `${mm}-${dd} ${hh}:${mi}`;
}

function formatTooltipLabel(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toUTCString();
}
