// RhpPinnedContent — lightweight registry for pinned-tab body content
// (gm-root.22.3).
//
// The main RhpAPI (gm-root.22.2/.4) does not include a body-content
// registry for pinned tabs — detail-tab body content is handled by the
// RhpDetailRegistryContext (.4). For the Help pinned tab (.3), we need a
// parallel mechanism that:
//
//   1. Lets HelpTab register its body renderer on mount.
//   2. Lets RhpShell look up that renderer by tab id.
//   3. Does NOT add to the public RhpAPI surface (which is owned by .2/.4).
//
// Pattern: the registry Map lives in state (so mutations trigger
// re-renders of consumers via context update). Using a new Map instance
// on each registration ensures referential inequality which lets useMemo
// in the provider produce a new context value.

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type JSX,
  type ReactNode,
} from 'react';

export interface RhpPinnedContentAPI {
  /** Register a renderer for a pinned tab id. Returns an unregister fn. */
  register(id: string, render: () => JSX.Element): () => void;
  /** Resolve the renderer for a pinned tab id. Returns null if not registered. */
  resolve(id: string): (() => JSX.Element) | null;
}

const RhpPinnedContentContext = createContext<RhpPinnedContentAPI | null>(null);

export function RhpPinnedContentProvider({ children }: { children: ReactNode }) {
  // Store the registry as React state so mutations trigger re-renders of
  // context consumers. Each update produces a new Map instance.
  const [registry, setRegistry] = useState<Map<string, () => JSX.Element>>(
    () => new Map()
  );

  const register = useCallback(
    (id: string, render: () => JSX.Element): (() => void) => {
      setRegistry((prev) => {
        const next = new Map(prev);
        next.set(id, render);
        return next;
      });
      return () => {
        setRegistry((prev) => {
          if (prev.get(id) !== render) return prev; // guards fast remount races
          const next = new Map(prev);
          next.delete(id);
          return next;
        });
      };
    },
    []
  );

  const resolve = useCallback(
    (id: string): (() => JSX.Element) | null => {
      return registry.get(id) ?? null;
    },
    [registry]
  );

  const value = useMemo<RhpPinnedContentAPI>(
    () => ({ register, resolve }),
    [register, resolve]
  );

  return (
    <RhpPinnedContentContext.Provider value={value}>
      {children}
    </RhpPinnedContentContext.Provider>
  );
}

export function useRhpPinnedContent(): RhpPinnedContentAPI {
  const ctx = useContext(RhpPinnedContentContext);
  if (!ctx) {
    throw new Error('useRhpPinnedContent: no RhpPinnedContentProvider in tree');
  }
  return ctx;
}
