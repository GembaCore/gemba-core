// Refine-specific columns (gm-51i2). These extend WorkItemGrid's core
// column registry and are hidden by default everywhere except /refine,
// where RefinePage flips them on via the visibilityOverride prop.
//
// Helpers (formatAge, countIncomingBlockers, getSuggestedEpic) are
// exported for unit testing — the cell renderers wrap them.

import type { ColumnDef } from '@tanstack/react-table';
import type { WorkItem } from '@/types/core.gen';

const SUGGESTED_EPIC_KEY = 'gemba.suggested_epic';
const MS_PER_DAY = 86_400_000;
const DAYS_PER_MONTH = 30;
const MONTHS_PER_YEAR = 12;

// Age string: <30d → "Nd", <12mo → "Nmo", else "Ny". Floor for stable
// bucket boundaries.
export function formatAge(createdAt: string | undefined, now: Date = new Date()): string {
  if (!createdAt) return '—';
  const t = Date.parse(createdAt);
  if (!Number.isFinite(t)) return '—';
  const diffMs = now.getTime() - t;
  if (diffMs < 0) return '0d';
  const days = Math.floor(diffMs / MS_PER_DAY);
  if (days < DAYS_PER_MONTH) return `${days}d`;
  const months = Math.floor(days / DAYS_PER_MONTH);
  if (months < MONTHS_PER_YEAR) return `${months}mo`;
  const years = Math.floor(months / MONTHS_PER_YEAR);
  return `${years}y`;
}

export function ageTimestamp(createdAt: string | undefined): number {
  if (!createdAt) return 0;
  const t = Date.parse(createdAt);
  return Number.isFinite(t) ? t : 0;
}

export function countIncomingBlockers(item: WorkItem): number {
  const rels = item.relationships;
  if (!rels || rels.length === 0) return 0;
  let n = 0;
  for (const r of rels) {
    if (r.kind === 'blocks' && r.to === item.id) n++;
  }
  return n;
}

export function getSuggestedEpic(item: WorkItem): string | null {
  const v = item.custom?.[SUGGESTED_EPIC_KEY];
  return typeof v === 'string' && v.length > 0 ? v : null;
}

// shouldRenderDispatchStatus suppresses both the empty string and the
// default "ready" status — the chip should only appear when something
// non-default is in play.
export function shouldRenderDispatchStatus(item: WorkItem): boolean {
  const ds = item.dispatch_status;
  return !!ds && ds !== 'ready';
}

export const refineColumns: ColumnDef<WorkItem>[] = [
  {
    id: 'age',
    header: 'Age',
    accessorFn: (r) => ageTimestamp(r.created_at),
    size: 80,
    sortingFn: 'basic',
    // Older = smaller timestamp; default-desc puts oldest first.
    sortDescFirst: false,
    cell: ({ row }) => (
      <span
        className="text-xs text-neutral-600 dark:text-neutral-400"
        data-testid={`grid-cell-age-${row.original.id}`}
      >
        {formatAge(row.original.created_at)}
      </span>
    ),
  },
  {
    id: 'suggested_epic',
    header: 'Suggested epic',
    // Empty string sorts after populated when ascending; flip the
    // sortDescFirst so "show populated first" is the natural click.
    accessorFn: (r) => getSuggestedEpic(r) ?? '',
    size: 160,
    sortingFn: (a, b, columnId) => {
      const av = (a.getValue(columnId) as string) ?? '';
      const bv = (b.getValue(columnId) as string) ?? '';
      // populated < empty (so empties sink in ascending order).
      if (!!av === !!bv) return av.localeCompare(bv);
      return av ? -1 : 1;
    },
    cell: ({ row }) => {
      const epic = getSuggestedEpic(row.original);
      if (!epic) {
        return (
          <span
            className="text-[11px] italic text-neutral-400 dark:text-neutral-600"
            data-testid={`grid-cell-suggested-epic-${row.original.id}`}
          >
            —
          </span>
        );
      }
      return (
        <span
          className="inline-block rounded bg-violet-100 px-1.5 py-0.5 font-mono text-[11px] text-violet-800 dark:bg-violet-950 dark:text-violet-200"
          data-testid={`grid-cell-suggested-epic-${row.original.id}`}
        >
          {epic}
        </span>
      );
    },
  },
  {
    id: 'blockers',
    header: 'Blockers',
    accessorFn: (r) => countIncomingBlockers(r),
    size: 88,
    sortingFn: 'basic',
    cell: ({ row }) => {
      const n = countIncomingBlockers(row.original);
      if (n === 0) {
        return (
          <span data-testid={`grid-cell-blockers-${row.original.id}`} className="text-xs text-neutral-400">
            —
          </span>
        );
      }
      return (
        <span
          className="inline-block rounded bg-amber-100 px-1.5 py-0.5 font-mono text-[11px] text-amber-800 dark:bg-amber-950 dark:text-amber-200"
          data-testid={`grid-cell-blockers-${row.original.id}`}
        >
          {n}
        </span>
      );
    },
  },
  {
    id: 'dispatch_status',
    header: 'Dispatch',
    accessorFn: (r) => r.dispatch_status ?? '',
    size: 144,
    sortingFn: 'alphanumeric',
    cell: ({ row }) => {
      if (!shouldRenderDispatchStatus(row.original)) {
        return (
          <span
            data-testid={`grid-cell-dispatch-${row.original.id}`}
            className="text-xs text-neutral-400"
          >
            —
          </span>
        );
      }
      return (
        <span
          className="inline-block rounded bg-sky-100 px-1.5 py-0.5 font-mono text-[11px] text-sky-800 dark:bg-sky-950 dark:text-sky-200"
          data-testid={`grid-cell-dispatch-${row.original.id}`}
        >
          {row.original.dispatch_status}
        </span>
      );
    },
  },
];
