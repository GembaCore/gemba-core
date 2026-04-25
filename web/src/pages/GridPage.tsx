// GridPage (gm-e12.3.3). Power-user view of the WorkItemGrid with
// column visibility presets enabled. Shares the filter chip bar +
// persistence mechanism with BacklogPage but uses its own storage
// keys so the two views remember their own last-used state.
//
// gm-5v8v.6.3: ui-spec §6.8 named default views — Staged / In
// Progress / Blocked / Ready to stage / Recently Done — exposed via
// a view-picker chip group above the existing filter chips. Active
// view persists in the URL hash (?view=...) and localStorage.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Search, Upload } from 'lucide-react';
import { useFilteredWorkItems, useUpdateWorkItem } from '@/hooks/useWorkItems';
import { usePersistedFilter } from '@/hooks/usePersistedFilter';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { WorkItemGrid, type BulkAction } from '@/components/grid/WorkItemGrid';
import { JsonlImportDialog } from '@/components/grid/JsonlImportDialog';
import { BulkEditDialog } from '@/components/grid/BulkEditDialog';
import type { WorkItemListFilter, WorkItemPatch } from '@/api/workItems';
import type { BacklogFilter } from '@/lib/backlogFilter';
import {
  NAMED_VIEWS,
  VIEW_FILTERS,
  VIEW_LABELS,
  VIEW_POST_FILTERS,
  VIEW_SORTS,
  isKnownView,
  type NamedView,
} from '@/lib/gridViews';
import { STATE_CATEGORIES, type StateCategory } from '@/types/core.gen';
import { cn } from '@/lib/utils';

const FILTER_STORAGE_KEY = 'gemba.grid.filter';
const PRESETS_STORAGE_KEY = 'gemba.grid.column-presets';
const VIEW_STORAGE_KEY = 'gemba.grid.view';
const VIEW_QUERY_PARAM = 'view';

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
  const [activeView, setActiveView] = useState<NamedView | null>(initialViewFromEnv);
  const [openId, setOpenId] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [bulkEdit, setBulkEdit] = useState<{ ids: string[] } | null>(null);
  const updateWorkItem = useUpdateWorkItem();

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
    [updateWorkItem],
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
    [applyBulkPatch],
  );

  // Keep the URL `?view=` param + localStorage entry in sync with the
  // active view. URL wins on first paint (handled by initialViewFromEnv);
  // every subsequent change writes both sinks. window.history.replaceState
  // (not pushState) so toggling views doesn't pollute browser history.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const url = new URL(window.location.href);
    if (activeView) {
      url.searchParams.set(VIEW_QUERY_PARAM, activeView);
      safeSet(VIEW_STORAGE_KEY, activeView);
    } else {
      url.searchParams.delete(VIEW_QUERY_PARAM);
      safeSet(VIEW_STORAGE_KEY, '');
    }
    if (url.toString() !== window.location.href) {
      window.history.replaceState(null, '', url.toString());
    }
  }, [activeView]);

  const applyView = useCallback(
    (next: NamedView | null) => {
      if (next === null) {
        // Clearing the view also drops the filter so the operator
        // sees the full unfiltered set — clicking a view chip a
        // second time is the natural "show me everything" gesture.
        setActiveView(null);
        setFilter({ state_category: [], kind: [], search: filter.search });
        return;
      }
      // Each view ships a base filter preset; the search box is
      // preserved across view switches so a typed query keeps the
      // operator's working set intact.
      const preset = VIEW_FILTERS[next];
      setFilter({ ...preset, search: filter.search });
      setActiveView(next);
    },
    [filter.search, setFilter]
  );

  // First-mount: if the URL or storage seeded an active view, apply
  // its base filter preset so the visible rows match what the chip
  // promised. Without this, ?view=staged would light up the chip but
  // leave the filter at its default-staged-state default and nothing
  // would render.
  const mountedRef = useRef(false);
  useEffect(() => {
    if (mountedRef.current) return;
    mountedRef.current = true;
    if (activeView !== null) {
      const preset = VIEW_FILTERS[activeView];
      setFilter({ ...preset, search: filter.search });
    }
    // Run once at mount; activeView/filter changes after this go
    // through applyView / patch / patchSearch which already handle
    // state correctly.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const apiFilter = useMemo<WorkItemListFilter>(() => {
    const f: WorkItemListFilter = {};
    if (filter.state_category.length > 0) f.state_category = filter.state_category;
    if (filter.kind.length > 0) f.kind = filter.kind;
    return f;
  }, [filter.state_category, filter.kind]);

  const { data = [], isLoading, error } = useFilteredWorkItems(apiFilter);

  const rows = useMemo(() => {
    const needle = filter.search.trim().toLowerCase();
    let out = needle ? data.filter((it) => it.title.toLowerCase().includes(needle)) : data;
    if (activeView) {
      const post = VIEW_POST_FILTERS[activeView];
      if (post) out = out.filter(post);
      const sort = VIEW_SORTS[activeView];
      if (sort) out = [...out].sort(sort);
    }
    return out;
  }, [data, filter.search, activeView]);

  // A manual chip toggle (state, kind) clears the active view since
  // the operator is hand-tuning the filter. The view picker continues
  // to highlight whichever preset most-recently won.
  const patch = (p: Partial<BacklogFilter>) => {
    setActiveView(null);
    setFilter({ ...filter, ...p });
  };
  const patchSearch = (search: string) => setFilter({ ...filter, search });
  const toggle = <T extends string>(arr: T[], v: T): T[] =>
    arr.includes(v) ? arr.filter((x) => x !== v) : [...arr, v];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-start justify-between border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Grid</h1>
          <p className="mt-1 text-sm text-neutral-500">
            Virtualized power-user view. Use presets to swap column layouts without retoggling.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setImportOpen(true)}
          className="inline-flex items-center gap-1 rounded-md border border-neutral-300 bg-white px-3 py-1.5 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          data-testid="grid-import-jsonl"
        >
          <Upload className="h-3.5 w-3.5" />
          Import JSONL
        </button>
      </header>

      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="grid-views"
      >
        <FilterGroup label="View">
          {NAMED_VIEWS.map((v) => (
            <Chip
              key={v}
              active={activeView === v}
              onClick={() => applyView(activeView === v ? null : v)}
              testid={`grid-view-${v}`}
            >
              {VIEW_LABELS[v]}
            </Chip>
          ))}
        </FilterGroup>
      </div>
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
            onChange={(e) => patchSearch(e.target.value)}
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
            onBulkAction={handleBulkAction}
          />
        )}
      </div>

      <WorkItemDrawer openId={openId} onClose={() => setOpenId(null)} />
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
    </div>
  );
}

// initialViewFromEnv resolves the starting view at mount time:
// URL ?view= wins (shareable links), falling back to localStorage,
// then null (no view active). Mirrors usePersistedFilter's priority.
function initialViewFromEnv(): NamedView | null {
  if (typeof window === 'undefined') return null;
  const url = new URL(window.location.href);
  const fromQuery = isKnownView(url.searchParams.get(VIEW_QUERY_PARAM));
  if (fromQuery) return fromQuery;
  return isKnownView(safeGet(VIEW_STORAGE_KEY));
}

function safeGet(key: string): string | null {
  try {
    return typeof window !== 'undefined' ? window.localStorage.getItem(key) : null;
  } catch {
    return null;
  }
}

function safeSet(key: string, value: string): void {
  try {
    if (typeof window !== 'undefined') {
      if (value) window.localStorage.setItem(key, value);
      else window.localStorage.removeItem(key);
    }
  } catch {
    /* see safeGet */
  }
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
