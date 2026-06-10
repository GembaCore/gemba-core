// SessionHealthPanel (gm-s47n.5.3). Read-only operator-facing
// table of every live session's health telemetry — context
// pressure, concept drift, time-on-task — with threshold-based
// advisory tags per spec §4 Layer 4:
//
//   pressure > 0.6 → warn
//   pressure > 0.8 → strongly suggest recycle
//   drift   > 0.5 → warn
//   drift   > 0.7 → suggest recycle (when next bead's concepts differ)
//   time_on_task > 4h → recycle if next bead is a new concept area
//
// Pure component. The page that renders it is responsible for
// fetching and refreshing the data.

import { useMemo } from 'react';
import { AlertTriangle, Clock, Sparkles } from 'lucide-react';
import type { PlannerOperationalContext } from '@/api/planner';
import { cn } from '@/lib/utils';

// Advisory thresholds — spec §4 Layer 4. Constants so the panel,
// the recycle gate (Go side), and tests reference the same numbers.
export const PRESSURE_WARN = 0.6;
export const PRESSURE_RECYCLE = 0.8;
export const DRIFT_WARN = 0.5;
export const DRIFT_RECYCLE = 0.7;
export const TIME_ON_TASK_RECYCLE_HOURS = 4;

export type Severity = 'ok' | 'warn' | 'recycle';

export interface SessionHealthPanelProps {
  sessions: PlannerOperationalContext[];
  // sortBySeverity orders the rows recycle → warn → ok so the
  // operator's eye lands on the sessions that need attention. When
  // false (default), preserves the input order so the page can
  // align rows with another component (e.g. the coach grid).
  sortBySeverity?: boolean;
  testid?: string;
}

