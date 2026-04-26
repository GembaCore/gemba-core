// Values editor modal for /project/config (gm-uipx.8). Per ui-spec
// §5.16: priority-ranked statement table with add / edit / remove
// + rank up/down. v1 persists to localStorage; a future bead lifts
// the model to .gemba/workspace.toml.

import { useMemo, useState, useSyncExternalStore } from 'react';
import { ArrowDown, ArrowUp, Plus, Trash2, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { ProjectValue } from './types';

const STORAGE_KEY = 'gemba.projectconfig.values';

// Module-level store so the section preview and the editor modal
// stay in sync without a Context provider. useSyncExternalStore
// drives re-renders on change; localStorage backs persistence.
type Listener = () => void;
const listeners = new Set<Listener>();

function readStored(): ProjectValue[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    return (JSON.parse(raw) as ProjectValue[]).sort((a, b) => a.rank - b.rank);
  } catch {
    return [];
  }
}

let cached: ProjectValue[] = (() => {
  try {
    return readStored();
  } catch {
    return [];
  }
})();

function writeStore(next: ProjectValue[]): void {
  cached = next;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // Quota / private mode — drop silently.
  }
  listeners.forEach((l) => l());
}

function subscribe(l: Listener): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

function getSnapshot(): ProjectValue[] {
  return cached;
}

