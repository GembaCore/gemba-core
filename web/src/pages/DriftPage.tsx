// DriftPage (gm-e12.13) — desired-vs-actual reconciliation surface.
//
// Renders the diff between OrchestrationPlaneAdaptor.DeclaredState() and
// ObservedState() (internal/core/orchestration.go), surfacing every
// field that has drifted between what the workspace's config asks for
// and what the adaptor actually observes running.
//
// Today /api/drift is a 501 stub (internal/server/router.go — gm-e2.6
// follow-up). The api/drift.ts fetcher maps 501 to an empty snapshot,
// so this page renders a clean "no drift detected" empty state until
// the endpoint is wired. When real data lands, the same loaded path
// kicks in unchanged.
//
// Resolves DD-10. Out of scope here: SSE auto-refresh, "fix drift"
// action button, and column-tinting on the Board (the latter would
// pull plumbing changes outside the drift slice — see bead notes).

import { useQuery } from '@tanstack/react-query';
import { GitCompare, RefreshCw } from 'lucide-react';
import { getDriftSnapshot } from '@/api/drift';
import type { DriftEntry, DriftSnapshot } from '@/types/drift';
import { cn } from '@/lib/utils';

const QUERY_KEY = ['drift'] as const;

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '∅';
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

function formatTimestamp(iso: string): string {
  if (!iso) return 'never';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

export function DriftPage(): JSX.Element {
  const { data, error, isLoading, isFetching, refetch } = useQuery<DriftSnapshot, Error>({
    queryKey: QUERY_KEY,
    queryFn: getDriftSnapshot,
    // Drift is forensic. Operators reload deliberately; no background
    // polling until SSE-on-drift lands.
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  });

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="drift-page">
      <header className="flex items-center justify-between border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
            <GitCompare className="h-5 w-5" aria-hidden />
            Desired vs actual
          </h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Snapshot of every field where the orchestration plane's
            observed state diverges from the workspace's declared
            topology. Read-only forensic view.
          </p>
        </div>
        <div className="flex items-center gap-3 text-xs text-neutral-500 dark:text-neutral-400">
          <span data-testid="drift-fetched-at">
            fetched&nbsp;
            <time dateTime={data?.fetched_at || undefined}>
              {formatTimestamp(data?.fetched_at ?? '')}
            </time>
          </span>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            data-testid="drift-refresh"
            className="inline-flex items-center gap-1 rounded-md border border-neutral-300 px-2 py-1 text-xs hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            <RefreshCw
              className={cn('h-3 w-3', isFetching && 'animate-spin')}
              aria-hidden
            />
            Refresh
          </button>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-auto p-6">
        {isLoading ? (
          <div data-testid="drift-loading" className="text-sm text-neutral-500">
            Loading drift snapshot…
          </div>
        ) : error ? (
          <div
            data-testid="drift-error"
            className="rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
          >
            Could not load drift data: {error.message}
          </div>
        ) : data ? (
          <DriftLoaded snapshot={data} />
        ) : null}
      </div>
    </div>
  );
}

function DriftLoaded({ snapshot }: { snapshot: DriftSnapshot }): JSX.Element {
  const { drift, desired_count, actual_count } = snapshot;
  return (
    <div className="space-y-6">
      <section
        data-testid="drift-summary"
        className="grid grid-cols-1 gap-4 sm:grid-cols-3"
      >
        <SummaryCard
          label="Desired entities"
          value={desired_count}
          testid="drift-summary-desired"
        />
        <SummaryCard
          label="Observed entities"
          value={actual_count}
          testid="drift-summary-actual"
        />
        <SummaryCard
          label="Drifted fields"
          value={drift.length}
          tone={drift.length > 0 ? 'warn' : 'ok'}
          testid="drift-summary-drift"
        />
      </section>

      {drift.length === 0 ? (
        <div
          data-testid="drift-empty"
          className="rounded-md border border-dashed border-neutral-300 p-8 text-center text-sm text-neutral-500 dark:border-neutral-700"
        >
          No drift detected. Every field of the observed topology
          matches the declared state.
        </div>
      ) : (
        <DriftTable rows={drift} />
      )}
    </div>
  );
}

function SummaryCard({
  label,
  value,
  tone,
  testid,
}: {
  label: string;
  value: number;
  tone?: 'ok' | 'warn';
  testid?: string;
}): JSX.Element {
  return (
    <div
      data-testid={testid}
      className={cn(
        'rounded-md border px-4 py-3',
        tone === 'warn'
          ? 'border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-950'
          : 'border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900'
      )}
    >
      <div className="text-[11px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
        {label}
      </div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
    </div>
  );
}

function DriftTable({ rows }: { rows: DriftEntry[] }): JSX.Element {
  return (
    <div
      data-testid="drift-table"
      className="overflow-hidden rounded-md border border-neutral-200 dark:border-neutral-800"
    >
      <table className="w-full text-sm">
        <thead className="bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-900 dark:text-neutral-400">
          <tr>
            <th className="px-3 py-2 font-medium">Kind</th>
            <th className="px-3 py-2 font-medium">Id</th>
            <th className="px-3 py-2 font-medium">Field</th>
            <th className="px-3 py-2 font-medium">Desired</th>
            <th className="px-3 py-2 font-medium">Actual</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-neutral-200 dark:divide-neutral-800">
          {rows.map((row, i) => (
            <DriftRow key={`${row.kind}:${row.id}:${row.field}:${i}`} row={row} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function DriftRow({ row }: { row: DriftEntry }): JSX.Element {
  const sameValue =
    JSON.stringify(row.desired) === JSON.stringify(row.actual);
  return (
    <tr
      data-testid={`drift-row-${row.kind}-${row.id}-${row.field}`}
      className={cn(sameValue && 'opacity-60')}
    >
      <td className="px-3 py-2 align-top">
        <span className="inline-flex items-center rounded bg-neutral-100 px-1.5 py-0.5 text-[11px] font-medium text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
          {row.kind}
        </span>
      </td>
      <td className="px-3 py-2 align-top font-mono text-xs">{row.id}</td>
      <td className="px-3 py-2 align-top font-mono text-xs">{row.field}</td>
      <td
        data-testid="drift-cell-desired"
        className="px-3 py-2 align-top font-mono text-xs text-neutral-700 dark:text-neutral-300"
      >
        {formatValue(row.desired)}
      </td>
      <td
        data-testid="drift-cell-actual"
        className={cn(
          'px-3 py-2 align-top font-mono text-xs',
          sameValue
            ? 'text-neutral-700 dark:text-neutral-300'
            : 'bg-rose-50 text-rose-800 dark:bg-rose-950 dark:text-rose-200'
        )}
      >
        {formatValue(row.actual)}
      </td>
    </tr>
  );
}

export default DriftPage;
