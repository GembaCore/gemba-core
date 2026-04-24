// NewWorkItemDialog (gm-e12.10). Operator fills in title + kind +
// priority + description and submits. Two entry contexts:
//   - Board header "+ New" button → no parent
//   - EpicDrawer "+ Child" button → parent prefilled + locked
//
// Parent is carried on the wire as a parent_child Relationship with
// From=parent and To="" (bd adaptor translates to `bd create --parent`).
// Validation front-loads title presence; the button stays disabled
// until the form is submittable. Server 400s surface inline.

import { useEffect, useState } from 'react';
import { useCreateWorkItem } from '@/hooks/useWorkItems';
import type { WorkItem } from '@/types/core.gen';

// Kinds bd's CLI recognises today. Presented as a select; the server
// still accepts any string so an adaptor-specific kind works by typing
// into the "Other…" freeform slot.
const COMMON_KINDS = ['task', 'feature', 'bug', 'epic', 'chore', 'decision'] as const;

const PRIORITY_OPTIONS: { value: number; label: string }[] = [
  { value: 0, label: 'P0 (highest)' },
  { value: 1, label: 'P1' },
  { value: 2, label: 'P2 (default)' },
  { value: 3, label: 'P3 (lowest)' },
];

export interface NewWorkItemDialogProps {
  open: boolean;
  onClose: () => void;
  // parentId locks a parent for the new item. EpicDrawer passes the
  // epic's id; the board's top-level "+ New" leaves it undefined.
  parentId?: string;
  // parentTitle is shown alongside parentId so the operator sees what
  // hierarchy they're dropping into. Optional — falls back to id only.
  parentTitle?: string;
  // onCreated fires after a successful create, before onClose. The
  // caller may use it to open the drawer on the new id, for example.
  onCreated?: (item: WorkItem) => void;
}

export function NewWorkItemDialog({
  open,
  onClose,
  parentId,
  parentTitle,
  onCreated,
}: NewWorkItemDialogProps) {
  const [title, setTitle] = useState('');
  const [kind, setKind] = useState<string>('task');
  const [priority, setPriority] = useState<number>(2);
  const [description, setDescription] = useState('');
  const create = useCreateWorkItem();

  // Reset on close transitions. `create` is intentionally not in the
  // deps: useMutation returns a fresh object every render so including
  // it would retrigger the effect each render. Tracking `open` is
  // enough — the only thing that changes reset behavior is the
  // open→close edge.
  const createReset = create.reset;
  useEffect(() => {
    if (!open) {
      setTitle('');
      setKind('task');
      setPriority(2);
      setDescription('');
      createReset();
    }
  }, [open, createReset]);

  if (!open) return null;

  const canSubmit = title.trim().length > 0 && !create.isPending;

  const submit = () => {
    if (!canSubmit) return;
    create.mutate(
      {
        input: {
          title: title.trim(),
          kind,
          status: 'open',
          state_category: 'backlog',
          priority,
          description: description.trim() || undefined,
          relationships: parentId
            ? [{ kind: 'parent_child', from: parentId, to: '' }]
            : undefined,
        },
      },
      {
        onSuccess: (item) => {
          onCreated?.(item);
          onClose();
        },
      }
    );
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="New work item"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      <div
        className="w-full max-w-lg rounded-lg bg-white p-6 shadow-xl dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-4 text-lg font-semibold">New work item</h2>

        {parentId ? (
          <div className="mb-4">
            <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
              Parent
            </label>
            <div className="rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 text-xs text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300">
              <span className="font-mono">{parentId}</span>
              {parentTitle ? (
                <span className="ml-2 text-neutral-500 dark:text-neutral-400">
                  — {parentTitle}
                </span>
              ) : null}
            </div>
          </div>
        ) : null}

        <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
          Title
        </label>
        <input
          type="text"
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="What needs doing?"
          className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
        />

        <div className="mb-4 grid grid-cols-2 gap-3">
          <div>
            <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
              Kind
            </label>
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value)}
              className="w-full rounded-md border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
            >
              {COMMON_KINDS.map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
              Priority
            </label>
            <select
              value={priority}
              onChange={(e) => setPriority(parseInt(e.target.value, 10))}
              className="w-full rounded-md border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
            >
              {PRIORITY_OPTIONS.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
          Description (optional)
        </label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={5}
          placeholder="Markdown OK."
          className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm font-mono dark:border-neutral-700 dark:bg-neutral-800"
        />

        {create.error && (
          <div className="mb-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
            {create.error.message}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!canSubmit}
            className="rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  );
}
