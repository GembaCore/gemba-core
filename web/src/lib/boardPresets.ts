// Board presets — ui-spec §4.11 (gm-e12.19.1).
//
// A "preset" is a named filter the operator can pin via URL
// (?preset=<name>). Presets pre-apply state_category + kind chips and
// optionally a post-filter predicate; user-edited chips remain layered
// on top, so a preset is a starting point, not a lock.
//
// v1 ships four presets:
//
//   staged       (default kanban) state_category in {staged, started}
//   backlog      (default list)   state_category in {backlog, unstarted}
//   done-recent  (list)           completed within 7d, sorted newest-first
//   mine         (kanban)         assignee = current agent (post-filter)
//
// Presets are intentionally orthogonal to view-mode (epic / workitem /
// list); a v1 preset suggests a default mode but the operator can flip.

import type { BacklogFilter } from '@/lib/backlogFilter';
import type { WorkItem } from '@/types/core.gen';

export const BOARD_PRESETS = ['staged', 'backlog', 'done-recent', 'mine'] as const;
export type BoardPreset = (typeof BOARD_PRESETS)[number];

export const BOARD_PRESET_LABELS: Record<BoardPreset, string> = {
  staged: 'Staged',
  backlog: 'Backlog',
  'done-recent': 'Done · 7d',
  mine: 'Mine',
};

// BoardView is duplicated here (rather than imported from BoardPage)
// so the preset module stays a leaf — BoardPage imports presets, not
// the other way around. Keep the union in sync.
export type BoardView = 'epic' | 'workitem' | 'list';

// Suggested default view for each preset. The Board falls back to
// `epic` (the global default) when no preset is active.
export const BOARD_PRESET_DEFAULT_VIEW: Record<BoardPreset, BoardView> = {
  staged: 'epic',
  backlog: 'list',
  'done-recent': 'list',
  mine: 'epic',
};

export const BOARD_PRESET_FILTERS: Record<BoardPreset, BacklogFilter> = {
  staged: { state_category: ['staged', 'started'], kind: [], search: '' },
  backlog: { state_category: ['backlog', 'unstarted'], kind: [], search: '' },
  // Recently-done's state filter is the API-side narrowing; the
  // 7-day cutoff is enforced post-filter so an operator who layers
  // additional state chips on top still gets the recency band.
  'done-recent': { state_category: ['completed'], kind: [], search: '' },
  // Mine's filter is empty — the post-filter narrows by assignee.
  mine: { state_category: [], kind: [], search: '' },
};

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

// BoardPresetContext carries the runtime info post-filters need
// (current agent identity for `mine`, "now" for `done-recent`'s
// recency band). Callers can stub `now` in tests.
export interface BoardPresetContext {
  currentAgent?: string | null;
  now?: number;
}

export type BoardPresetPredicate = (item: WorkItem, ctx: BoardPresetContext) => boolean;

export const BOARD_PRESET_POST_FILTERS: Partial<Record<BoardPreset, BoardPresetPredicate>> = {
  'done-recent': (it, ctx) => {
    if (!it.updated_at) return true;
    const ts = Date.parse(it.updated_at);
    if (Number.isNaN(ts)) return true;
    return (ctx.now ?? Date.now()) - ts <= SEVEN_DAYS_MS;
  },
  mine: (it, ctx) => {
    if (!ctx.currentAgent || !it.assignee) return false;
    // AgentRef carries both an `id` and a `name`; match either so a
    // CLI-typed identity ("gemba/crew/mike3") and a server-canonical
    // id both work without forcing the operator to know which form
    // the SPA stored.
    return it.assignee.id === ctx.currentAgent || it.assignee.name === ctx.currentAgent;
  },
};

// Sorts apply after the post-filter. Recently-done is the only v1
// preset that needs a non-default order.
export const BOARD_PRESET_SORTS: Partial<Record<BoardPreset, (a: WorkItem, b: WorkItem) => number>> = {
  'done-recent': (a, b) => {
    const at = a.updated_at ? Date.parse(a.updated_at) : 0;
    const bt = b.updated_at ? Date.parse(b.updated_at) : 0;
    return bt - at;
  },
};

export function isKnownPreset(value: string | null | undefined): BoardPreset | null {
  if (!value) return null;
  return (BOARD_PRESETS as readonly string[]).includes(value) ? (value as BoardPreset) : null;
}
