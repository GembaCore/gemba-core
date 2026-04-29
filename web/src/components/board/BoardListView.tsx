// BoardListView (gm-e12.19.1). Flat WorkItem list rendering for
// Board's `layout=list` mode. Replaces the standalone BacklogPage —
// same chip-bar, search, and grid renderer, but driven by Board's
// URL search params (?layout=list&view=…&state_category=…) instead
// of the URL-hash + localStorage hybrid the old BacklogPage used.
//
// gm-uipx.17: when `power` is true the list also exposes column
// visibility presets, the bulk-action surface, and the JSONL import
// dialog — the four features that used to distinguish the standalone
// /grid route. Power is a separate URL param (?power=1) so the
// operator can toggle it inside list mode without losing their view /
// chip selections.
//
// Selection drives popDetail({kind: 'workitem', id}) at the BoardPage
// level so the RHP detail tab is owned in one place regardless of layout.

import { useCallback, useMemo, useState } from 'react';
import { Search, Upload } from 'lucide-react';
import { useFilteredWorkItems, useUpdateWorkItem } from '@/hooks/useWorkItems';
import { WorkItemGrid, type BulkAction } from '@/components/grid/WorkItemGrid';
import { BulkEditDialog } from '@/components/grid/BulkEditDialog';
import { JsonlImportDialog } from '@/components/grid/JsonlImportDialog';
import type { WorkItemListFilter, WorkItemPatch } from '@/api/workItems';
import {
  applyView,
  type ViewContext,
  type WorkItemView,
} from '@/lib/workItemViews';
import { lineageIDs, SCOPE_ALL } from '@/components/board/scope';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

// Storage key for the power-mode column-visibility presets. Renamed
// from `gemba.grid.column-presets` to drop the dead-route prefix; an
// operator's old saved presets are not migrated (this is a single-user
// dogfood project — the cost of re-saving once is lower than the
// migration code carrying its own bug surface).
const POWER_PRESETS_STORAGE_KEY = 'gemba.board.column-presets';

const KIND_CHIPS = ['task', 'bug', 'epic'] as const;

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

export interface BoardListViewProps {
  view: WorkItemView | null;
  stateCategories: StateCategory[];
  kinds: string[];
  search: string;
  onChangeStateCategories: (next: StateCategory[]) => void;
  onChangeKinds: (next: string[]) => void;
  onChangeSearch: (next: string) => void;
  onSelectWorkItem: (id: string) => void;
  viewContext?: ViewContext;
  // power lights up the column-presets menu, the bulk-action surface
  // (selection model + Cmd-E/D + context menu), and the JSONL import
  // entry point. Off by default; the toolbar toggle in BoardPage flips
  // it. gm-uipx.17.
  power?: boolean;
  // gm-uekk: optional scope id (e.g. an epic) to narrow the rows to
  // its lineage. SCOPE_ALL or undefined = no narrowing.
  scope?: string;
}

