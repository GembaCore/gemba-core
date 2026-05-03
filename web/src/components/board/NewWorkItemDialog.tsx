// NewWorkItemDialog (gm-e12.10). Operator fills in title + kind +
// priority + description and submits. Two entry contexts:
//   - Board header "+ New" button → no parent
//   - EpicDetail "+ Child" button → parent prefilled + locked
//
// Parent is carried on the wire as a parent_child Relationship with
// From=parent and To="" (bd adaptor translates to `bd create --parent`).
// Validation front-loads title presence; the button stays disabled
// until the form is submittable. Server 400s surface inline.

import { useEffect, useState } from 'react';
import { useCreateWorkItem, useWorkItems } from '@/hooks/useWorkItems';
import type { WorkItem } from '@/types/core.gen';

// Kinds bd's CLI recognises today. Presented as a select; the server
// still accepts any string so an adaptor-specific kind works by typing
// into the "Other…" freeform slot.
const COMMON_KINDS = ['task', 'feature', 'bug', 'epic', 'milestone', 'chore', 'decision'] as const;

const PRIORITY_OPTIONS: { value: number; label: string }[] = [
  { value: 0, label: 'P0 (highest)' },
  { value: 1, label: 'P1' },
  { value: 2, label: 'P2 (default)' },
  { value: 3, label: 'P3 (lowest)' },
];

export interface NewWorkItemDialogProps {
  open: boolean;
  onClose: () => void;
  // parentId locks a parent for the new item. EpicDetail passes the
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
  const [selectedLabels, setSelectedLabels] = useState<string[]>([]);
  const create = useCreateWorkItem();
  const { data: existingItems = [] } = useWorkItems();

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
      setSelectedLabels([]);
      createReset();
    }
  }, [open, createReset]);

  useEffect(() => {
    if (!open || title.trim() !== '') return;
    const prefix = nextPrefix(kind, parentId, existingItems);
    if (prefix) setTitle(`${prefix} `);
  }, [open, kind, parentId, existingItems, title]);

  if (!open) return null;

  const canSubmit = hasAuthoredTitle(title) && !create.isPending;

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
          labels: selectedLabels.length > 0 ? selectedLabels : undefined,
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
          aria-label="Title"
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={suggestedTitle(kind, parentId, existingItems)}
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

        <TagPills
          items={existingItems}
          selected={selectedLabels}
          onToggle={(label) =>
            setSelectedLabels((cur) =>
              cur.includes(label) ? cur.filter((v) => v !== label) : [...cur, label]
            )
          }
        />

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

function TagPills({
  items,
  selected,
  onToggle,
}: {
  items: WorkItem[];
  selected: string[];
  onToggle: (label: string) => void;
}) {
  const tags = items
    .filter((item) => item.kind === 'milestone' || item.kind === 'decision')
    .slice(0, 12)
    .map((item) => ({
      label: `${item.kind}:${item.id}`,
      title: item.title || item.id,
      kind: item.kind,
    }));
  if (tags.length === 0) return null;
  return (
    <div className="mb-4">
      <div className="mb-1 text-xs font-medium text-neutral-600 dark:text-neutral-400">
        Tags
      </div>
      <div className="flex flex-wrap gap-1.5" data-testid="new-workitem-tag-pills">
        {tags.map((tag) => {
          const active = selected.includes(tag.label);
          return (
            <button
              key={tag.label}
              type="button"
              data-active={active || undefined}
              onClick={() => onToggle(tag.label)}
              className={
                active
                  ? 'rounded-full bg-cyan-700 px-2 py-0.5 text-xs text-white'
                  : 'rounded-full border border-neutral-300 px-2 py-0.5 text-xs text-neutral-600 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800'
              }
            >
              {tag.kind}: {tag.title}
            </button>
          );
        })}
      </div>
    </div>
  );
}

function suggestedTitle(kind: string, parentId: string | undefined, items: WorkItem[]): string {
  const prefix = nextPrefix(kind, parentId, items);
  if (!prefix) return 'What needs doing?';
  return `${prefix} short title`;
}

function nextPrefix(kind: string, parentId: string | undefined, items: WorkItem[]): string {
  const normalized = kind.toLowerCase();
  if (normalized === 'milestone') return `M${nextNumber(items, /^M(\d+)[:\s-]/i)}:`;
  if (normalized === 'epic') return `E${nextNumber(items, /^E(\d+)[:\s-]/i)}:`;
  if (normalized === 'decision') return `D${nextNumber(items, /^D(\d+)[:\s-]/i)}:`;
  if (parentId) {
    const children = items.filter((item) =>
      (item.relationships ?? []).some(
        (rel) => rel.kind === 'parent_child' && rel.from === parentId && rel.to === item.id
      )
    );
    return `W${children.length + 1}:`;
  }
  return '';
}

function hasAuthoredTitle(title: string): boolean {
  const trimmed = title.trim();
  if (!trimmed) return false;
  return !/^[A-Z]+\d+:\s*$/i.test(trimmed);
}

function nextNumber(items: WorkItem[], re: RegExp): number {
  let max = 0;
  for (const item of items) {
    const m = item.title.match(re);
    if (!m) continue;
    const n = Number.parseInt(m[1] ?? '', 10);
    if (Number.isFinite(n)) max = Math.max(max, n);
  }
  return max + 1;
}
