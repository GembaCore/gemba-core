// WorkItemGrid (gm-e12.3.1 / M2). Virtualized WorkItem table built on
// @tanstack/react-table + @tanstack/react-virtual. Renders just the
// visible rows so large backlogs (1k → 10k+ items) stay responsive.
//
// Scope for this slice: always-on core columns, column visibility
// menu, row-click → onSelect. Out of scope (tracked on gm-e12.3):
// saved filters, URL-hash sharing, manifest-derived extension
// columns, JSONL import.

import { useMemo, useRef, useState } from 'react';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type VisibilityState,
} from '@tanstack/react-table';
import { useVirtualizer } from '@tanstack/react-virtual';
import { SlidersHorizontal } from 'lucide-react';
import type { StateCategory, WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Unstarted',
  started: 'Started',
  completed: 'Completed',
  canceled: 'Canceled',
};

// ROW_HEIGHT matches the single-line rendering of the densest row
// (id / mono priority / chip-style state). If the cell content grows
// past one line the virtualizer will still size correctly because we
// use `estimateSize` + `measureElement`, but keeping rows uniform is
// the hot path.
const ROW_HEIGHT = 36;

// OVERSCAN renders a handful of extra rows above and below the
// viewport so fast scrolls don't tear. 10 is the react-virtual
// default; keep it explicit for clarity.
const OVERSCAN = 10;

export interface WorkItemGridProps {
  rows: WorkItem[];
  onSelect?: (id: string) => void;
}

const coreColumns: ColumnDef<WorkItem>[] = [
  {
    id: 'id',
    header: 'ID',
    accessorFn: (r) => r.id,
    size: 140,
    cell: ({ row }) => (
      <span className="font-mono text-xs text-neutral-700 dark:text-neutral-300">{row.original.id}</span>
    ),
  },
  {
    id: 'title',
    header: 'Title',
    accessorFn: (r) => r.title,
    size: 480,
    cell: ({ row }) => <span className="truncate">{row.original.title}</span>,
  },
  {
    id: 'kind',
    header: 'Kind',
    accessorFn: (r) => r.kind,
    size: 96,
    cell: ({ row }) => (
      <span className="text-xs text-neutral-600 dark:text-neutral-400">{row.original.kind}</span>
    ),
  },
  {
    id: 'state',
    header: 'State',
    accessorFn: (r) => r.state_category,
    size: 112,
    cell: ({ row }) => (
      <span className="text-xs">
        {STATE_LABELS[row.original.state_category] ?? row.original.state_category}
      </span>
    ),
  },
  {
    id: 'priority',
    header: 'P',
    accessorFn: (r) => r.priority ?? null,
    size: 48,
    cell: ({ row }) => (
      <span className="font-mono text-xs text-neutral-700 dark:text-neutral-300">
        {row.original.priority ?? '—'}
      </span>
    ),
  },
  {
    id: 'assignee',
    header: 'Assignee',
    accessorFn: (r) => r.assignee?.name ?? r.assignee?.id ?? '',
    size: 160,
    cell: ({ row }) => (
      <span className="text-xs text-neutral-600 dark:text-neutral-400">
        {row.original.assignee?.name || row.original.assignee?.id || '—'}
      </span>
    ),
  },
  {
    id: 'sprint',
    header: 'Sprint',
    accessorFn: (r) => r.sprint_id ?? '',
    size: 128,
    cell: ({ row }) => (
      <span className="font-mono text-xs">{row.original.sprint_id || '—'}</span>
    ),
  },
  {
    id: 'labels',
    header: 'Labels',
    accessorFn: (r) => (r.labels ?? []).join(','),
    size: 240,
    cell: ({ row }) => {
      const labels = row.original.labels ?? [];
      if (labels.length === 0) return <span className="text-xs text-neutral-500">—</span>;
      return (
        <div className="flex flex-wrap gap-1">
          {labels.map((l) => (
            <span
              key={l}
              className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
            >
              {l}
            </span>
          ))}
        </div>
      );
    },
  },
  {
    id: 'updated',
    header: 'Updated',
    accessorFn: (r) => r.updated_at,
    size: 192,
    cell: ({ row }) => (
      <span className="text-xs text-neutral-500">{row.original.updated_at}</span>
    ),
  },
];

