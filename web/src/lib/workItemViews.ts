// workItemViews — the unified named-view registry (gm-uipx.18).
//
// Replaces the two parallel registries that grew side-by-side:
//
//   web/src/lib/boardPresets.ts (BOARD_PRESETS, 4 entries)
//   web/src/lib/gridViews.ts    (NAMED_VIEWS, 5 entries)
//
// Each was a (filter, post-filter, sort, default-layout) tuple under
// a different name with partial overlap; operators had to remember
// which page exposed "Blocked" (Grid) vs. "Mine" (Board). The
// intersection is a single conceptual primitive — a `WorkItemView`
// — and this module is its single home.
//
// Every page (Board's epic / workitem / list layouts; the future
// Grid-as-list-power layout in gm-uipx.17) reads from this registry
// so the same view name means the same thing on every surface.

import type { BacklogFilter } from '@/lib/backlogFilter';
import type { WorkItem } from '@/types/core.gen';

// WorkItemLayout is the closed set of viewing modes the SPA exposes.
// Today three are reachable on /board (?view=...); 'list-power' is
// reserved for gm-uipx.17 which will fold /grid into /board's list
// view behind a power toggle.
export type WorkItemLayout = 'epic-kanban' | 'workitem-kanban' | 'list-simple' | 'list-power';

// LegacyBoardView is the union the existing BoardPage uses today
// (epic / workitem / list). We keep it as an alias so the page
// migration in this bead stays mechanical; gm-uipx.17 widens to the
// full WorkItemLayout when the power toggle lands.
export type LegacyBoardView = 'epic' | 'workitem' | 'list';

// LAYOUT_FROM_LEGACY maps the legacy three-mode union onto the
// four-mode WorkItemLayout. Used when a view's defaultLayout is
// referenced from BoardPage's old setView() flow.
export const LAYOUT_FROM_LEGACY: Record<LegacyBoardView, WorkItemLayout> = {
  epic: 'epic-kanban',
  workitem: 'workitem-kanban',
  list: 'list-simple',
};

// LEGACY_FROM_LAYOUT is the inverse, used by BoardPage to translate
// the registry's defaultLayout back into its current three-mode
// state. list-power maps to list-simple here because the power
// toggle is a separate URL param (?power=1, gm-uipx.17), not a
// layout switch.
export const LEGACY_FROM_LAYOUT: Record<WorkItemLayout, LegacyBoardView> = {
  'epic-kanban': 'epic',
  'workitem-kanban': 'workitem',
  'list-simple': 'list',
  'list-power': 'list',
};

// ViewContext carries runtime info that post-filters may need. Same
// shape as the old BoardPresetContext; tests inject `now` for
// deterministic assertions, production leaves it undefined.
export interface ViewContext {
  // currentAgent identifies the operator for the `mine` view's
  // assignee comparison. Empty / null → mine yields nothing rather
  // than throwing.
  currentAgent?: string | null;
  // now overrides Date.now() for predicates that compare against a
  // wall-clock window (today: done-recent's 7-day band).
  now?: number;
}

// WorkItemView is the registry entry. Every page that exposes
// named views reads from this shape.
export interface WorkItemView {
  // id is the URL-stable canonical name (lower-kebab-case). Stays
  // stable across releases so bookmarks don't rot.
  id: string;
  // label is the operator-visible name (chip text + dropdown entry).
  label: string;
  // baseFilter narrows the API fetch; the chip strip lets the
  // operator override on top.
  baseFilter: BacklogFilter;
  // postFilter narrows after fetch + after the search-box's title
  // substring filter. Returning true keeps the row. Optional —
  // filter-only views leave it undefined.
  postFilter?: (item: WorkItem, ctx: ViewContext) => boolean;
  // sort reorders the post-filtered rows. Standard Array.sort
  // comparator; optional.
  sort?: (a: WorkItem, b: WorkItem) => number;
  // defaultLayout is the layout this view prefers when no explicit
  // layout is in the URL. The operator can flip layouts inside any
  // view; this is just the suggested starting point.
  defaultLayout: WorkItemLayout;
  // aliases are old names that resolve to this view. Used by the
  // URL + localStorage migration so existing bookmarks keep working
  // — see migrateLegacyParams + readLegacyViewStorage.
  aliases?: string[];
}

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

// Empty filter — most views start here and narrow via state_category.
const noFilter: BacklogFilter = { state_category: [], kind: [], search: '' };

