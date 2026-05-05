// /refine — dedicated triage + refinement surface (gm-3ofd).
//
// Hardwired filter: state_category=backlog. Default sort: priority desc,
// then age desc (older items bubble up inside a priority band).
// Power features (bulk-action surface, column presets, JSONL import)
// are always on — refinement is a dense triage activity by definition.
//
// Reuses WorkItemGrid; refine-specific columns + actions land in
// follow-up children (see docs/design/refine.md).

import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { GitBranch, LayoutGrid, Search, TableProperties } from 'lucide-react';
import { WorkItemGrid, type BulkAction } from '@/components/grid/WorkItemGrid';
import { BulkEditDialog } from '@/components/grid/BulkEditDialog';
import { EpicPickerDialog } from '@/components/refine/EpicPickerDialog';
import { useFilteredWorkItems, useUpdateWorkItem, useWorkItems } from '@/hooks/useWorkItems';
import type { WorkItemPatch } from '@/api/workItems';
import { useRhp } from '@/components/rhp/RhpContext';
import { useSearchParams } from 'react-router-dom';
import type { WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';
import { BeadsCascadeView } from '@/components/board/BeadsCascadeView';
import { EpicView } from '@/components/board/EpicView';
import { DndContext } from '@dnd-kit/core';

const REFINE_PRESETS_STORAGE_KEY = 'gemba.refine.column-presets';
const DEFER_LABEL_PREFIX = 'defer-until:';
const REFINE_VIEW_PARAM = 'view';
type RefineView = 'table' | 'hierarchy' | 'swimlanes';

function refineViewFromParams(params: URLSearchParams): RefineView {
  const raw = params.get(REFINE_VIEW_PARAM);
  if (raw === 'hierarchy' || raw === 'swimlanes') return raw;
  return 'table';
}

// /refine surfaces the refine-specific columns (gm-51i2). The grid hides
// these globally so the Board's list mode stays lean; this override
// flips them back on for /refine only.
const REFINE_VISIBILITY = {
  age: true,
  suggested_epic: true,
  blockers: true,
  dispatch_status: true,
};

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

// stripDeferUntil removes any existing defer-until:* labels — only the
// prefix-with-colon form, never a substring match — so we never leak a
// stale defer marker when the operator picks a new date.
function stripDeferUntil(labels: string[] | undefined): string[] {
  return (labels ?? []).filter((l) => !l.startsWith(DEFER_LABEL_PREFIX));
}

export function RefinePage() {
  const [params, setParams] = useSearchParams();
  const view = refineViewFromParams(params);
  const search = params.get('q') ?? '';
  const setView = useCallback(
    (next: RefineView) => {
      const p = new URLSearchParams(params);
      if (next === 'table') p.delete(REFINE_VIEW_PARAM);
      else p.set(REFINE_VIEW_PARAM, next);
      setParams(p, { replace: true });
    },
    [params, setParams]
  );
  const onChangeSearch = useCallback(
    (next: string) => {
      const p = new URLSearchParams(params);
      if (next) p.set('q', next);
      else p.delete('q');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  const { popDetail } = useRhp();

  const { data = [], isLoading, error } = useFilteredWorkItems({
    state_category: ['backlog'],
  });
  const allItems = useWorkItems();

  const updateWorkItem = useUpdateWorkItem();
  const [bulkEdit, setBulkEdit] = useState<{ ids: string[] } | null>(null);
  const [epicPick, setEpicPick] = useState<{ ids: string[] } | null>(null);
  const [deferTarget, setDeferTarget] = useState<{ ids: string[] } | null>(null);
  const [dismissTarget, setDismissTarget] = useState<{ ids: string[] } | null>(null);

  const applyBulkPatch = useCallback(
    (ids: string[], patch: WorkItemPatch) => {
      for (const id of ids) {
        updateWorkItem.mutate({ id, patch });
      }
    },
    [updateWorkItem]
  );

  // applyDefer writes a defer-until:<iso> label per id, stripping any
  // prior defer-until:* marker so the row carries exactly one. An empty
  // iso is a no-op (rows are already backlog; nothing to record).
  const applyDefer = useCallback(
    (ids: string[], iso: string) => {
      if (!iso) return;
      for (const id of ids) {
        const row = data.find((r) => r.id === id);
        if (!row) continue;
        const next = [...stripDeferUntil(row.labels), `${DEFER_LABEL_PREFIX}${iso}`];
        updateWorkItem.mutate({ id, patch: { labels: next } });
      }
    },
    [data, updateWorkItem]
  );

  // applyDismiss flips state_category → canceled. The optional reason is
  // captured but not currently sent — WorkItemPatch has no notes-append
  // field, and the bead's spec says ship the state-change without it
  // rather than invent a backend feature. TODO(gm-mw5n): pipe reason
  // through once an append-notes patch path lands.
  const applyDismiss = useCallback(
    (ids: string[], _reason: string) => {
      for (const id of ids) {
        updateWorkItem.mutate({ id, patch: { state_category: 'canceled' } });
      }
    },
    [updateWorkItem]
  );

  const handleBulkAction = useCallback(
    (action: BulkAction, ids: string[]) => {
      if (action === 'edit') {
        setBulkEdit({ ids });
      } else if (action === 'defer') {
        setDeferTarget({ ids });
      } else if (action === 'dismiss' || action === 'delete') {
        // Re-purpose 'delete' as a dismiss alias — /refine never wants
        // to actually drop data, only to cancel with a recorded reason.
        setDismissTarget({ ids });
      } else if (action === 'drop-into-epic') {
        setEpicPick({ ids });
      }
    },
    []
  );

  const rows = useMemo(() => {
    let r = refineDefaultSort(data);
    const needle = search.trim().toLowerCase();
    if (needle) r = r.filter((it) => it.title.toLowerCase().includes(needle));
    return r;
  }, [data, search]);
  const planningRows = useMemo(() => {
    let r = refineDefaultSort(allItems.data ?? []);
    const needle = search.trim().toLowerCase();
    if (needle) {
      r = r.filter(
        (it) =>
          it.title.toLowerCase().includes(needle) ||
          it.id.toLowerCase().includes(needle) ||
          it.kind.toLowerCase().includes(needle)
      );
    }
    return r;
  }, [allItems.data, search]);

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="refine-page">
      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="refine-toolbar"
      >
        <h1 className="text-sm font-semibold tracking-tight">Refine</h1>
        <span className="text-neutral-500" data-testid="refine-row-count">
          {(view === 'table' ? rows : planningRows).length} item
          {(view === 'table' ? rows : planningRows).length === 1 ? '' : 's'}
        </span>
        <div className="flex items-center gap-1">
          <RefineViewButton
            active={view === 'table'}
            onClick={() => setView('table')}
            label="Table"
            icon={<TableProperties className="h-3.5 w-3.5" />}
            testid="refine-view-table"
          />
          <RefineViewButton
            active={view === 'hierarchy'}
            onClick={() => setView('hierarchy')}
            label="Hierarchy"
            icon={<GitBranch className="h-3.5 w-3.5" />}
            testid="refine-view-hierarchy"
          />
          <RefineViewButton
            active={view === 'swimlanes'}
            onClick={() => setView('swimlanes')}
            label="Swimlanes"
            icon={<LayoutGrid className="h-3.5 w-3.5" />}
            testid="refine-view-swimlanes"
          />
        </div>
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
        {view === 'table' && isLoading ? (
          <RefineSkeleton />
        ) : view === 'table' && error ? (
          <RefineError message={error.message} />
        ) : view === 'table' && rows.length === 0 ? (
          <RefineEmpty hasSearch={!!search.trim()} />
        ) : view === 'table' ? (
          <WorkItemGrid
            rows={rows}
            onSelect={(id) => popDetail({ kind: 'workitem', id })}
            onBulkAction={handleBulkAction}
            presets={{ storageKey: REFINE_PRESETS_STORAGE_KEY }}
            visibilityOverride={REFINE_VISIBILITY}
          />
        ) : allItems.isLoading ? (
          <RefineSkeleton />
        ) : allItems.error ? (
          <RefineError message={allItems.error.message} />
        ) : planningRows.length === 0 ? (
          <RefineEmpty hasSearch={!!search.trim()} />
        ) : view === 'hierarchy' ? (
          <BeadsCascadeView
            items={planningRows}
            orderKey="modified"
            onSelect={(item) => {
              if (item.kind === 'epic') popDetail({ kind: 'epic', id: item.id });
              else popDetail({ kind: 'workitem', id: item.id });
            }}
          />
        ) : (
          <DndContext>
            <EpicView
              items={planningRows}
              onSelectEpic={(id) => popDetail({ kind: 'epic', id })}
              showBacklog
              orderKey="modified"
            />
          </DndContext>
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

      {deferTarget && (
        <DeferDialog
          ids={deferTarget.ids}
          onClose={() => setDeferTarget(null)}
          onApply={(iso) => {
            applyDefer(deferTarget.ids, iso);
            setDeferTarget(null);
          }}
        />
      )}

      {dismissTarget && (
        <DismissDialog
          ids={dismissTarget.ids}
          onClose={() => setDismissTarget(null)}
          onApply={(reason) => {
            applyDismiss(dismissTarget.ids, reason);
            setDismissTarget(null);
          }}
        />
      )}

    </div>
  );
}

interface RefineViewButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  icon: ReactNode;
  testid: string;
}

function RefineViewButton({ active, onClick, label, icon, testid }: RefineViewButtonProps) {
  return (
    <button
      type="button"
      data-testid={testid}
      data-active={active}
      onClick={onClick}
      className={cn(
        'inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs',
        active
          ? 'border-sky-700 bg-sky-700 text-white dark:border-sky-500 dark:bg-sky-600'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800'
      )}
    >
      {icon}
      {label}
    </button>
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

// DeferDialog — date input + Confirm/Cancel. Empty date → caller skips
// the patch, so confirming an empty form is silently a no-op.
interface DeferDialogProps {
  ids: string[];
  onClose: () => void;
  onApply: (iso: string) => void;
}
function DeferDialog({ ids, onClose, onApply }: DeferDialogProps) {
  const [date, setDate] = useState<string>('');

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Defer work items"
      data-testid="refine-defer-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-sm rounded-lg border border-neutral-200 bg-white p-5 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="mb-3">
          <h2 className="text-base font-semibold">Defer</h2>
          <p className="mt-0.5 text-xs text-neutral-500">
            Marking <span data-testid="refine-defer-count">{ids.length}</span> item
            {ids.length === 1 ? '' : 's'} with a defer-until date.
          </p>
        </header>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs font-medium text-neutral-700 dark:text-neutral-300">
            Revisit on
          </span>
          <input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            data-testid="refine-defer-date"
            className="rounded border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-900"
          />
        </label>

        <footer className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="refine-defer-cancel"
            className="rounded border border-neutral-300 bg-white px-3 py-1 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onApply(date.trim())}
            data-testid="refine-defer-confirm"
            className="rounded bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-700"
          >
            Confirm
          </button>
        </footer>
      </div>
    </div>
  );
}

// DismissDialog — optional reason textarea + Confirm/Cancel. Reason is
// captured but not sent today (see applyDismiss TODO).
interface DismissDialogProps {
  ids: string[];
  onClose: () => void;
  onApply: (reason: string) => void;
}
function DismissDialog({ ids, onClose, onApply }: DismissDialogProps) {
  const [reason, setReason] = useState<string>('');

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Dismiss work items"
      data-testid="refine-dismiss-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-5 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="mb-3">
          <h2 className="text-base font-semibold">Dismiss</h2>
          <p className="mt-0.5 text-xs text-neutral-500">
            Cancelling <span data-testid="refine-dismiss-count">{ids.length}</span> item
            {ids.length === 1 ? '' : 's'}. Reason is optional.
          </p>
        </header>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs font-medium text-neutral-700 dark:text-neutral-300">
            Reason (optional)
          </span>
          <textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            data-testid="refine-dismiss-reason"
            rows={3}
            className="resize-none rounded border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-900"
          />
        </label>

        <footer className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="refine-dismiss-cancel"
            className="rounded border border-neutral-300 bg-white px-3 py-1 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onApply(reason.trim())}
            data-testid="refine-dismiss-confirm"
            className="rounded bg-rose-600 px-3 py-1 text-sm font-medium text-white hover:bg-rose-700"
          >
            Confirm
          </button>
        </footer>
      </div>
    </div>
  );
}
