import { describe, expect, it, vi } from 'vitest';
import { HotkeyRegistry, HOTKEY_EVENT } from '../registry';
import type { Hotkey } from '../types';

const HK: Hotkey = {
  id: 'new',
  keys: ['n'],
  description: 'New',
  category: 'creation',
};

function fakeEvent(): KeyboardEvent {
  return new KeyboardEvent('keydown', { key: 'n' });
}

describe('HotkeyRegistry', () => {
  it('registers and lists hotkeys', () => {
    const r = new HotkeyRegistry();
    const dispose = r.register(HK);
    expect(r.all()).toHaveLength(1);
    dispose();
    expect(r.all()).toHaveLength(0);
  });

  it('routes dispatch to a subscribed handler', () => {
    const r = new HotkeyRegistry();
    r.register(HK);
    const handler = vi.fn();
    r.subscribe('new', handler);
    r.dispatch('new', fakeEvent());
    expect(handler).toHaveBeenCalledOnce();
  });

  it('dispatches a CustomEvent when no handler is subscribed', () => {
    const r = new HotkeyRegistry();
    r.register(HK);
    const listener = vi.fn();
    window.addEventListener(HOTKEY_EVENT, listener);
    window.addEventListener(`${HOTKEY_EVENT}:new`, listener);
    r.dispatch('new', fakeEvent());
    expect(listener).toHaveBeenCalledTimes(2);
    window.removeEventListener(HOTKEY_EVENT, listener);
    window.removeEventListener(`${HOTKEY_EVENT}:new`, listener);
  });

  it('only dispatches when scope is active', () => {
    const r = new HotkeyRegistry();
    r.register({ ...HK, id: 'scoped', scope: 'drawer' });
    const handler = vi.fn();
    r.subscribe('scoped', handler);
    expect(r.dispatch('scoped', fakeEvent())).toBe(false);
    const pop = r.pushScope('drawer');
    expect(r.dispatch('scoped', fakeEvent())).toBe(true);
    expect(handler).toHaveBeenCalledOnce();
    pop();
    expect(r.dispatch('scoped', fakeEvent())).toBe(false);
  });

  it('filters to active hotkeys based on current scope stack', () => {
    const r = new HotkeyRegistry();
    r.register(HK);
    r.register({ ...HK, id: 'scoped', scope: 'drawer' });
    expect(r.active().map((h) => h.id)).toEqual(['new']);
    r.pushScope('drawer');
    expect(
      r
        .active()
        .map((h) => h.id)
        .sort()
    ).toEqual(['new', 'scoped']);
  });

  it('notifies subscribers on change', () => {
    const r = new HotkeyRegistry();
    const cb = vi.fn();
    r.onChange(cb);
    r.register(HK);
    expect(cb).toHaveBeenCalled();
  });
});