export function useProjectValues(): {
  values: ProjectValue[];
  add: (statement: string) => void;
  update: (id: string, statement: string) => void;
  remove: (id: string) => void;
  moveUp: (id: string) => void;
  moveDown: (id: string) => void;
} {
  const values = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  return {
    values,
    add: (statement) => {
      const trimmed = statement.trim();
      if (!trimmed) return;
      const nextRank =
        cached.length === 0 ? 1 : Math.max(...cached.map((v) => v.rank)) + 1;
      writeStore([
        ...cached,
        {
          id: `v-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
          statement: trimmed,
          rank: nextRank,
        },
      ]);
    },
    update: (id, statement) =>
      writeStore(cached.map((v) => (v.id === id ? { ...v, statement } : v))),
    remove: (id) => writeStore(normalizeRanks(cached.filter((v) => v.id !== id))),
    moveUp: (id) => writeStore(swapRank(cached, id, -1)),
    moveDown: (id) => writeStore(swapRank(cached, id, +1)),
  };
}

// resetProjectValuesForTests is a test-only seam — clears the
// in-memory cache so beforeEach/afterEach localStorage clears
// remain authoritative across test files.
export function resetProjectValuesForTests(): void {
  cached = [];
  listeners.forEach((l) => l());
}

// renumberInOrder takes an already-positioned array (whatever the
// caller's preferred order is) and writes ranks 1..N matching that
// order. Used by remove + swapRank so the rank field is always the
// list position, never a stale free-form index.
function renumberInOrder(positioned: ProjectValue[]): ProjectValue[] {
  return positioned.map((v, i) => ({ ...v, rank: i + 1 }));
}

function normalizeRanks(values: ProjectValue[]): ProjectValue[] {
  // Sort by current rank first so the renumbered output matches
  // operator intent when the caller passes an unsorted slice.
  return renumberInOrder(values.slice().sort((a, b) => a.rank - b.rank));
}

function swapRank(prev: ProjectValue[], id: string, delta: number): ProjectValue[] {
  const sorted = prev.slice().sort((a, b) => a.rank - b.rank);
  const idx = sorted.findIndex((v) => v.id === id);
  const swapIdx = idx + delta;
  if (idx < 0 || swapIdx < 0 || swapIdx >= sorted.length) return prev;
  const out = sorted.slice();
  [out[idx], out[swapIdx]] = [out[swapIdx], out[idx]];
  return renumberInOrder(out);
}

export function ValuesEditor({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): JSX.Element | null {
  const v = useProjectValues();
  const [draft, setDraft] = useState('');

  if (!open) return null;
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Edit values"
      data-testid="values-editor-modal"
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/40"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-[640px] max-w-[90vw] rounded-lg border border-neutral-200 bg-white shadow-xl dark:border-neutral-800 dark:bg-neutral-950">
        <header className="flex items-center justify-between border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <h2 className="text-sm font-semibold">Edit project values</h2>
          <button
            type="button"
            data-testid="values-editor-close"
            aria-label="Close values editor"
            onClick={onClose}
            className="rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="max-h-[60vh] overflow-y-auto px-4 py-3">
          {v.values.length === 0 ? (
            <p
              data-testid="values-editor-empty"
              className="rounded border border-dashed border-neutral-300 px-3 py-4 text-center text-xs italic text-neutral-500 dark:border-neutral-700"
            >
              No values yet. Add one below to start ranking.
            </p>
          ) : (
            <table className="w-full table-fixed text-sm" data-testid="values-editor-table">
              <thead>
                <tr className="text-left text-[10px] uppercase tracking-wider text-neutral-500">
                  <th className="w-12 px-2 py-1">Rank</th>
                  <th className="px-2 py-1">Statement</th>
                  <th className="w-32 px-2 py-1 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {v.values.map((value, idx) => (
                  <ValueRow
                    key={value.id}
                    value={value}
                    canMoveUp={idx > 0}
                    canMoveDown={idx < v.values.length - 1}
                    onUpdate={(s) => v.update(value.id, s)}
                    onRemove={() => v.remove(value.id)}
                    onMoveUp={() => v.moveUp(value.id)}
                    onMoveDown={() => v.moveDown(value.id)}
                  />
                ))}
              </tbody>
            </table>
          )}
        </div>
        <footer className="flex items-center gap-2 border-t border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <textarea
            data-testid="values-editor-draft"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="State a value the team holds…"
            rows={1}
            className="flex-1 resize-none rounded border border-neutral-300 bg-white px-2 py-1 text-xs dark:border-neutral-700 dark:bg-neutral-900"
          />
          <button
            type="button"
            data-testid="values-editor-add"
            onClick={() => {
              v.add(draft);
              setDraft('');
            }}
            className="inline-flex items-center gap-1 rounded bg-sky-600 px-2 py-1 text-xs text-white hover:bg-sky-700 disabled:opacity-50"
            disabled={draft.trim().length === 0}
          >
            <Plus className="h-3 w-3" />
            Add
          </button>
        </footer>
      </div>
    </div>
  );
}

function ValueRow({
  value,
  canMoveUp,
  canMoveDown,
  onUpdate,
  onRemove,
  onMoveUp,
  onMoveDown,
}: {
  value: ProjectValue;
  canMoveUp: boolean;
  canMoveDown: boolean;
  onUpdate: (s: string) => void;
  onRemove: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
}): JSX.Element {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value.statement);
  return (
    <tr data-testid={`values-editor-row-${value.id}`} className="border-t border-neutral-100 dark:border-neutral-800/60">
      <td className="px-2 py-2 align-top font-mono text-xs text-neutral-500">{value.rank}</td>
      <td className="px-2 py-2 align-top">
        {editing ? (
          <textarea
            data-testid={`values-editor-row-${value.id}-edit`}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={() => {
              if (draft.trim().length > 0) onUpdate(draft.trim());
              setEditing(false);
            }}
            rows={2}
            className="w-full resize-none rounded border border-neutral-300 bg-white px-2 py-1 text-xs dark:border-neutral-700 dark:bg-neutral-900"
          />
        ) : (
          <button
            type="button"
            data-testid={`values-editor-row-${value.id}-statement`}
            onClick={() => {
              setDraft(value.statement);
              setEditing(true);
            }}
            className="w-full text-left text-xs text-neutral-800 hover:underline dark:text-neutral-200"
          >
            {value.statement}
          </button>
        )}
      </td>
      <td className="px-2 py-2 align-top">
        <div className="flex justify-end gap-1">
          <IconBtn
            disabled={!canMoveUp}
            onClick={onMoveUp}
            label="Move up"
            testid={`values-editor-row-${value.id}-up`}
          >
            <ArrowUp className="h-3 w-3" />
          </IconBtn>
          <IconBtn
            disabled={!canMoveDown}
            onClick={onMoveDown}
            label="Move down"
            testid={`values-editor-row-${value.id}-down`}
          >
            <ArrowDown className="h-3 w-3" />
          </IconBtn>
          <IconBtn
            onClick={onRemove}
            label="Remove value"
            testid={`values-editor-row-${value.id}-remove`}
          >
            <Trash2 className="h-3 w-3" />
          </IconBtn>
        </div>
      </td>
    </tr>
  );
}

function IconBtn({
  children,
  disabled,
  onClick,
  label,
  testid,
}: {
  children: React.ReactNode;
  disabled?: boolean;
  onClick: () => void;
  label: string;
  testid: string;
}): JSX.Element {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-label={label}
      data-testid={testid}
      onClick={onClick}
      className={cn(
        'rounded border px-1.5 py-1 text-neutral-600',
        'border-neutral-300 hover:bg-neutral-100 disabled:opacity-30',
        'dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800'
      )}
    >
      {children}
    </button>
  );
}

// useProjectValuesPreview returns a memo'd top-N preview for the
// section card. Read-only; the editor itself is the write surface.
export function useProjectValuesPreview(n = 3): ProjectValue[] {
  const v = useProjectValues();
  return useMemo(() => v.values.slice(0, n), [v.values, n]);
}