export function SessionHealthPanel({
  sessions,
  sortBySeverity,
  testid,
}: SessionHealthPanelProps) {
  const rows = useMemo(() => {
    const decorated = sessions.map((s) => ({ ctx: s, sev: maxSeverity(s) }));
    if (sortBySeverity) {
      decorated.sort((a, b) => severityRank(b.sev) - severityRank(a.sev));
    }
    return decorated;
  }, [sessions, sortBySeverity]);

  if (sessions.length === 0) {
    return (
      <div
        data-testid={testid ?? 'session-health-panel'}
        className="rounded-md border border-neutral-200 bg-white p-4 text-sm text-neutral-500 dark:border-neutral-800 dark:bg-neutral-900"
      >
        No live sessions.
      </div>
    );
  }
  return (
    <div
      data-testid={testid ?? 'session-health-panel'}
      className="overflow-hidden rounded-md border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900"
    >
      <table className="min-w-full text-sm">
        <thead className="bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-950">
          <tr>
            <Th>Session</Th>
            <Th>
              <span className="inline-flex items-center gap-1">
                <Sparkles className="h-3 w-3" aria-hidden /> pressure
              </span>
            </Th>
            <Th>
              <span className="inline-flex items-center gap-1">
                <AlertTriangle className="h-3 w-3" aria-hidden /> drift
              </span>
            </Th>
            <Th>
              <span className="inline-flex items-center gap-1">
                <Clock className="h-3 w-3" aria-hidden /> time-on-task
              </span>
            </Th>
            <Th>state</Th>
          </tr>
        </thead>
        <tbody>
          {rows.map(({ ctx, sev }) => (
            <HealthRow key={ctx.session?.id ?? Math.random()} ctx={ctx} severity={sev} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function HealthRow({
  ctx,
  severity,
}: {
  ctx: PlannerOperationalContext;
  severity: Severity;
}) {
  const sid = ctx.session?.id ?? '?';
  const h = ctx.health;
  const pressureSev = h ? pressureSeverity(h.context_pressure) : 'ok';
  const driftSev = h ? driftSeverity(h.concept_drift) : 'ok';
  const timeSev = h ? timeOnTaskSeverity(h.time_on_task_ns) : 'ok';
  return (
    <tr
      data-testid={`session-health-row-${sid}`}
      data-severity={severity}
      className={cn('border-t border-neutral-100 dark:border-neutral-900', rowTone(severity))}
    >
      <td className="px-3 py-2">
        <div className="font-mono text-[11px] text-neutral-500">{sid}</div>
        <div className="font-medium">{ctx.agent?.id ?? '(anon)'}</div>
      </td>
      <td className="px-3 py-2 align-top">
        <Metric value={h?.context_pressure} severity={pressureSev} />
      </td>
      <td className="px-3 py-2 align-top">
        <Metric value={h?.concept_drift} severity={driftSev} />
      </td>
      <td className="px-3 py-2 align-top">
        <Metric
          value={h ? hoursFromNs(h.time_on_task_ns) : undefined}
          severity={timeSev}
          suffix="h"
          precision={1}
        />
      </td>
      <td className="px-3 py-2 align-top">
        <SeverityBadge severity={severity} />
      </td>
    </tr>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return (
    <th className="border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">{children}</th>
  );
}

function Metric({
  value,
  severity,
  suffix,
  precision = 2,
}: {
  value: number | undefined;
  severity: Severity;
  suffix?: string;
  precision?: number;
}) {
  if (value === undefined) {
    return <span className="text-neutral-400">—</span>;
  }
  return (
    <span className={cn('font-mono', metricTone(severity))} data-metric-severity={severity}>
      {value.toFixed(precision)}
      {suffix ?? ''}
    </span>
  );
}

function SeverityBadge({ severity }: { severity: Severity }) {
  const label = severity === 'recycle' ? 'RECYCLE' : severity === 'warn' ? 'WARN' : 'ok';
  return (
    <span
      data-testid="session-health-severity"
      className={cn(
        'inline-flex items-center rounded px-2 py-0.5 font-mono text-[10px] uppercase tracking-wide',
        badgeTone(severity)
      )}
    >
      {label}
    </span>
  );
}

// pressureSeverity / driftSeverity / timeOnTaskSeverity ARE the
// public predicates: tests assert on them directly so the planner
// (Go) and the panel never drift.

export function pressureSeverity(p: number): Severity {
  if (p > PRESSURE_RECYCLE) return 'recycle';
  if (p > PRESSURE_WARN) return 'warn';
  return 'ok';
}

export function driftSeverity(d: number): Severity {
  if (d > DRIFT_RECYCLE) return 'recycle';
  if (d > DRIFT_WARN) return 'warn';
  return 'ok';
}

export function timeOnTaskSeverity(ns: number): Severity {
  const hours = hoursFromNs(ns);
  if (hours > TIME_ON_TASK_RECYCLE_HOURS) return 'recycle';
  return 'ok';
}

// maxSeverity is the row-level summary: the worst across the three
// metrics. Drives the table sort + the right-hand badge.
export function maxSeverity(ctx: PlannerOperationalContext): Severity {
  if (!ctx.health) return 'ok';
  const sevs = [
    pressureSeverity(ctx.health.context_pressure),
    driftSeverity(ctx.health.concept_drift),
    timeOnTaskSeverity(ctx.health.time_on_task_ns),
  ];
  if (sevs.includes('recycle')) return 'recycle';
  if (sevs.includes('warn')) return 'warn';
  return 'ok';
}

function severityRank(s: Severity): number {
  switch (s) {
    case 'recycle':
      return 2;
    case 'warn':
      return 1;
    default:
      return 0;
  }
}

function hoursFromNs(ns: number): number {
  return ns / 1e9 / 3600;
}

function rowTone(s: Severity): string {
  switch (s) {
    case 'recycle':
      return 'bg-rose-50/50 dark:bg-rose-950/20';
    case 'warn':
      return 'bg-amber-50/50 dark:bg-amber-950/20';
    default:
      return '';
  }
}

function metricTone(s: Severity): string {
  switch (s) {
    case 'recycle':
      return 'text-rose-700 dark:text-rose-300';
    case 'warn':
      return 'text-amber-700 dark:text-amber-300';
    default:
      return 'text-neutral-700 dark:text-neutral-300';
  }
}

function badgeTone(s: Severity): string {
  switch (s) {
    case 'recycle':
      return 'bg-rose-100 text-rose-800 dark:bg-rose-900/40 dark:text-rose-200';
    case 'warn':
      return 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200';
    default:
      return 'bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300';
  }
}
