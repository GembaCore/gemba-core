import { describe, expect, it, vi, afterEach } from 'vitest';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { HotkeysProvider } from '../provider';
import { HOTKEY_EVENT } from '../registry';

function fire(key: string, opts: Partial<KeyboardEventInit> = {}) {
  window.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...opts })
  );
}

let mountedRoots: Root[] = [];
let containers: HTMLElement[] = [];

function mount(node: React.ReactNode) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  mountedRoots.push(root);
  containers.push(container);
  return container;
}

afterEach(() => {
  act(() => mountedRoots.forEach((r) => r.unmount()));
  mountedRoots = [];
  containers.forEach((c) => c.remove());
  containers = [];
});

describe('HotkeysProvider', () => {
  it('registers default hotkeys and dispatches CustomEvent for unhandled shortcuts', () => {
    mount(<HotkeysProvider>{null}</HotkeysProvider>);
    const spy = vi.fn();
    window.addEventListener(`${HOTKEY_EVENT}:new`, spy);
    act(() => fire('n'));
    expect(spy).toHaveBeenCalledOnce();
    window.removeEventListener(`${HOTKEY_EVENT}:new`, spy);
  });

  it('fires two-key chord (g g) as goto-top', () => {
    mount(<HotkeysProvider>{null}</HotkeysProvider>);
    const spy = vi.fn();
    window.addEventListener(`${HOTKEY_EVENT}:goto-top`, spy);
    act(() => {
      fire('g');
      fire('g');
    });
    expect(spy).toHaveBeenCalledOnce();
    window.removeEventListener(`${HOTKEY_EVENT}:goto-top`, spy);
  });

  it('ignores keys typed inside inputs (except Escape and ?)', () => {
    const container = mount(
      <HotkeysProvider>
        <input data-testid="editor" />
      </HotkeysProvider>
    );
    const spy = vi.fn();
    window.addEventListener(`${HOTKEY_EVENT}:new`, spy);
    const input = container.querySelector('input')!;
    input.focus();
    act(() => {
      input.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'n', bubbles: true, cancelable: true })
      );
    });
    expect(spy).not.toHaveBeenCalled();
    window.removeEventListener(`${HOTKEY_EVENT}:new`, spy);
  });

  it('still fires Escape from inside an input (so overlays can close)', () => {
    const container = mount(
      <HotkeysProvider>
        <input />
      </HotkeysProvider>
    );
    const spy = vi.fn();
    window.addEventListener(`${HOTKEY_EVENT}:drawer-close`, spy);
    const input = container.querySelector('input')!;
    input.focus();
    act(() => {
      input.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
      );
    });
    expect(spy).toHaveBeenCalledOnce();
    window.removeEventListener(`${HOTKEY_EVENT}:drawer-close`, spy);
  });
});
