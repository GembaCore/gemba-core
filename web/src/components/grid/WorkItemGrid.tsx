// WorkItemGrid (gm-e12.3.1 / M2). Virtualized WorkItem table built on
// @tanstack/react-table + @tanstack/react-virtual. Renders just the
// visible rows so large backlogs (1k → 10k+ items) stay responsive.
//
// Scope for this slice: always-on core columns, column visibility
// menu, row-click → onSelect. Out of scope (tracked on gm-e12.3):
// saved filters, URL-hash sharing, manifest-derived extension
// columns, JSONL import.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type RowData,
  type VisibilityState,
} from '@tanstack/react-table';
import { useVirtualizer } from '@tanstack/react-virtual';
import { Bookmark, SlidersHorizontal, Trash2 } from 'lucide-react';
import { useUpdateWorkItem } from '@/hooks/useWorkItems';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

// EditableColumnId is the closed set of columns that participate in
// inline editing (gm-5v8v.6.1). Other columns stay read-only — id
// is server-owned, kind / sprint / labels / updated / assignee live
// in the drawer where the multi-step editors are.
type EditableColumnId = 'title' | 'priority' | 'state';

interface GridEditState {
  rowId: string;
  columnId: EditableColumnId;
}

// GridMeta is the per-table state we hand to cell renderers via
// react-table's `meta` channel. Keeping the column defs module-scope
// (so react-table's row-model is stable across renders) means the
// closure can't reach component state directly — meta is the bridge.
interface GridMeta {
  editing: GridEditState | null;
  startEdit: (rowId: string, columnId: EditableColumnId) => void;
  cancelEdit: () => void;
  commit: (rowId: string, columnId: EditableColumnId, value: string) => void;
}

// Augment react-table's TableMeta so cell renderers can read GridMeta
// without casting. Standard v8 pattern.
declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface TableMeta<TData extends RowData> extends GridMeta {}
}

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

// ROW_HEIGHT matches the single-line rendering of the densest row
// (id / mono priority / chip-style state). If the cell content grows
// past one line the virtualizer will still size correctly because we
// use `estimateSize` + `measureElement`, but keeping rows uniform is
// the hot path.
const ROW_HEIGHT = 36;

// OVERSCAN renders a handful of extra rows above and below the
// viewport so fast scrolls don't tear. 10 is the react-virtual
// default; keep it explicit for clarity.
const OVERSCAN = 10;

export interface WorkItemGridProps {
  rows: WorkItem[];
  onSelect?: (id: string) => void;
}