// WORK_ITEM_VIEWS is the canonical registry. Order matters for the
// default chip-strip ordering — operators learn the layout, so
// adding a view appends to the end. Removing one is breaking
// (existing URLs / localStorage entries silently fall back to no
// view selected); the migration helpers protect against that for
// the four cases we already shipped.
export const WORK_ITEM_VIEWS: readonly WorkItemView[] = [
  {
    id: 'staged',
    label: 'Staged',
    baseFilter: { ...noFilter, state_category: ['staged'] },
    defaultLayout: 'epic-kanban',
  },
  {
    id: 'in-progress',
    label: 'In Progress',
    baseFilter: { ...noFilter, state_category: ['started'] },
    defaultLayout: 'epic-kanban',
  },
  {
    id: 'blocked',
    label: 'Blocked',
    // Blocked work can be in any state column; the post-filter
    // narrows by the server-derived "human action required"
    // boolean (gm-gsh DerivedSignals).
    baseFilter: noFilter,
    postFilter: (it) => Boolean(it.derived?.human_action_required),
    defaultLayout: 'list-simple',
  },
  {
    id: 'ready-to-stage',
    label: 'Ready to stage',
    baseFilter: { ...noFilter, state_category: ['unstarted'] },
    postFilter: (it) => Boolean(it.derived?.agent_claimable),
    defaultLayout: 'list-simple',
  },
  {
    id: 'backlog',
    label: 'Backlog',
    baseFilter: { ...noFilter, state_category: ['backlog', 'unstarted'] },
    defaultLayout: 'list-simple',
  },
  {
    id: 'done-recent',
    label: 'Done · 7d',
    baseFilter: { ...noFilter, state_category: ['completed'] },
    postFilter: (it, ctx) => {
      if (!it.updated_at) return true;
      const ts = Date.parse(it.updated_at);
      if (Number.isNaN(ts)) return true;
      const now = ctx.now ?? Date.now();
      return now - ts <= SEVEN_DAYS_MS;
    },
    sort: (a, b) => {
      const at = a.updated_at ? Date.parse(a.updated_at) : 0;
      const bt = b.updated_at ? Date.parse(b.updated_at) : 0;
      return bt - at;
    },
    defaultLayout: 'list-simple',
    // recently-done was Grid's pre-unification name for the same
    // view. Keep it as an alias so existing URLs / saved chip
    // settings resolve cleanly.
    aliases: ['recently-done'],
  },
  {
    id: 'mine',
    label: 'Mine',
    baseFilter: noFilter,
    postFilter: (it, ctx) => {
      if (!ctx.currentAgent || !it.assignee) return false;
      // AgentRef carries both an `id` and a `name`; match either so
      // a CLI-typed identity ("gemba/crew/mike3") and a server-
      // canonical id both resolve without forcing the operator to
      // know which form the SPA stored.
      return it.assignee.id === ctx.currentAgent || it.assignee.name === ctx.currentAgent;
    },
    defaultLayout: 'epic-kanban',
  },
];

// findView looks up a view by id OR alias. Returns null when the
// name is unknown — callers default to "no view selected".
export function findView(name: string | null | undefined): WorkItemView | null {
  if (!name) return null;
  const lc = name;
  for (const v of WORK_ITEM_VIEWS) {
    if (v.id === lc) return v;
    if (v.aliases?.includes(lc)) return v;
  }
  return null;
}

// applyView post-filters + sorts in one pass. Pure — same items +
// view + ctx → same output. Tests inject ctx.now for deterministic
// assertions on the time-based views.
export function applyView(items: WorkItem[], view: WorkItemView | null, ctx: ViewContext): WorkItem[] {
  if (!view) return items;
  let out = items;
  if (view.postFilter) {
    out = out.filter((it) => view.postFilter!(it, ctx));
  }
  if (view.sort) {
    out = [...out].sort(view.sort);
  }
  return out;
}

// LEGACY_VIEW_PARAMS lists the URL keys the old code used for the
// named-view selection. Board used `?preset=`; Grid used `?view=`.
// The migration helpers fold both into the canonical `?view=`.
export const LEGACY_VIEW_PARAMS = ['preset'] as const;
export const VIEW_PARAM = 'view';

// LAYOUT_PARAM is the URL key for the layout selection. Distinct
// from VIEW_PARAM after gm-uipx.18: Board's old `?view=epic` /
// `?view=workitem` / `?view=list` collided with the named-view
// registry, so the layout moves to `?layout=`.
export const LAYOUT_PARAM = 'layout';

// LEGACY_LAYOUT_VALUES are the strings Board's old `?view=` carried
// for layout selection. When migrateLegacyParams sees one of these
// in `?view=`, it moves the value to `?layout=` and drops the view
// key (the operator wasn't selecting a named view, they were
// selecting a layout).
const LEGACY_LAYOUT_VALUES: Record<string, LegacyBoardView> = {
  epic: 'epic',
  workitem: 'workitem',
  list: 'list',
};

