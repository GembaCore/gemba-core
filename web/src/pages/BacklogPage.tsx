// BacklogPage (gm-e12.9.1 / M2). Flat filterable list of WorkItems —
// the "planning" surface. Ships ahead of the virtualized grid
// (gm-e12.3) and the full backlog board (gm-e12.9): simple table, no
// virtualization, state_category + kind filter chips, client-side
// title search. Clicking a row opens the shared WorkItemDrawer.
//
// URL is intentionally minimal today: the route is /backlog with no
// query-synced filter state. URL-synced filters + saved presets land
// in follow-up beads once the UI spec settles (gm-p27).

import { useMemo, useState } from 'react';
import { Search } from 'lucide-react';
import { useFilteredWorkItems } from '@/hooks/useWorkItems';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import type { WorkItemListFilter } from '@/api/workItems';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

// Kinds we expose as chips. An adaptor can surface more via the
// CapabilityManifest field_extensions slot once gm-e11.4 is fully
// wired; until then the SPA hard-codes the three common-case kinds
// every adaptor we ship supports.
const KIND_CHIPS = ['task', 'bug', 'epic'] as const;

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Unstarted',
  started: 'Started',
  completed: 'Completed',
  canceled: 'Canceled',
};

export function BacklogPage() {
  const [stateFilter, setStateFilter] = useState<Set<StateCategory>>(
    () => new Set<StateCategory>(['backlog', 'unstarted'])
  );
  const [kindFilter, setKindFilter] = useState<Set<string>>(() => new Set<string>());
  const [search, setSearch] = useState('');
  const [openId, setOpenId] = useState<string | null>(null);

  const filter = useMemo<WorkItemListFilter>(() => {
    const f: WorkItemListFilter = {};
    if (stateFilter.size > 0) {
      f.state_category = [...stateFilter];
    }
    if (kindFilter.size > 0) {
      f.kind = [...kindFilter];
    }
    return f;
  }, [stateFilter, kindFilter]);

  const { data = [], isLoading, error } = useFilteredWorkItems(filter);

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return data;
    return data.filter((it) => it.title.toLowerCase().includes(needle));
  }, [data, search]);

  const toggle = <T,>(set: Set<T>, value: T): Set<T> => {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    return next;
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold tracking-tight">Backlog</h1>
        <p className="mt-1 text-sm text-neutral-500">
          Filterable list of every WorkItem the bound adaptor exposes.
        </p>
      </header>

      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="backlog-filters"
      >
        <FilterGroup label="State">
          {STATE_CATEGORIES.map((sc) => (
            <Chip
              key={sc}
              active={stateFilter.has(sc)}
              onClick={() => setStateFilter((s) => toggle(s, sc))}
              testid={`backlog-state-${sc}`}
            >
              {STATE_LABELS[sc]}
            </Chip>
          ))}
        </FilterGroup>
        <FilterGroup label="Kind">
          {KIND_CHIPS.map((k) => (
            <Chip
              key={k}
              active={kindFilter.has(k)}
              onClick={() => setKindFilter((s) => toggle(s, k))}
              testid={`backlog-kind-${k}`}
            >
              {k}
            </Chip>
          ))}
        </FilterGroup>
        <label className="relative ml-auto flex items-center">
          <Search className="absolute left-2 h-3 w-3 text-neutral-400" aria-hidden />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search titles…"
            className="w-56 rounded border border-neutral-300 bg-white py-1 pl-7 pr-2 text-sm dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
            data-testid="backlog-search"
          />
        </label>
      </div>

      <div
        className="flex items-center justify-between border-b border-neutral-200 px-6 py-2 text-xs text-neutral-500 dark:border-neutral-800"
        data-testid="backlog-count"
      >
        <span>
          {data.length} item{data.length === 1 ? '' : 's'}
          {filtered.length !== data.length ? ` · ${filtered.length} shown` : null}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {isLoading ? (
          <div className="p-6 text-sm text-neutral-500">Loading…</div>
        ) : error ? (
          <div className="p-6 text-sm text-rose-600 dark:text-rose-400" data-testid="backlog-error">
            {error.message}
          </div>
        ) : filtered.length === 0 ? (
          <div className="p-6 text-sm text-neutral-500" data-testid="backlog-empty">
            No work items match the current filters.
          </div>
        ) : (
          <BacklogTable rows={filtered} onSelect={setOpenId} />
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

function BacklogTable({ rows, onSelect }: { rows: WorkItem[]; onSelect: (id: string) => void }) {
  return (
    <table className="w-full border-collapse text-sm" data-testid="backlog-table">
      <thead className="sticky top-0 bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-950">
        <tr>
          <Th className="w-32">ID</Th>
          <Th>Title</Th>
          <Th className="w-24">Kind</Th>
          <Th className="w-28">State</Th>
          <Th className="w-14">P</Th>
          <Th className="w-32">Assignee</Th>
          <Th className="w-32">Sprint</Th>
          <Th className="w-44">Updated</Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr
            key={r.id}
            onClick={() => onSelect(r.id)}
            className="cursor-pointer border-b border-neutral-100 hover:bg-neutral-50 dark:border-neutral-900 dark:hover:bg-neutral-900"
            data-testid={`backlog-row-${r.id}`}
          >
            <Td className="font-mono text-xs">{r.id}</Td>
            <Td className="truncate">{r.title}</Td>
            <Td className="text-xs text-neutral-600 dark:text-neutral-400">{r.kind}</Td>
            <Td className="text-xs">{STATE_LABELS[r.state_category] ?? r.state_category}</Td>
            <Td className="font-mono text-xs">{r.priority ?? '—'}</Td>
            <Td className="text-xs text-neutral-600 dark:text-neutral-400">
              {r.assignee?.name || r.assignee?.id || '—'}
            </Td>
            <Td className="font-mono text-xs">{r.sprint_id || '—'}</Td>
            <Td className="text-xs text-neutral-500">{r.updated_at}</Td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
  return <th className={cn('px-4 py-2 font-medium', className)}>{children}</th>;
}

function Td({ children, className }: { children: React.ReactNode; className?: string }) {
  return <td className={cn('px-4 py-2 align-top', className)}>{children}</td>;
}