const coreColumns: ColumnDef<WorkItem>[] = [
  {
    id: 'id',
    header: 'ID',
    accessorFn: (r) => r.id,
    size: 140,
    cell: ({ row }) => (
      <span className="font-mono text-xs text-neutral-700 dark:text-neutral-300">{row.original.id}</span>
    ),
  },
  {
    id: 'title',
    header: 'Title',
    accessorFn: (r) => r.title,
    size: 480,
    cell: ({ row, table }) => {
      const meta = table.options.meta;
      const isEditing =
        meta?.editing?.rowId === row.original.id && meta.editing.columnId === 'title';
      if (isEditing) {
        return (
          <CellTextEditor
            columnId="title"
            initial={row.original.title}
            onCommit={(v) => meta!.commit(row.original.id, 'title', v)}
            onCancel={() => meta!.cancelEdit()}
          />
        );
      }
      return (
        <EditableCellSpan
          rowId={row.original.id}
          columnId="title"
          onActivate={() => meta?.startEdit(row.original.id, 'title')}
          className="truncate"
        >
          {row.original.title}
        </EditableCellSpan>
      );
    },
  },
  {
    id: 'kind',
    header: 'Kind',
    accessorFn: (r) => r.kind,
    size: 96,
    cell: ({ row }) => (
      <span className="text-xs text-neutral-600 dark:text-neutral-400">{row.original.kind}</span>
    ),
  },
  {
    id: 'state',
    header: 'State',
    accessorFn: (r) => r.state_category,
    size: 112,
    cell: ({ row, table }) => {
      const meta = table.options.meta;
      const isEditing =
        meta?.editing?.rowId === row.original.id && meta.editing.columnId === 'state';
      if (isEditing) {
        return (
          <CellStateEditor
            initial={row.original.state_category}
            onCommit={(v) => meta!.commit(row.original.id, 'state', v)}
            onCancel={() => meta!.cancelEdit()}
          />
        );
      }
      return (
        <EditableCellSpan
          rowId={row.original.id}
          columnId="state"
          onActivate={() => meta?.startEdit(row.original.id, 'state')}
          className="text-xs"
        >
          {STATE_LABELS[row.original.state_category] ?? row.original.state_category}
        </EditableCellSpan>
      );
    },
  },
  {
    id: 'priority',
    header: 'P',
    accessorFn: (r) => r.priority ?? null,
    size: 48,
    cell: ({ row, table }) => {
      const meta = table.options.meta;
      const isEditing =
        meta?.editing?.rowId === row.original.id && meta.editing.columnId === 'priority';
      if (isEditing) {
        return (
          <CellTextEditor
            columnId="priority"
            // priority is a stored as `number | null`. The editor is a
            // free-form text input; the commit path coerces empty → null
            // and bare numerics → number. Anything else is rejected by
            // the commit handler in the component (see startEdit below).
            initial={row.original.priority == null ? '' : String(row.original.priority)}
            onCommit={(v) => meta!.commit(row.original.id, 'priority', v)}
            onCancel={() => meta!.cancelEdit()}
            inputMode="numeric"
            size={4}
          />
        );
      }
      return (
        <EditableCellSpan
          rowId={row.original.id}
          columnId="priority"
          onActivate={() => meta?.startEdit(row.original.id, 'priority')}
          className="font-mono text-xs text-neutral-700 dark:text-neutral-300"
        >
          {row.original.priority ?? '—'}
        </EditableCellSpan>
      );
    },
  },
  {
    id: 'assignee',
    header: 'Assignee',
    accessorFn: (r) => r.assignee?.name ?? r.assignee?.id ?? '',
    size: 160,
    cell: ({ row }) => (
      <span className="text-xs text-neutral-600 dark:text-neutral-400">
        {row.original.assignee?.name || row.original.assignee?.id || '—'}
      </span>
    ),
  },
  {
    id: 'sprint',
    header: 'Sprint',
    accessorFn: (r) => r.sprint_id ?? '',
    size: 128,
    cell: ({ row }) => (
      <span className="font-mono text-xs">{row.original.sprint_id || '—'}</span>
    ),
  },
  {
    id: 'labels',
    header: 'Labels',
    accessorFn: (r) => (r.labels ?? []).join(','),
    size: 240,
    cell: ({ row }) => {
      const labels = row.original.labels ?? [];
      if (labels.length === 0) return <span className="text-xs text-neutral-500">—</span>;
      return (
        <div className="flex flex-wrap gap-1">
          {labels.map((l) => (
            <span
              key={l}
              className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
            >
              {l}
            </span>
          ))}
        </div>
      );
    },
  },
  {
    id: 'updated',
    header: 'Updated',
    accessorFn: (r) => r.updated_at,
    size: 192,
    cell: ({ row }) => (
      <span className="text-xs text-neutral-500">{row.original.updated_at}</span>
    ),
  },
];

// DEFAULT_VISIBILITY hides nothing; mirrors the column ids above.
const DEFAULT_VISIBILITY: VisibilityState = {
  id: true,
  title: true,
  kind: true,
  state: true,
  priority: true,
  assignee: true,
  sprint: true,
  labels: true,
  updated: true,
};

// COMPACT_VISIBILITY — power-user preset hiding the chattier columns
// (kind, sprint, labels) so the screen is dominated by the two most
// scannable ones: title + state + assignee.
const COMPACT_VISIBILITY: VisibilityState = {
  id: true,
  title: true,
  kind: false,
  state: true,
  priority: true,
  assignee: true,
  sprint: false,
  labels: false,
  updated: true,
};

export interface GridPreset {
  id: string;
  name: string;
  visibility: VisibilityState;
  builtin?: boolean;
}

