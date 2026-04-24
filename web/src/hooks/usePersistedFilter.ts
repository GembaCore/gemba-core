// usePersistedFilter (gm-e12.3.2). Persists a BacklogFilter into the
// URL hash (shareable) + localStorage (sticky across reloads).
//
// Init priority on mount:
//   1. URL hash (a shared link beats everything else)
//   2. localStorage (last-used filter from this browser)
//   3. defaults (caller-supplied)
//
// Every setFilter writes to both sinks. hashchange events from the
// browser (back/forward) reseed state so the user's nav history
// replays correctly.

import { useCallback, useEffect, useState } from 'react';
import {
  DEFAULT_BACKLOG_FILTER,
  parseFromHash,
  parseFromJSON,
  serializeToHash,
  type BacklogFilter,
} from '@/lib/backlogFilter';

export function usePersistedFilter(
  storageKey: string,
  defaults: BacklogFilter = DEFAULT_BACKLOG_FILTER
): [BacklogFilter, (next: BacklogFilter) => void] {
  const [filter, setFilterState] = useState<BacklogFilter>(() => {
    if (typeof window === 'undefined') return defaults;
    const fromHash = parseFromHash(window.location.hash);
    if (fromHash) return fromHash;
    const fromStorage = parseFromJSON(safeGet(storageKey));
    if (fromStorage) return fromStorage;
    return defaults;
  });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const handler = () => {
      const fromHash = parseFromHash(window.location.hash);
      // An explicit empty-hash navigation (e.g. the user deletes the
      // fragment in the address bar) should reset to defaults so the
      // URL stays in sync with what the UI renders.
      setFilterState(fromHash ?? defaults);
    };
    window.addEventListener('hashchange', handler);
    return () => window.removeEventListener('hashchange', handler);
  }, [defaults]);

  const setFilter = useCallback(
    (next: BacklogFilter) => {
      setFilterState(next);
      safeSet(storageKey, JSON.stringify(next));
      if (typeof window !== 'undefined') {
        const hash = serializeToHash(next);
        if (hash !== window.location.hash) {
          // replaceState (not pushState) so typing in the search box
          // doesn't build up 12 history entries per word.
          const url =
            hash.length > 0
              ? `${window.location.pathname}${window.location.search}${hash}`
              : `${window.location.pathname}${window.location.search}`;
          window.history.replaceState(null, '', url);
        }
      }
    },
    [storageKey]
  );

  return [filter, setFilter];
}

function safeGet(key: string): string | null {
  try {
    return typeof window !== 'undefined' ? window.localStorage.getItem(key) : null;
  } catch {
    // Storage can throw in Safari private mode and when the quota is
    // exceeded. Both shapes fall back to "no stored filter".
    return null;
  }
}

function safeSet(key: string, value: string): void {
  try {
    if (typeof window !== 'undefined') window.localStorage.setItem(key, value);
  } catch {
    // See safeGet.
  }
}
