// /refine — dedicated triage + refinement surface (gm-3ofd).
//
// Hardwired filter: state_category=backlog. Default sort: priority desc,
// then age desc (older items bubble up inside a priority band).
// Power features (bulk-action surface, column presets, JSONL import)
// are always on — refinement is a dense triage activity by definition.
//
// Reuses WorkItemGrid; refine-specific columns + actions land in
// follow-up children (see docs/design/refine.md).

import { useCallback, useMemo, useState } from 'react';
import { Search } from 'lucide-react';
import { WorkItemGrid, type BulkAction } from '@/components/grid/WorkItemGrid';
import { BulkEditDialog } from '@/components/grid/BulkEditDialog';
import { EpicPickerDialog } from '@/components/refine/EpicPickerDialog';
import { useFilteredWorkItems, useUpdateWorkItem } from '@/hooks/useWorkItems';
import type { WorkItemPatch } from '@/api/workItems';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { useSearchParams } from 'react-router-dom';
import type { WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

const REFINE_PRESETS_STORAGE_KEY = 'gemba.refine.column-presets';

// Backlog rows sort by priority (lower number = higher) then by age
// (older = updated_at ascending so it sinks to the top after the
// reverse below). Stable sort.
function refineDefaultSort(rows: WorkItem[]): WorkItem[] {
  const out = rows.slice();
  out.sort((a, b) => {
    const pa = a.priority ?? 99;
    const pb = b.priority ?? 99;
    if (pa !== pb) return pa - pb;
    // older first within priority band
    const ta = Date.parse(a.created_at) || 0;
    const tb = Date.parse(b.created_at) || 0;
    return ta - tb;
  });
  return out;
}

export function RefinePage() {
  const [params, setParams] = useSearchParams();
  const search = params.get('q') ?? '';
  const onChangeSearch = useCallback(
    (next: string) => {
      const p = new URLSearchParams(params);
      if (next) p.set('q', next);
      else p.delete('q');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  const openId = params.get('bead');
  const setOpenId = useCallback(
    (next: string | null) => {
      const p = new URLSearchParams(params);
      if (next) p.set('bead', next);
      else p.delete('bead');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  const { data = [], isLoading, error } = useFilteredWorkItems({
    state_category: ['backlog'],
  });

  const updateWorkItem = useUpdateWorkItem();
  const [bulkEdit, setBulkEdit] = useState<{ ids: string[] } | null>(null);
  const [epicPick, setEpicPick] = useState<{ ids: string[] } | null>(null);

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
        // /refine rows are already backlog — defer is a no-op here.
        // Triage-specific defer (with defer-until note) ships in a
        // follow-up child.
      } else if (action === 'delete') {
        applyBulkPatch(ids, { state_category: 'canceled' });
      } else if (action === 'drop-into-epic') {
        setEpicPick({ ids });
      }
    },
    [applyBulkPatch]
  );

  const rows = useMemo(() => {
    let r = refineDefaultSort(data);
    const needle = search.trim().toLowerCase();
    if (needle) r = r.filter((it) => it.title.toLowerCase().includes(needle));
    return r;
  }, [data, search]);

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="refine-page">
      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="refine-toolbar"
      >
        <h1 className="text-sm font-semibold tracking-tight">Refine</h1>
        <span className="text-neutral-500" data-testid="refine-row-count">
          {rows.length} item{rows.length === 1 ? '' : 's'}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <label className="relative">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-neutral-500" />
            <input
              data-testid="refine-search"
              type="search"
              placeholder="Search title…"
              value={search}
              onChange={(e) => onChangeSearch(e.target.value)}
              className={cn(
                'h-7 w-56 rounded-md border border-neutral-300 bg-white pl-7 pr-2 text-xs',
                'dark:border-neutral-700 dark:bg-neutral-900'
              )}
            />
          </label>
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-auto">
        {isLoading ? (
          <RefineSkeleton />
        ) : error ? (
          <RefineError message={error.message} />
        ) : rows.length === 0 ? (
          <RefineEmpty hasSearch={!!search.trim()} />
        ) : (
          <WorkItemGrid
            rows={rows}
            onSelect={setOpenId}
            onBulkAction={handleBulkAction}
            presets={{ storageKey: REFINE_PRESETS_STORAGE_KEY }}
          />
        )}
      </div>

      {bulkEdit && (
        <BulkEditDialog
          open
          ids={bulkEdit.ids}
          onClose={() => setBulkEdit(null)}
          onApply={(patch) => {
            applyBulkPatch(bulkEdit.ids, patch);
            setBulkEdit(null);
          }}
        />
      )}

      {epicPick && (
        <EpicPickerDialog
          open
          ids={epicPick.ids}
          onClose={() => setEpicPick(null)}
          onPick={(epicId) => {
            applyBulkPatch(epicPick.ids, { parent_id: epicId });
            setEpicPick(null);
          }}
        />
      )}

      <WorkItemDrawer openId={openId} onClose={() => setOpenId(null)} />
    </div>
  );
}

function RefineSkeleton() {
  return (
    <div className="p-6 text-xs text-neutral-500" data-testid="refine-loading">
      Loading backlog…
    </div>
  );
}

function RefineError({ message }: { message: string }) {
  return (
    <div className="p-6 text-xs text-rose-700 dark:text-rose-300" data-testid="refine-error">
      {message}
    </div>
  );
}

function RefineEmpty({ hasSearch }: { hasSearch: boolean }) {
  return (
    <div
      className="flex h-full items-center justify-center p-6 text-xs text-neutral-500"
      data-testid="refine-empty"
    >
      {hasSearch
        ? 'No backlog items match the search.'
        : 'Nothing in the backlog. New items land here on filing.'}
    </div>
  );
}
