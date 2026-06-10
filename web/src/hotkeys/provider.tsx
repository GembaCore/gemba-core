import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { HotkeyRegistry } from './registry';
import { ChordMatcher } from './matcher';
import { isEditableTarget } from './keys';
import { registerDefaults } from './defaults';
import { HotkeysContext } from './context';

export type HotkeysProviderProps = {
  children: ReactNode;
  // Test seam: skip registering defaults so tests can exercise the raw registry.
  withDefaults?: boolean;
};

export function HotkeysProvider({ children, withDefaults = true }: HotkeysProviderProps) {
  const registry = useMemo(() => new HotkeyRegistry(), []);
  const matcherRef = useRef<ChordMatcher>(new ChordMatcher());
  const [, forceTick] = useState(0);

  useEffect(() => registry.onChange(() => forceTick((t) => t + 1)), [registry]);

  useEffect(() => {
    if (!withDefaults) return;
    return registerDefaults(registry);
  }, [registry, withDefaults]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Allow Escape + help (`?`) even when an input is focused, so users
      // can always dismiss overlays and discover shortcuts.
      if (isEditableTarget(e.target) && e.key !== 'Escape' && e.key !== '?') {
        return;
      }
      const active = registry.active();
      const result = matcherRef.current.consume(e, active);
      if (result.type === 'match') {
        e.preventDefault();
        e.stopPropagation();
        registry.dispatch(result.hotkey.id, e);
      } else if (result.type === 'prefix') {
        e.preventDefault();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [registry]);

  return <HotkeysContext.Provider value={registry}>{children}</HotkeysContext.Provider>;
}
