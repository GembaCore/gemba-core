// EpicPickerDialog (gm-ju5o). Modal picker that lists every WorkItem
// with kind === 'epic' and resolves with the chosen epic id. The
// caller fans the id out across one or more updateWorkItem({ parent_id })
// patches; this component is intentionally semantics-free so single-row
// and bulk callers share the same UI.
//
// The dialog renders its own overlay (mirrors BulkEditDialog) so it
// composes inside the WorkItemGrid surface without fighting any parent
// portal / drawer stacking.

import { useEffect, useMemo, useState } from 'react';
import { useFilteredWorkItems } from '@/hooks/useWorkItems';
import type { WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

export interface EpicPickerDialogProps {
  open: boolean;
  // Plural for symmetry — single-row callers pass a one-element array.
  ids: string[];
  onClose: () => void;
  onPick: (epicId: string) => void;
}

export function EpicPickerDialog({ open, ids, onClose, onPick }: EpicPickerDialogProps) {
  const { data = [], isLoading, error } = useFilteredWorkItems({ kind: ['epic'] });
  const [filter, setFilter] = useState('');

  useEffect(() => {
    if (open) setFilter('');
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  const epics = useMemo(() => {
    // Defensive: server filter is the source of truth, but a stale
    // cache on the unfiltered list path could leak non-epics through
    // — re-check kind here so the picker NEVER offers a non-epic.
    const onlyEpics = data.filter((w) => w.kind === 'epic');
    const needle = filter.trim().toLowerCase();
    if (!needle) return onlyEpics;
    return onlyEpics.filter(
      (e) =>
        e.title.toLowerCase().includes(needle) ||
        e.id.toLowerCase().includes(needle),
    );
  }, [data, filter]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Drop into epic"
      data-testid="epic-picker-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-5 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="mb-3">
          <h2 className="text-sm font-semibold tracking-tight">
            Drop {ids.length} into epic
          </h2>
          <p className="mt-0.5 text-xs text-neutral-500">
            Pick the target epic. The selected items will be re-parented
            via parent_id.
          </p>
        </header>

        <input
          type="search"
          autoFocus
          placeholder="Filter epics by title or id…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          data-testid="epic-picker-filter"
          className={cn(
            'mb-2 h-7 w-full rounded-md border border-neutral-300 bg-white px-2 text-xs',
            'dark:border-neutral-700 dark:bg-neutral-900',
          )}
        />

        <div
          role="listbox"
          aria-label="Epics"
          data-testid="epic-picker-options"
          className="max-h-72 overflow-y-auto rounded-md border border-neutral-200 dark:border-neutral-800"
        >
          {isLoading ? (
            <div className="p-3 text-xs text-neutral-500" data-testid="epic-picker-loading">
              Loading epics…
            </div>
          ) : error ? (
            <div className="p-3 text-xs text-rose-700 dark:text-rose-300" data-testid="epic-picker-error">
              {error.message}
            </div>
          ) : epics.length === 0 ? (
            <div className="p-3 text-xs text-neutral-500" data-testid="epic-picker-empty">
              {filter.trim() ? 'No epics match the filter.' : 'No epics in the project yet.'}
            </div>
          ) : (
            epics.map((e) => <EpicRow key={e.id} epic={e} onPick={onPick} />)
          )}
        </div>

        <footer className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="epic-picker-cancel"
            className="rounded border border-neutral-300 bg-white px-3 py-1 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
        </footer>
      </div>
    </div>
  );
}

interface EpicRowProps {
  epic: WorkItem;
  onPick: (epicId: string) => void;
}

function EpicRow({ epic, onPick }: EpicRowProps) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={false}
      onClick={() => onPick(epic.id)}
      data-testid={`epic-picker-option-${epic.id}`}
      className={cn(
        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs',
        'hover:bg-neutral-100 dark:hover:bg-neutral-800',
      )}
    >
      <span className="truncate text-neutral-700 dark:text-neutral-300">
        {epic.title || epic.id}
      </span>
      <span className="ml-auto truncate font-mono text-[10px] text-neutral-500">
        {epic.id}
      </span>
    </button>
  );
}