// migrateLegacyParams normalises a URLSearchParams in place: any
// legacy key (today: ?preset=staged) gets converted to the canonical
// ?view=staged. Aliases (e.g. recently-done → done-recent) get
// rewritten to the canonical id at the same time so two operators
// who bookmarked the same intent under different names see the same
// chip light up.
//
// Returns true when something was migrated so callers can decide
// whether to history.replaceState the cleaned URL.
export function migrateLegacyParams(params: URLSearchParams): boolean {
  let changed = false;

  // Pass 1 — Move Board's old layout-in-view (?view=epic|workitem|list)
  // out to ?layout= so the canonical view slot is free for pass 2.
  // Done first because pass 2 may want to write into the now-empty
  // view key from a legacy ?preset=.
  const initialView = params.get(VIEW_PARAM);
  if (initialView !== null && initialView in LEGACY_LAYOUT_VALUES) {
    params.delete(VIEW_PARAM);
    if (!params.has(LAYOUT_PARAM)) {
      params.set(LAYOUT_PARAM, LEGACY_LAYOUT_VALUES[initialView]);
    }
    changed = true;
  }

  // Pass 2 — Migrate every legacy view-param key (today: ?preset=)
  // into the canonical ?view=, but only when the slot is free.
  for (const key of LEGACY_VIEW_PARAMS) {
    const v = params.get(key);
    if (v === null) continue;
    params.delete(key);
    if (!params.has(VIEW_PARAM)) {
      const canonical = canonicaliseViewName(v);
      if (canonical) {
        params.set(VIEW_PARAM, canonical);
      }
    }
    changed = true;
  }

  // Pass 3 — Canonicalise an alias in ?view= (e.g. recently-done →
  // done-recent), or drop a value the registry doesn't know.
  const view = params.get(VIEW_PARAM);
  if (view !== null) {
    const canonical = canonicaliseViewName(view);
    if (canonical && canonical !== view) {
      params.set(VIEW_PARAM, canonical);
      changed = true;
    } else if (!canonical) {
      params.delete(VIEW_PARAM);
      changed = true;
    }
  }

  // Pass 4 (gm-uekk) — drop the deprecated ?swimlane= param. Scope
  // subsumes its narrowing semantics; by-parent-epic was the
  // implicit default and the others (by-label / none) are no longer
  // surfaced. Leftover bookmarks just lose the param without error.
  if (params.has('swimlane')) {
    params.delete('swimlane');
    changed = true;
  }
  return changed;
}

// canonicaliseViewName returns the canonical id for a view name,
// resolving aliases. null when the name is unknown.
export function canonicaliseViewName(name: string): string | null {
  const v = findView(name);
  return v?.id ?? null;
}

// VIEW_STORAGE_KEY is the unified localStorage key. Replaces the
// per-page keys the old registries used.
export const VIEW_STORAGE_KEY = 'gemba.workitem.view';

// LEGACY_VIEW_STORAGE_KEYS are the per-page keys the old code wrote.
// readLegacyViewStorage migrates the first non-empty one on first
// load so an operator's last-used view survives the unification.
export const LEGACY_VIEW_STORAGE_KEYS = ['gemba.grid.view'] as const;

// readLegacyViewStorage performs a one-time migration: if the
// canonical key is empty but a legacy key has a value, copy it (and
// resolve aliases). Returns the resolved canonical view id, or null
// when nothing meaningful was found.
//
// Safe to call on every mount — it's idempotent once the canonical
// key is populated.
export function readLegacyViewStorage(storage: Storage = globalThis.localStorage): string | null {
  if (typeof storage === 'undefined') return null;
  const canonical = storage.getItem(VIEW_STORAGE_KEY);
  if (canonical) {
    const resolved = canonicaliseViewName(canonical);
    if (!resolved) {
      // Unknown stored value (typo, deleted view, foreign rig).
      // Clear so the next read doesn't keep returning a name no
      // page can render; null tells the caller "no view selected."
      storage.removeItem(VIEW_STORAGE_KEY);
      return null;
    }
    if (resolved !== canonical) {
      storage.setItem(VIEW_STORAGE_KEY, resolved);
    }
    return resolved;
  }
  for (const key of LEGACY_VIEW_STORAGE_KEYS) {
    const v = storage.getItem(key);
    if (!v) continue;
    const resolved = canonicaliseViewName(v);
    if (resolved) {
      storage.setItem(VIEW_STORAGE_KEY, resolved);
      return resolved;
    }
  }
  return null;
}
