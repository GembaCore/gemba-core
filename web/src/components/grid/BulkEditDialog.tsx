// BulkEditDialog (gm-5v8v.6.2). Minimal bulk-update surface: state
// category + priority for every selected work item. The grid binds it
// behind onBulkAction='edit' (Cmd+E or context menu). Each apply
// fans out to one PATCH per id with its own nonce — the server
// throttles per-id idempotency, so a flurry of clicks doesn't
// duplicate work even if the operator double-clicks Apply.
//
// Out of scope: editing labels / assignee. Add fields here as the
// bulk surface grows; the contract with the grid (onApply: (patch) =>
// void) doesn't need to widen.

import { useEffect, useState } from 'react';
import { STATE_CATEGORIES, type StateCategory } from '@/types/core.gen';
import type { WorkItemPatch } from '@/api/workItems';

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Deferred',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

export interface BulkEditDialogProps {
  open: boolean;
  ids: string[];
  onClose: () => void;
  onApply: (patch: WorkItemPatch) => void;
}

export function BulkEditDialog({ open, ids, onClose, onApply }: BulkEditDialogProps) {
  const [stateCategory, setStateCategory] = useState<StateCategory | ''>('');
  const [priority, setPriority] = useState<string>('');

  // Reset form fields whenever the dialog opens — the previous batch's
  // selection shouldn't leak into the next one.
  useEffect(() => {
    if (open) {
      setStateCategory('');
      setPriority('');
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  const apply = () => {
    const patch: WorkItemPatch = {};
    if (stateCategory) patch.state_category = stateCategory;
    if (priority) {
      const n = Number(priority);
      if (!Number.isNaN(n)) patch.priority = n;
    }
    if (Object.keys(patch).length === 0) {
      onClose();
      return;
    }
    onApply(patch);
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Bulk edit work items"
      data-testid="bulk-edit-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-5 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="mb-3">
          <h2 className="text-base font-semibold">Bulk edit</h2>
          <p className="mt-0.5 text-xs text-neutral-500">
            Applying to <span data-testid="bulk-edit-count">{ids.length}</span>{' '}
            selected work item{ids.length === 1 ? '' : 's'}.
          </p>
        </header>

        <div className="flex flex-col gap-3 text-sm">
          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium text-neutral-700 dark:text-neutral-300">
              State
            </span>
            <select
              value={stateCategory}
              onChange={(e) => setStateCategory(e.target.value as StateCategory | '')}
              data-testid="bulk-edit-state"
              className="rounded border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-900"
            >
              <option value="">— no change —</option>
              {STATE_CATEGORIES.map((sc) => (
                <option key={sc} value={sc}>
                  {STATE_LABELS[sc]}
                </option>
              ))}
            </select>
          </label>

          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium text-neutral-700 dark:text-neutral-300">
              Priority
            </span>
            <select
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              data-testid="bulk-edit-priority"
              className="rounded border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-900"
            >
              <option value="">— no change —</option>
              {[0, 1, 2, 3, 4, 5].map((p) => (
                <option key={p} value={String(p)}>
                  P{p}
                </option>
              ))}
            </select>
          </label>
        </div>

        <footer className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="bulk-edit-cancel"
            className="rounded border border-neutral-300 bg-white px-3 py-1 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={apply}
            data-testid="bulk-edit-apply"
            className="rounded bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-700"
          >
            Apply
          </button>
        </footer>
      </div>
    </div>
  );
}
