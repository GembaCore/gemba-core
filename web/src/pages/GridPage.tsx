// GridPage (gm-e12.3.3). Power-user view of the WorkItemGrid with
// column visibility presets enabled. Shares the filter chip bar +
// persistence mechanism with BacklogPage but uses its own storage
// keys so the two views remember their own last-used state.

import { useMemo, useState } from 'react';
import { Search } from 'lucide-react';
import { useFilteredWorkItems } from '@/hooks/useWorkItems';
import { usePersistedFilter } from '@/hooks/usePersistedFilter';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { WorkItemGrid } from '@/components/grid/WorkItemGrid';
import type { WorkItemListFilter } from '@/api/workItems';
import type { BacklogFilter } from '@/lib/backlogFilter';
import { STATE_CATEGORIES, type StateCategory } from '@/types/core.gen';
import { cn } from '@/lib/utils';

const FILTER_STORAGE_KEY = 'gemba.grid.filter';
const PRESETS_STORAGE_KEY = 'gemba.grid.column-presets';

const KIND_CHIPS = ['task', 'bug', 'epic'] as const;

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

export function GridPage() {
  const [filter, setFilter] = usePersistedFilter(FILTER_STORAGE_KEY);
  const [openId, setOpenId] = useState<string | null>(null);

  const apiFilter = useMemo<WorkItemListFilter>(() => {
    const f: WorkItemListFilter = {};
    if (filter.state_category.length > 0) f.state_category = filter.state_category;
    if (filter.kind.length > 0) f.kind = filter.kind;
    return f;
  }, [filter.state_category, filter.kind]);

  const { data = [], isLoading, error } = useFilteredWorkItems(apiFilter);

  const rows = useMemo(() => {
    const needle = filter.search.trim().toLowerCase();
    if (!needle) return data;
    return data.filter((it) => it.title.toLowerCase().includes(needle));
  }, [data, filter.search]);

  const patch = (p: Partial<BacklogFilter>) => setFilter({ ...filter, ...p });
  const toggle = <T extends string>(arr: T[], v: T): T[] =>
    arr.includes(v) ? arr.filter((x) => x !== v) : [...arr, v];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold tracking-tight">Grid</h1>
        <p className="mt-1 text-sm text-neutral-500">
          Virtualized power-user view. Use presets to swap column layouts without retoggling.
        </p>
      </header>

      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="grid-filters"
      >
        <FilterGroup label="State">
          {STATE_CATEGORIES.map((sc) => (
            <Chip
              key={sc}
              active={filter.state_category.includes(sc)}
              onClick={() => patch({ state_category: toggle(filter.state_category, sc) })}
              testid={`grid-state-${sc}`}
            >
              {STATE_LABELS[sc]}
            </Chip>
          ))}
        </FilterGroup>
        <FilterGroup label="Kind">
          {KIND_CHIPS.map((k) => (
            <Chip
              key={k}
              active={filter.kind.includes(k)}
              onClick={() => patch({ kind: toggle(filter.kind, k) })}
              testid={`grid-kind-${k}`}
            >
              {k}
            </Chip>
          ))}
        </FilterGroup>
        <label className="relative ml-auto flex items-center">
          <Search className="absolute left-2 h-3 w-3 text-neutral-400" aria-hidden />
          <input
            value={filter.search}
            onChange={(e) => patch({ search: e.target.value })}
            placeholder="Search titles…"
            className="w-56 rounded border border-neutral-300 bg-white py-1 pl-7 pr-2 text-sm dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
            data-testid="grid-search"
          />
        </label>
      </div>

      <div
        className="flex items-center justify-between border-b border-neutral-200 px-6 py-2 text-xs text-neutral-500 dark:border-neutral-800"
        data-testid="grid-count"
      >
        <span>
          {data.length} item{data.length === 1 ? '' : 's'}
          {rows.length !== data.length ? ` · ${rows.length} shown` : null}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {isLoading ? (
          <div className="p-6 text-sm text-neutral-500">Loading…</div>
        ) : error ? (
          <div className="p-6 text-sm text-rose-600 dark:text-rose-400" data-testid="grid-error">
            {error.message}
          </div>
        ) : rows.length === 0 ? (
          <div className="p-6 text-sm text-neutral-500" data-testid="grid-empty">
            No work items match the current filters.
          </div>
        ) : (
          <WorkItemGrid
            rows={rows}
            onSelect={setOpenId}
            presets={{ storageKey: PRESETS_STORAGE_KEY }}
          />
        )}
      </div>

      <WorkItemDrawer openId={openId} onClose={() => setOpenId(null)} />
    </div>
  );
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-1">
      <span className="mr-1 text-neutral-500">{label}:</span>
      {children}
    </div>
  );
}

function Chip({
  active,
  onClick,
  children,
  testid,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
  testid?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={testid}
      data-active={active || undefined}
      className={cn(
        'rounded border px-2 py-0.5 text-xs transition-colors',
        active
          ? 'border-sky-500 bg-sky-500 text-white'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
      )}
    >
      {children}
    </button>
  );
}