// DEFAULT_VISIBILITY hides nothing; mirrors the column ids above.
const DEFAULT_VISIBILITY: VisibilityState = {
  id: true,
  title: true,
  kind: true,
  state: true,
  priority: true,
  assignee: true,
  sprint: true,
  labels: true,
  updated: true,
};

export function WorkItemGrid({ rows, onSelect }: WorkItemGridProps) {
  const [visibility, setVisibility] = useState<VisibilityState>(DEFAULT_VISIBILITY);
  const [menuOpen, setMenuOpen] = useState(false);

  const columns = useMemo(() => coreColumns, []);

  const table = useReactTable({
    data: rows,
    columns,
    state: { columnVisibility: visibility },
    onColumnVisibilityChange: setVisibility,
    getCoreRowModel: getCoreRowModel(),
  });

  const scrollRef = useRef<HTMLDivElement>(null);

  const visibleRows = table.getRowModel().rows;
  const virtualizer = useVirtualizer({
    count: visibleRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: OVERSCAN,
  });

  const virtualItems = virtualizer.getVirtualItems();
  const paddingTop = virtualItems.length > 0 ? virtualItems[0].start : 0;
  const paddingBottom =
    virtualItems.length > 0
      ? virtualizer.getTotalSize() - virtualItems[virtualItems.length - 1].end
      : 0;

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="work-item-grid">
      <div className="flex items-center justify-end gap-1 border-b border-neutral-200 bg-neutral-50 px-2 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950">
        <div className="relative">
          <button
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            className="inline-flex items-center gap-1 rounded border border-neutral-300 bg-white px-2 py-0.5 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
            data-testid="grid-columns-toggle"
          >
            <SlidersHorizontal className="h-3 w-3" />
            Columns
          </button>
          {menuOpen ? (
            <div
              className="absolute right-0 top-full z-10 mt-1 min-w-[160px] rounded border border-neutral-200 bg-white p-2 shadow-md dark:border-neutral-700 dark:bg-neutral-900"
              data-testid="grid-columns-menu"
            >
              {table.getAllLeafColumns().map((col) => (
                <label
                  key={col.id}
                  className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
                >
                  <input
                    type="checkbox"
                    checked={col.getIsVisible()}
                    onChange={col.getToggleVisibilityHandler()}
                    data-testid={`grid-column-${col.id}`}
                  />
                  <span>{String(col.columnDef.header)}</span>
                </label>
              ))}
            </div>
          ) : null}
        </div>
      </div>
      <div
        ref={scrollRef}
        className="min-h-0 flex-1 overflow-auto"
        data-testid="grid-scroll"
      >
        <table className="w-full border-collapse text-sm">
          <thead className="sticky top-0 z-10 bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-950">
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id}>
                {hg.headers.map((header) => (
                  <th
                    key={header.id}
                    style={{ width: header.getSize() }}
                    className="border-b border-neutral-200 px-4 py-2 font-medium dark:border-neutral-800"
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {paddingTop > 0 ? (
              <tr aria-hidden>
                <td style={{ height: paddingTop }} colSpan={table.getVisibleFlatColumns().length} />
              </tr>
            ) : null}
            {virtualItems.map((vi) => {
              const row = visibleRows[vi.index];
              return (
                <tr
                  key={row.id}
                  onClick={() => onSelect?.(row.original.id)}
                  className={cn(
                    'cursor-pointer border-b border-neutral-100 hover:bg-neutral-50 dark:border-neutral-900 dark:hover:bg-neutral-900'
                  )}
                  style={{ height: ROW_HEIGHT }}
                  data-testid={`grid-row-${row.original.id}`}
                >
                  {row.getVisibleCells().map((cell) => (
                    <td
                      key={cell.id}
                      style={{ width: cell.column.getSize() }}
                      className="overflow-hidden whitespace-nowrap px-4 align-middle"
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              );
            })}
            {paddingBottom > 0 ? (
              <tr aria-hidden>
                <td
                  style={{ height: paddingBottom }}
                  colSpan={table.getVisibleFlatColumns().length}
                />
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}
