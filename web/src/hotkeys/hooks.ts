import { useCallback, useContext, useEffect, useRef, useSyncExternalStore } from 'react';
import { HotkeysContext } from './context';
import type { Hotkey, HotkeyHandler } from './types';
import type { HotkeyRegistry } from './registry';

export function useHotkeyRegistry(): HotkeyRegistry {
  const ctx = useContext(HotkeysContext);
  if (!ctx) {
    throw new Error('useHotkeyRegistry must be used inside <HotkeysProvider>');
  }
  return ctx;
}

export function useHotkey(id: string, handler: HotkeyHandler): void {
  const registry = useHotkeyRegistry();
  useEffect(() => registry.subscribe(id, handler), [registry, id, handler]);
}

export function useHotkeyScope(scope: string): void {
  const registry = useHotkeyRegistry();
  useEffect(() => registry.pushScope(scope), [registry, scope]);
}

// useSyncExternalStore requires a stable snapshot between renders when
// nothing has changed. Cache the array and only recompute on registry
// notifications.
export function useActiveHotkeys(): Hotkey[] {
  const registry = useHotkeyRegistry();
  const cacheRef = useRef<Hotkey[]>(registry.active());
  const subscribe = useCallback(
    (cb: () => void) =>
      registry.onChange(() => {
        cacheRef.current = registry.active();
        cb();
      }),
    [registry]
  );
  const getSnapshot = useCallback(() => cacheRef.current, []);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
