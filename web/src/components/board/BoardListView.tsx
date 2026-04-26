// BoardListView (gm-e12.19.1). Flat WorkItem list rendering for
// Board's `view=list` mode. Replaces the standalone BacklogPage —
// same chip-bar, search, and grid renderer, but driven by Board's
// URL search params (?view=list&preset=…&state_category=…) instead
// of the URL-hash + localStorage hybrid the old BacklogPage used.
//
// Selection — ?bead=<id> — is owned by BoardPage so the WorkItemDrawer
// stays a single instance regardless of view-mode.

import { useMemo } from 'react';
import { Search } from 'lucide-react';
import { useFilteredWorkItems } from '@/hooks/useWorkItems';
import { WorkItemGrid } from '@/components/grid/WorkItemGrid';
import type { WorkItemListFilter } from '@/api/workItems';
import {
  BOARD_PRESET_FILTERS,
  BOARD_PRESET_POST_FILTERS,
  BOARD_PRESET_SORTS,
  type BoardPreset,
  type BoardPresetContext,
} from '@/lib/boardPresets';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

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
  preset: BoardPreset | null;
  stateCategories: StateCategory[];
  kinds: string[];
  search: string;
  onChangeStateCategories: (next: StateCategory[]) => void;
  onChangeKinds: (next: string[]) => void;
  onChangeSearch: (next: string) => void;
  onSelectWorkItem: (id: string) => void;
  presetContext?: BoardPresetContext;
}

export function BoardListView({
  preset,
  stateCategories,
  kinds,
  search,
  onChangeStateCategories,
  onChangeKinds,
  onChangeSearch,
  onSelectWorkItem,
  presetContext,
}: BoardListViewProps) {
  // Effective filter = explicit chips, falling back to the preset's
  // base filter when the operator hasn't touched a chip. Once they
  // start toggling chips the URL carries explicit values and the
  // preset stops contributing to the API filter (it still drives the
  // post-filter so `mine` / `done-recent` keep working).
  const effectiveStates =
    stateCategories.length > 0
      ? stateCategories
      : preset
        ? BOARD_PRESET_FILTERS[preset].state_category
        : [];
  const effectiveKinds =
    kinds.length > 0 ? kinds : preset ? BOARD_PRESET_FILTERS[preset].kind : [];

  const apiFilter = useMemo<WorkItemListFilter>(() => {
    const f: WorkItemListFilter = {};
    if (effectiveStates.length > 0) f.state_category = effectiveStates;
    if (effectiveKinds.length > 0) f.kind = effectiveKinds;
    return f;
  }, [effectiveStates, effectiveKinds]);

  const { data = [], isLoading, error } = useFilteredWorkItems(apiFilter);

  const filtered = useMemo(() => {
    let rows = data;
    if (preset) {
      const post = BOARD_PRESET_POST_FILTERS[preset];
      if (post) rows = rows.filter((it) => post(it, presetContext ?? {}));
      const sort = BOARD_PRESET_SORTS[preset];
      if (sort) rows = [...rows].sort(sort);
    }
    const needle = search.trim().toLowerCase();
    if (needle) rows = rows.filter((it) => it.title.toLowerCase().includes(needle));
    return rows;
  }, [data, search, preset, presetContext]);

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
          <WorkItemGrid rows={filtered as WorkItem[]} onSelect={onSelectWorkItem} />
        )}
      </div>
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
