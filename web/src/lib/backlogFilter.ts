// Backlog filter state — the small subset of user intent that lives
// in the URL hash and localStorage (gm-e12.3.2). The server-facing
// WorkItemListFilter (api/workItems.ts) is a superset; this module
// only models what the BacklogPage chip bar + search input control.

import { STATE_CATEGORIES, type StateCategory } from '@/types/core.gen';

export interface BacklogFilter {
  state_category: StateCategory[];
  kind: string[];
  search: string;
}

// DEFAULT_BACKLOG_FILTER is what a fresh /backlog visit shows when
// nothing is persisted: the two "planning" states (backlog + unstarted).
// Matches the seed behaviour shipped in gm-e12.9.1.
export const DEFAULT_BACKLOG_FILTER: BacklogFilter = {
  state_category: ['backlog', 'unstarted'],
  kind: [],
  search: '',
};

// serializeToHash produces the #… fragment that represents `filter`.
// Returns "" when the filter is empty so the address bar stays clean
// for the default-default case.
export function serializeToHash(filter: BacklogFilter): string {
  const p = new URLSearchParams();
  for (const v of filter.state_category) p.append('state_category', v);
  for (const v of filter.kind) p.append('kind', v);
  if (filter.search) p.set('search', filter.search);
  const qs = p.toString();
  return qs ? `#${qs}` : '';
}

// parseFromHash reverses serializeToHash. Returns null when the hash
// carries no recognised parameters (including the empty string), so
// callers can decide to fall back to localStorage / defaults. Unknown
// state_category values are dropped silently — a shared URL from a
// future build that introduces a new state shouldn't 500 the SPA.
export function parseFromHash(hash: string): BacklogFilter | null {
  const raw = hash.startsWith('#') ? hash.slice(1) : hash;
  if (!raw) return null;
  const p = new URLSearchParams(raw);
  const states = p.getAll('state_category').filter(isKnownState);
  const kinds = p.getAll('kind').filter((s) => s.length > 0);
  const search = p.get('search') ?? '';
  if (states.length === 0 && kinds.length === 0 && !search) return null;
  return { state_category: states, kind: kinds, search };
}

function isKnownState(s: string): s is StateCategory {
  return (STATE_CATEGORIES as readonly string[]).includes(s);
}

// parseFromJSON deserialises the JSON blob stored in localStorage.
// Returns null on any parse failure or shape mismatch so a corrupt
// storage entry doesn't brick the page.
export function parseFromJSON(raw: string | null): BacklogFilter | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw) as unknown;
    if (!v || typeof v !== 'object') return null;
    const o = v as Partial<BacklogFilter>;
    if (!Array.isArray(o.state_category) || !Array.isArray(o.kind)) return null;
    const states = o.state_category.filter(
      (s): s is StateCategory => typeof s === 'string' && isKnownState(s)
    );
    const kinds = o.kind.filter((s): s is string => typeof s === 'string');
    const search = typeof o.search === 'string' ? o.search : '';
    return { state_category: states, kind: kinds, search };
  } catch {
    return null;
  }
}