const BUILTIN_PRESETS: GridPreset[] = [
  { id: 'default', name: 'Default', visibility: DEFAULT_VISIBILITY, builtin: true },
  { id: 'compact', name: 'Compact', visibility: COMPACT_VISIBILITY, builtin: true },
];

// loadUserPresets / saveUserPresets round-trip through localStorage.
// Shape mismatches fall back to [] so a corrupt entry doesn't break
// the grid header.
function loadUserPresets(storageKey: string): GridPreset[] {
  try {
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) return [];
    const v = JSON.parse(raw) as unknown;
    if (!Array.isArray(v)) return [];
    return v.filter(
      (p): p is GridPreset =>
        !!p &&
        typeof p === 'object' &&
        typeof (p as GridPreset).id === 'string' &&
        typeof (p as GridPreset).name === 'string' &&
        !!(p as GridPreset).visibility &&
        typeof (p as GridPreset).visibility === 'object'
    );
  } catch {
    return [];
  }
}

function saveUserPresets(storageKey: string, presets: GridPreset[]): void {
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(presets));
  } catch {
    // Quota / private-mode; silently drop — the UI will re-prompt
    // next time.
  }
}

export interface WorkItemGridPresetOptions {
  // localStorage key for user-saved presets. Omit to disable the
  // preset menu entirely (Backlog page uses the grid without presets).
  storageKey: string;
  // Prompt runner — injected in tests to bypass window.prompt.
  promptName?: () => string | null;
}

export interface WorkItemGridProps {
  rows: WorkItem[];
  onSelect?: (id: string) => void;
  presets?: WorkItemGridPresetOptions;
}