export function BoardListView({
  view,
  stateCategories,
  kinds,
  search,
  onChangeStateCategories,
  onChangeKinds,
  onChangeSearch,
  onSelectWorkItem,
  viewContext,
  power = false,
  scope,
}: BoardListViewProps) {
  const updateWorkItem = useUpdateWorkItem();
  const [importOpen, setImportOpen] = useState(false);
  const [bulkEdit, setBulkEdit] = useState<{ ids: string[] } | null>(null);

  // applyBulkPatch fans the patch out across the selection. Each PATCH
  // gets its own auto-minted nonce (api/workItems.ts) — the server
  // dedupes per-id replays, so a double-applied dialog can't double-
  // apply the change. We don't await the chain; the optimistic cache
  // updates land per-mutation and the list invalidation happens once
  // each mutation settles.
  const applyBulkPatch = useCallback(
    (ids: string[], patch: WorkItemPatch) => {
      for (const id of ids) {
        updateWorkItem.mutate({ id, patch });
      }
    },
    [updateWorkItem]
  );

  const handleBulkAction = useCallback(
    (action: BulkAction, ids: string[]) => {
      if (action === 'edit') {
        setBulkEdit({ ids });
      } else if (action === 'defer') {
        applyBulkPatch(ids, { state_category: 'backlog' });
      } else if (action === 'delete') {
        // No DELETE endpoint on the work-item surface yet; stub by
        // marking canceled so the operator's intent is recorded
        // without dropping data. Replace once a destructive endpoint
        // lands.
        applyBulkPatch(ids, { state_category: 'canceled' });
      }
    },
    [applyBulkPatch]
  );
  // Effective filter = explicit chips, falling back to the named
  // view's base filter when the operator hasn't touched a chip. Once
  // they start toggling chips the URL carries explicit values and
  // the view stops contributing to the API filter (it still drives
  // the post-filter so `mine` / `done-recent` keep working).
  // Memoized so the conditional fallback doesn't re-allocate on every
  // render (which would invalidate the apiFilter useMemo below).
  const effectiveStates = useMemo(
    () =>
      stateCategories.length > 0
        ? stateCategories
        : view
          ? view.baseFilter.state_category
          : [],
    [stateCategories, view]
  );
  const effectiveKinds = useMemo(
    () => (kinds.length > 0 ? kinds : view ? view.baseFilter.kind : []),
    [kinds, view]
  );

  const apiFilter = useMemo<WorkItemListFilter>(() => {
    const f: WorkItemListFilter = {};
    if (effectiveStates.length > 0) f.state_category = effectiveStates;
    if (effectiveKinds.length > 0) f.kind = effectiveKinds;
    return f;
  }, [effectiveStates, effectiveKinds]);

  const { data = [], isLoading, error } = useFilteredWorkItems(apiFilter);

  const filtered = useMemo(() => {
    let rows = applyView(data, view, viewContext ?? {});
    // gm-uekk: scope filtering layers on top of the named view +
    // search box. Lineage is computed against the unfiltered
    // dataset so the scope's tree is reachable even when the named
    // view would have hidden the scope's root from the underlying
    // result.
    if (scope && scope !== SCOPE_ALL) {
      const lineage = lineageIDs(data, scope);
      rows = rows.filter((it) => lineage.has(it.id));
    }
    const needle = search.trim().toLowerCase();
    if (needle) rows = rows.filter((it) => it.title.toLowerCase().includes(needle));
    return rows;
  }, [data, search, view, viewContext, scope]);

  const toggleArrayValue = <T extends string>(arr: T[], value: T): T[] =>
    arr.includes(value) ? arr.filter((v) => v !== value) : [...arr, value];

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="board-list">
      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="board-list-filters"
      >
        <FilterGroup label="State">
          {STATE_CATEGORIES.map((sc) => (
            <Chip
              key={sc}
              active={effectiveStates.includes(sc)}
              onClick={() =>
                onChangeStateCategories(toggleArrayValue(effectiveStates, sc))
              }
              testid={`board-list-state-${sc}`}
            >
              {STATE_LABELS[sc]}
            </Chip>
          ))}
        </FilterGroup>
        <FilterGroup label="Kind">
          {KIND_CHIPS.map((k) => (
            <Chip
              key={k}
              active={effectiveKinds.includes(k)}
              onClick={() => onChangeKinds(toggleArrayValue(effectiveKinds, k))}
              testid={`board-list-kind-${k}`}
            >
              {k}
            </Chip>
          ))}
        </FilterGroup>
        <label className="relative ml-auto flex items-center">
          <Search className="absolute left-2 h-3 w-3 text-neutral-400" aria-hidden />
          <input
            value={search}
            onChange={(e) => onChangeSearch(e.target.value)}
            placeholder="Search titles…"
            className="w-56 rounded border border-neutral-300 bg-white py-1 pl-7 pr-2 text-sm dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
            data-testid="board-list-search"
          />
        </label>
        {power ? (
          <button
            type="button"
            onClick={() => setImportOpen(true)}
            className="inline-flex items-center gap-1 rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
            data-testid="grid-import-jsonl"
          >
            <Upload className="h-3 w-3" />
            Import JSONL
          </button>
        ) : null}
      </div>

      <div
        className="flex items-center justify-between border-b border-neutral-200 px-6 py-2 text-xs text-neutral-500 dark:border-neutral-800"
        data-testid="board-list-count"
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
          <div
            className="p-6 text-sm text-rose-600 dark:text-rose-400"
            data-testid="board-list-error"
          >
            {error.message}
          </div>
        ) : filtered.length === 0 ? (
          <div
            className="p-6 text-sm text-neutral-500"
            data-testid="board-list-empty"
          >
            No work items match the current filters.
          </div>
        ) : (
          <WorkItemGrid
            rows={filtered as WorkItem[]}
            onSelect={onSelectWorkItem}
            presets={power ? { storageKey: POWER_PRESETS_STORAGE_KEY } : undefined}
            onBulkAction={power ? handleBulkAction : undefined}
          />
        )}
      </div>
      {power ? (
        <>
          <JsonlImportDialog
            open={importOpen}
            onClose={() => setImportOpen(false)}
            existing={data}
          />
          <BulkEditDialog
            open={bulkEdit !== null}
            ids={bulkEdit?.ids ?? []}
            onClose={() => setBulkEdit(null)}
            onApply={(patch) => {
              if (bulkEdit) applyBulkPatch(bulkEdit.ids, patch);
              setBulkEdit(null);
            }}
          />
        </>
      ) : null}
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
          ? 'border-sky-700 bg-sky-700 text-white'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
      )}
    >
      {children}
    </button>
  );
}
