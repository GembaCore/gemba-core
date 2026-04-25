// gridViews — ui-spec §6.8 named default views (gm-5v8v.6.3).
//
// Five canonical views the operator can flip to with one click:
//
//   staged          state_category = staged
//   in-progress     state_category = started
//   blocked         human_action_required (open escalation / unmet
//                   guardrail / purview violation, surfaced server-side)
//   ready-to-stage  state_category = unstarted AND agent_claimable
//   recently-done   state_category = completed AND updated within 7d
//                   (sorted by updated_at desc by the page renderer)
//
// Each view ships a base BacklogFilter preset; views that need more
// than state_category to express their semantics also carry a
// post-filter predicate that runs on the page-rendered rows. The
// caller (GridPage) is responsible for sort overrides — the data
// here is pure metadata.

import type { BacklogFilter } from '@/lib/backlogFilter';
import type { WorkItem } from '@/types/core.gen';

export const NAMED_VIEWS = [
  'staged',
  'in-progress',
  'blocked',
  'ready-to-stage',
  'recently-done',
] as const;

export type NamedView = (typeof NAMED_VIEWS)[number];

export const VIEW_LABELS: Record<NamedView, string> = {
  'staged': 'Staged',
  'in-progress': 'In Progress',
  'blocked': 'Blocked',
  'ready-to-stage': 'Ready to stage',
  'recently-done': 'Recently Done',
};

// Base filter presets — enough to drive /api/work-items query params.
// Views that need more than state_category to express themselves
// (blocked, ready-to-stage) leave state_category as a superset so the
// fetch returns enough rows for the post-filter to narrow.
export const VIEW_FILTERS: Record<NamedView, BacklogFilter> = {
  'staged': { state_category: ['staged'], kind: [], search: '' },
  'in-progress': { state_category: ['started'], kind: [], search: '' },
  // Blocked work can be in any column — the predicate is "human
  // action required". An empty state_category list lets the SPA's
  // filter machinery treat it as "no filter" so every category
  // passes through to the post-filter.
  'blocked': { state_category: [], kind: [], search: '' },
  'ready-to-stage': { state_category: ['unstarted'], kind: [], search: '' },
  'recently-done': { state_category: ['completed'], kind: [], search: '' },
};

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

// Post-filter predicates run AFTER the API fetch + AFTER the search
// box's title-substring filter. Returning true keeps the row.
//
// The signals come from server-derived booleans (DerivedSignals,
// gm-gsh) — we don't try to re-derive them client-side. Rows without
// a `derived` block fall through as not-blocked / not-claimable.
export const VIEW_POST_FILTERS: Partial<Record<NamedView, (it: WorkItem) => boolean>> = {
  'blocked': (it) => Boolean(it.derived?.human_action_required),
  'ready-to-stage': (it) => Boolean(it.derived?.agent_claimable),
  'recently-done': (it) => {
    if (!it.updated_at) return true;
    const ts = Date.parse(it.updated_at);
    if (Number.isNaN(ts)) return true;
    return Date.now() - ts <= SEVEN_DAYS_MS;
  },
};

// Sort overrides applied AFTER post-filter. Currently only Recently
// Done needs a non-default sort — newest-first by update timestamp.
export const VIEW_SORTS: Partial<Record<NamedView, (a: WorkItem, b: WorkItem) => number>> = {
  'recently-done': (a, b) => {
    const at = a.updated_at ? Date.parse(a.updated_at) : 0;
    const bt = b.updated_at ? Date.parse(b.updated_at) : 0;
    return bt - at;
  },
};

// isKnownView — narrow an arbitrary string (URL param, storage entry)
// to a NamedView. Returns the typed value or null.
export function isKnownView(value: string | null | undefined): NamedView | null {
  if (!value) return null;
  return (NAMED_VIEWS as readonly string[]).includes(value) ? (value as NamedView) : null;
}