export function WorkItemGrid({ rows, onSelect, presets }: WorkItemGridProps) {
  const [visibility, setVisibility] = useState<VisibilityState>(DEFAULT_VISIBILITY);
  const [menuOpen, setMenuOpen] = useState(false);
  const [presetMenuOpen, setPresetMenuOpen] = useState(false);
  const [userPresets, setUserPresets] = useState<GridPreset[]>(() =>
    presets ? loadUserPresets(presets.storageKey) : []
  );

  // Keep user presets in sync with localStorage so a second tab
  // editing presets shows up after focus events. Low-friction: only
  // reread when the menu opens; avoids polling.
  useEffect(() => {
    if (presets && presetMenuOpen) {
      setUserPresets(loadUserPresets(presets.storageKey));
    }
  }, [presets, presetMenuOpen]);

  const applyPreset = (p: GridPreset) => {
    setVisibility({ ...p.visibility });
    setPresetMenuOpen(false);
  };

  const savePreset = () => {
    if (!presets) return;
    const name = (presets.promptName ?? defaultPromptName)();
    const trimmed = name?.trim();
    if (!trimmed) return;
    const next: GridPreset = {
      id: `user:${Date.now()}:${Math.random().toString(36).slice(2, 8)}`,
      name: trimmed,
      visibility: { ...visibility },
    };
    const merged = [...userPresets, next];
    setUserPresets(merged);
    saveUserPresets(presets.storageKey, merged);
  };

  const deletePreset = (id: string) => {
    if (!presets) return;
    const merged = userPresets.filter((p) => p.id !== id);
    setUserPresets(merged);
    saveUserPresets(presets.storageKey, merged);
  };

  const columns = useMemo(() => coreColumns, []);

  // Inline-edit state (gm-5v8v.6.1). One cell editable at a time;
  // Esc cancels, Enter / blur commits via useUpdateWorkItem (which
  // owns the optimistic write + rollback on PATCH failure, so a
  // failed commit reverts the cell automatically through the
  // react-query cache the rows are derived from).
  const [editing, setEditing] = useState<GridEditState | null>(null);
  const updateWi = useUpdateWorkItem();

  const startEdit = useCallback((rowId: string, columnId: EditableColumnId) => {
    setEditing({ rowId, columnId });
  }, []);
  const cancelEdit = useCallback(() => setEditing(null), []);
  const commit = useCallback(
    (rowId: string, columnId: EditableColumnId, raw: string) => {
      // Decode the raw editor string into the typed patch field. An
      // unchanged value (same as the row's current state) shorts out
      // before firing PATCH so a click-away on an unedited cell
      // doesn't burn a network roundtrip.
      const row = rows.find((r) => r.id === rowId);
      if (!row) {
        setEditing(null);
        return;
      }
      if (columnId === 'title') {
        const next = raw.trim();
        if (next === '' || next === row.title) {
          setEditing(null);
          return;
        }
        updateWi.mutate({ id: rowId, patch: { title: next } });
      } else if (columnId === 'priority') {
        const trimmed = raw.trim();
        let next: number | null;
        if (trimmed === '') {
          next = null;
        } else {
          const parsed = Number(trimmed);
          if (!Number.isFinite(parsed) || !Number.isInteger(parsed) || parsed < 0) {
            // Reject non-numeric / negative input — bail without
            // firing PATCH, leave editor open is wrong (operator
            // already pressed Enter); cancel instead so the cell
            // reverts to the old value visibly.
            setEditing(null);
            return;
          }
          next = parsed;
        }
        if (next === (row.priority ?? null)) {
          setEditing(null);
          return;
        }
        updateWi.mutate({ id: rowId, patch: { priority: next } });
      } else if (columnId === 'state') {
        const next = raw as StateCategory;
        if (!STATE_CATEGORIES.includes(next) || next === row.state_category) {
          setEditing(null);
          return;
        }
        updateWi.mutate({ id: rowId, patch: { state_category: next } });
      }
      setEditing(null);
    },
    [rows, updateWi]
  );

  const meta = useMemo<GridMeta>(
    () => ({ editing, startEdit, cancelEdit, commit }),
    [editing, startEdit, cancelEdit, commit]
  );

  const table = useReactTable({
    data: rows,
    columns,
    state: { columnVisibility: visibility },
    onColumnVisibilityChange: setVisibility,
    getCoreRowModel: getCoreRowModel(),
    meta,
  });

  const scrollRef = useRef<HTMLDivElement>(null);

  const visibleRows = table.getRowModel().rows;
  const virtualizer = useVirtualizer({
    count: visibleRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: OVERSCAN,
  });

  const virtualItems = virtualizer.getVirtualItems();
  const paddingTop = virtualItems.length > 0 ? virtualItems[0].start : 0;
  const paddingBottom =
    virtualItems.length > 0
      ? virtualizer.getTotalSize() - virtualItems[virtualItems.length - 1].end
      : 0;

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="work-item-grid">
      <div className="flex items-center justify-end gap-1 border-b border-neutral-200 bg-neutral-50 px-2 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950">
        {presets ? (
          <div className="relative">
            <button
              type="button"
              onClick={() => setPresetMenuOpen((v) => !v)}
              className="inline-flex items-center gap-1 rounded border border-neutral-300 bg-white px-2 py-0.5 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
              data-testid="grid-presets-toggle"
            >
              <Bookmark className="h-3 w-3" />
              Presets
            </button>
            {presetMenuOpen ? (
              <div
                className="absolute right-0 top-full z-10 mt-1 min-w-[200px] rounded border border-neutral-200 bg-white p-1 shadow-md dark:border-neutral-700 dark:bg-neutral-900"
                data-testid="grid-presets-menu"
              >
                <PresetGroup label="Built-in">
                  {BUILTIN_PRESETS.map((p) => (
                    <PresetRow
                      key={p.id}
                      preset={p}
                      onApply={() => applyPreset(p)}
                    />
                  ))}
                </PresetGroup>
                {userPresets.length > 0 ? (
                  <PresetGroup label="Saved">
                    {userPresets.map((p) => (
                      <PresetRow
                        key={p.id}
                        preset={p}
                        onApply={() => applyPreset(p)}
                        onDelete={() => deletePreset(p.id)}
                      />
                    ))}
                  </PresetGroup>
                ) : null}
                <div className="my-1 border-t border-neutral-200 dark:border-neutral-700" />
                <button
                  type="button"
                  onClick={savePreset}
                  className="flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
                  data-testid="grid-presets-save"
                >
                  Save current as…
                </button>
              </div>
            ) : null}
          </div>
        ) : null}
        <div className="relative">
          <button
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            className="inline-flex items-center gap-1 rounded border border-neutral-300 bg-white px-2 py-0.5 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
            data-testid="grid-columns-toggle"
          >
            <SlidersHorizontal className="h-3 w-3" />
            Columns
          </button>
          {menuOpen ? (
            <div
              className="absolute right-0 top-full z-10 mt-1 min-w-[160px] rounded border border-neutral-200 bg-white p-2 shadow-md dark:border-neutral-700 dark:bg-neutral-900"
              data-testid="grid-columns-menu"
            >
              {table.getAllLeafColumns().map((col) => (
                <label
                  key={col.id}
                  className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
                >
                  <input
                    type="checkbox"
                    checked={col.getIsVisible()}
                    onChange={col.getToggleVisibilityHandler()}
                    data-testid={`grid-column-${col.id}`}
                  />
                  <span>{String(col.columnDef.header)}</span>
                </label>
              ))}
            </div>
          ) : null}
        </div>
      </div>
      <div
        ref={scrollRef}
        className="min-h-0 flex-1 overflow-auto"
        data-testid="grid-scroll"
      >
        <table className="w-full border-collapse text-sm">
          <thead className="sticky top-0 z-10 bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-950">
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id}>
                {hg.headers.map((header) => (
                  <th
                    key={header.id}
                    style={{ width: header.getSize() }}
                    className="border-b border-neutral-200 px-4 py-2 font-medium dark:border-neutral-800"
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {paddingTop > 0 ? (
              <tr aria-hidden>
                <td style={{ height: paddingTop }} colSpan={table.getVisibleFlatColumns().length} />
              </tr>
            ) : null}
            {virtualItems.map((vi) => {
              const row = visibleRows[vi.index];
              return (
                <tr
                  key={row.id}
                  onClick={() => onSelect?.(row.original.id)}
                  className={cn(
                    'cursor-pointer border-b border-neutral-100 hover:bg-neutral-50 dark:border-neutral-900 dark:hover:bg-neutral-900'
                  )}
                  style={{ height: ROW_HEIGHT }}
                  data-testid={`grid-row-${row.original.id}`}
                >
                  {row.getVisibleCells().map((cell) => (
                    <td
                      key={cell.id}
                      style={{ width: cell.column.getSize() }}
                      className="overflow-hidden whitespace-nowrap px-4 align-middle"
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              );
            })}
            {paddingBottom > 0 ? (
              <tr aria-hidden>
                <td
                  style={{ height: paddingBottom }}
                  colSpan={table.getVisibleFlatColumns().length}
                />
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function PresetGroup({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="py-0.5">
      <div className="px-2 py-0.5 font-medium uppercase tracking-wide text-[10px] text-neutral-500">
        {label}
      </div>
      {children}
    </div>
  );
}

function PresetRow({
  preset,
  onApply,
  onDelete,
}: {
  preset: GridPreset;
  onApply: () => void;
  onDelete?: () => void;
}) {
  return (
    <div
      className="flex items-center justify-between rounded px-2 py-1 text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
      data-testid={`grid-preset-${preset.id}`}
    >
      <button
        type="button"
        onClick={onApply}
        className="flex-1 text-left"
        data-testid={`grid-preset-apply-${preset.id}`}
      >
        {preset.name}
      </button>
      {onDelete ? (
        <button
          type="button"
          onClick={onDelete}
          aria-label={`Delete preset ${preset.name}`}
          data-testid={`grid-preset-delete-${preset.id}`}
          className="ml-2 rounded p-1 text-neutral-500 hover:bg-rose-100 hover:text-rose-700 dark:hover:bg-rose-950 dark:hover:text-rose-300"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      ) : null}
    </div>
  );
}

function defaultPromptName(): string | null {
  if (typeof window === 'undefined' || typeof window.prompt !== 'function') return null;
  return window.prompt('Preset name');
}

// ── Inline-edit components (gm-5v8v.6.1) ──────────────────────────

// EditableCellSpan wraps a read-only cell in a click target that
// stops propagation (so the row's onClick → drawer doesn't fire
// when the operator means to edit the cell) and dispatches into the
// grid's startEdit. The hover affordance is subtle but present — a
// dotted underline on hover signals "click to edit" without
// shouting.
interface EditableCellSpanProps {
  rowId: string;
  columnId: EditableColumnId;
  onActivate: () => void;
  className?: string;
  children: React.ReactNode;
}
function EditableCellSpan({
  rowId,
  columnId,
  onActivate,
  className,
  children,
}: EditableCellSpanProps) {
  return (
    <span
      role="button"
      tabIndex={0}
      data-testid={`grid-cell-${rowId}-${columnId}`}
      onClick={(e) => {
        e.stopPropagation();
        onActivate();
      }}
      onKeyDown={(e) => {
        // Enter from the focused cell ALSO opens the editor — keeps
        // the keyboard-only operator on the same path.
        if (e.key === 'Enter') {
          e.stopPropagation();
          onActivate();
        }
      }}
      className={cn(
        'inline-block cursor-text rounded px-0.5 hover:bg-neutral-100 hover:underline hover:decoration-dotted focus:bg-neutral-100 focus:outline-none dark:hover:bg-neutral-800 dark:focus:bg-neutral-800',
        className
      )}
    >
      {children}
    </span>
  );
}

interface CellTextEditorProps {
  columnId: EditableColumnId;
  initial: string;
  onCommit: (value: string) => void;
  onCancel: () => void;
  inputMode?: 'text' | 'numeric';
  size?: number;
}

function CellTextEditor({
  columnId,
  initial,
  onCommit,
  onCancel,
  inputMode = 'text',
  size,
}: CellTextEditorProps) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement>(null);
  // committedRef guards against a double-commit when the user hits
  // Enter (which also blurs the input). Without it, the onBlur fires
  // a second commit with the same value, which mostly is short-
  // circuited by the equality check in `commit` but still costs a
  // function call and a microtask.
  const committedRef = useRef(false);

  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
  }, []);

  const tryCommit = (v: string) => {
    if (committedRef.current) return;
    committedRef.current = true;
    onCommit(v);
  };

  return (
    <input
      ref={ref}
      type="text"
      inputMode={inputMode}
      size={size}
      data-testid={`grid-cell-editor-${columnId}`}
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === 'Enter') {
          tryCommit(value);
        } else if (e.key === 'Escape') {
          committedRef.current = true;
          onCancel();
        }
      }}
      onBlur={() => tryCommit(value)}
      className="w-full rounded border border-sky-500 bg-white px-1 py-0.5 text-sm outline-none ring-1 ring-sky-200 dark:bg-neutral-900 dark:ring-sky-900"
    />
  );
}

interface CellStateEditorProps {
  initial: StateCategory;
  onCommit: (value: StateCategory) => void;
  onCancel: () => void;
}

function CellStateEditor({ initial, onCommit, onCancel }: CellStateEditorProps) {
  const ref = useRef<HTMLSelectElement>(null);
  const committedRef = useRef(false);

  useEffect(() => {
    ref.current?.focus();
  }, []);

  const tryCommit = (v: StateCategory) => {
    if (committedRef.current) return;
    committedRef.current = true;
    onCommit(v);
  };

  return (
    <select
      ref={ref}
      data-testid="grid-cell-editor-state"
      defaultValue={initial}
      onChange={(e) => tryCommit(e.target.value as StateCategory)}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === 'Escape') {
          committedRef.current = true;
          onCancel();
        }
      }}
      onBlur={() => {
        // If the operator clicked away without changing, the change
        // event never fired; commit current value (which equals
        // initial and short-circuits in the grid's commit path).
        if (ref.current) {
          tryCommit(ref.current.value as StateCategory);
        }
      }}
      className="w-full rounded border border-sky-500 bg-white px-1 py-0.5 text-xs outline-none dark:bg-neutral-900"
    >
      {STATE_CATEGORIES.map((sc) => (
        <option key={sc} value={sc}>
          {STATE_LABELS[sc] ?? sc}
        </option>
      ))}
    </select>
  );
}
