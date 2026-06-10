import type { Hotkey, HotkeyHandler } from './types';

export const HOTKEY_EVENT = 'gemba:hotkey';

export class HotkeyRegistry {
  private hotkeys = new Map<string, Hotkey>();
  private handlers = new Map<string, Map<number, HotkeyHandler>>();
  private scopes: string[] = ['global'];
  private listeners = new Set<() => void>();
  private handlerSeq = 0;

  register(hk: Hotkey): () => void {
    this.hotkeys.set(hk.id, hk);
    this.notify();
    return () => {
      this.hotkeys.delete(hk.id);
      this.notify();
    };
  }

  subscribe(id: string, handler: HotkeyHandler): () => void {
    let bucket = this.handlers.get(id);
    if (!bucket) {
      bucket = new Map();
      this.handlers.set(id, bucket);
    }
    const handlerId = ++this.handlerSeq;
    bucket.set(handlerId, handler);
    return () => {
      const b = this.handlers.get(id);
      if (!b) return;
      b.delete(handlerId);
      if (b.size === 0) this.handlers.delete(id);
    };
  }

  pushScope(scope: string): () => void {
    this.scopes.push(scope);
    this.notify();
    return () => {
      const i = this.scopes.lastIndexOf(scope);
      if (i >= 0) this.scopes.splice(i, 1);
      this.notify();
    };
  }

  isScopeActive(scope?: string): boolean {
    if (!scope || scope === 'global') return true;
    return this.scopes.includes(scope);
  }

  activeScopes(): string[] {
    return [...this.scopes];
  }

  all(): Hotkey[] {
    return Array.from(this.hotkeys.values());
  }

  active(): Hotkey[] {
    return this.all().filter((hk) => this.isScopeActive(hk.scope));
  }

  hasHandler(id: string): boolean {
    const b = this.handlers.get(id);
    return !!b && b.size > 0;
  }

  dispatch(id: string, event: KeyboardEvent): boolean {
    const hk = this.hotkeys.get(id);
    if (!hk || !this.isScopeActive(hk.scope)) return false;
    const bucket = this.handlers.get(id);
    if (bucket && bucket.size > 0) {
      for (const h of bucket.values()) h(hk, event);
    } else if (typeof window !== 'undefined') {
      window.dispatchEvent(new CustomEvent(HOTKEY_EVENT, { detail: { id, keys: hk.keys } }));
      window.dispatchEvent(new CustomEvent(`${HOTKEY_EVENT}:${id}`));
    }
    return true;
  }

  onChange(cb: () => void): () => void {
    this.listeners.add(cb);
    return () => {
      this.listeners.delete(cb);
    };
  }

  private notify() {
    this.listeners.forEach((l) => l());
  }
}
