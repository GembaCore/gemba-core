// RhpShell tests — gm-root.22.2.
//
// Cover render, focus, close, collapse, width persistence, and rail
// overflow scroll. The pinned-tab content (Help) and detail-tab
// content registries land in sibling beads (.3 / .4); this file
// exercises the shell + provider only via the typed `useRhp()` API.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { useEffect, type ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { Sparkles, BookOpen } from 'lucide-react';
import {
  RhpProvider,
  useRhp,
  RHP_COLLAPSED_STORAGE_KEY,
  RHP_WIDTH_STORAGE_KEY,
  RHP_WIDTH_MAX,
} from '../RhpContext';
import { RhpPinnedContentProvider } from '../RhpPinnedContent';
import { RhpShell } from '../RhpShell';

// Wraps the shell + provider in the minimum providers it needs:
// MemoryRouter (for useLocation/useSearchParams in RhpProvider — added
// when gm-root.22.4 wired the URL codec), and RhpPinnedContentProvider
// (for pinned-tab body resolution — gm-root.22.3 added this sibling
// context). Detail-content registry context is provided by RhpProvider
// itself.
function Providers({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <RhpProvider>
        <RhpPinnedContentProvider>{children}</RhpPinnedContentProvider>
      </RhpProvider>
    </MemoryRouter>
  );
}

// Test harness: mounts the provider + shell and exposes the API via
// a callback so tests can drive it imperatively.
function Harness({
  onApi,
}: {
  onApi?: (api: ReturnType<typeof useRhp>) => void;
}) {
  return (
    <Providers>
      <ApiProbe onApi={onApi} />
      <RhpShell />
    </Providers>
  );
}

function ApiProbe({ onApi }: { onApi?: (api: ReturnType<typeof useRhp>) => void }) {
  const api = useRhp();
  useEffect(() => {
    onApi?.(api);
  }, [api, onApi]);
  return null;
}

// HelpRegistrar — registers a pinned 'help' tab on mount, mirroring
// what the Help-tab bead (.3) will do once it lands.
function HelpRegistrar() {
  const { registerPinnedTab } = useRhp();
  useEffect(() => {
    return registerPinnedTab({ id: 'help', icon: BookOpen, label: 'Help' });
  }, [registerPinnedTab]);
  return null;
}

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    writable: true,
    value: width,
  });
  window.dispatchEvent(new Event('resize'));
}

describe('RhpShell', () => {
  beforeEach(() => {
    window.localStorage.clear();
    setViewportWidth(1024);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders cold-start collapsed when no tabs are registered', () => {
    render(<Harness />);
    const shell = screen.getByTestId('rhp-shell');
    expect(shell.dataset.collapsed).toBe('true');
    // Body is not rendered while collapsed.
    expect(screen.queryByTestId('rhp-body')).toBeNull();
  });

  it('registerPinnedTab adds an icon to the rail', () => {
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    expect(screen.getByTestId('rhp-tab-help')).toBeTruthy();
  });

  it('cold-start auto-expands when a tab is first registered', () => {
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    const shell = screen.getByTestId('rhp-shell');
    expect(shell.dataset.collapsed).toBe('false');
  });

  it('falls back to the rail when the viewport cannot fit main content and an expanded panel', () => {
    setViewportWidth(800);
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    const shell = screen.getByTestId('rhp-shell');
    expect(shell.dataset.collapsed).toBe('true');
    expect(shell.dataset.responsiveCollapsed).toBe('true');
    expect(screen.queryByTestId('rhp-body')).toBeNull();
    expect(screen.getByTestId('rhp-tab-help')).toBeTruthy();
  });

  it('clicking an icon focuses that tab', () => {
    let api: ReturnType<typeof useRhp> | undefined;
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <ApiProbe onApi={(a) => (api = a)} />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    // Help is the default-active pinned tab on first mount.
    expect(api!.activeTabId).toBe('help');
    // Pop a detail tab — focus shifts away from Help.
    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    expect(api!.activeTabId).toBe('workitem:gm-1');
    // Click the detail icon — explicit focus on the same kind keeps it
    // active and exercises the rail-icon click path.
    fireEvent.click(screen.getByTestId('rhp-tab-workitem:gm-1'));
    expect(api!.activeTabId).toBe('workitem:gm-1');
  });

  // gm-root.22.9: clicking a pinned tab (Help) while a detail tab is
  // open in ?rhp= must refocus the pinned tab. Before this fix the
  // activeTabId derivation fell back to the rightmost detail whenever
  // focusedKind was null, ignoring the operator's click on the rail.
  it('clicking a pinned tab while a detail is open refocuses the pinned tab (gm-root.22.9)', () => {
    let api: ReturnType<typeof useRhp> | undefined;
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <ApiProbe onApi={(a) => (api = a)} />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    // Pop a detail — Help loses focus, the detail wins (cold-start
    // rightmost-detail fallback after popDetail sets focusedKind).
    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    expect(api!.activeTabId).toBe('workitem:gm-1');
    // Click Help — explicit pinned focus must win over the
    // rightmost-detail fallback.
    fireEvent.click(screen.getByTestId('rhp-tab-help'));
    expect(api!.activeTabId).toBe('help');
    // Detail tab remains in the rail (URL ?rhp= is untouched).
    expect(screen.getByTestId('rhp-tab-workitem:gm-1')).toBeTruthy();
    // Clicking the detail icon switches focus back to it.
    fireEvent.click(screen.getByTestId('rhp-tab-workitem:gm-1'));
    expect(api!.activeTabId).toBe('workitem:gm-1');
  });

  it('closeTab removes a detail tab; no-op on pinned', () => {
    let api: ReturnType<typeof useRhp> | undefined;
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <ApiProbe onApi={(a) => (api = a)} />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    expect(api!.tabs.find((t) => t.id === 'workitem:gm-1')).toBeTruthy();

    act(() => {
      api!.closeTab('workitem:gm-1');
    });
    expect(api!.tabs.find((t) => t.id === 'workitem:gm-1')).toBeUndefined();
    // Help auto-refocuses.
    expect(api!.activeTabId).toBe('help');

    // closeTab on the pinned 'help' tab is a no-op.
    act(() => {
      api!.closeTab('help');
    });
    expect(api!.tabs.find((t) => t.id === 'help')).toBeTruthy();
  });

  it('collapse/expand toggles via the caret + persists across remount', () => {
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <RhpShell />
        </Providers>
      );
    }
    const { unmount } = render(<App />);
    // Auto-expanded after Help registers.
    expect(screen.getByTestId('rhp-shell').dataset.collapsed).toBe('false');

    fireEvent.click(screen.getByTestId('rhp-collapse-toggle'));
    expect(screen.getByTestId('rhp-shell').dataset.collapsed).toBe('true');
    expect(window.localStorage.getItem(RHP_COLLAPSED_STORAGE_KEY)).toBe('true');

    unmount();
    render(<App />);
    expect(screen.getByTestId('rhp-shell').dataset.collapsed).toBe('true');
  });

  it('drag-resize updates width and persists to localStorage', () => {
    let api: ReturnType<typeof useRhp> | undefined;
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <ApiProbe onApi={(a) => (api = a)} />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    // The provider's setWidth clamps + persists; that's the
    // load-bearing surface (drag handler delegates to it). Drive it
    // directly so we don't have to simulate pointer-capture in jsdom.
    act(() => {
      api!.setWidth(500);
    });
    expect(api!.width).toBe(500);
    expect(window.localStorage.getItem(RHP_WIDTH_STORAGE_KEY)).toBe('500');

    // Out-of-range values are clamped.
    act(() => {
      api!.setWidth(99999);
    });
    expect(api!.width).toBe(RHP_WIDTH_MAX);
    expect(window.localStorage.getItem(RHP_WIDTH_STORAGE_KEY)).toBe(String(RHP_WIDTH_MAX));
  });

  it('rail overflow: many pinned tabs scroll and the active tab is kept in view', () => {
    const scrollSpy = vi.fn();
    // Stub scrollIntoView per-instance — vitest.setup.ts only stubs
    // when the prototype lacks it; here we capture calls to assert.
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;

    function ManyTabs() {
      const { registerPinnedTab } = useRhp();
      useEffect(() => {
        const ids = Array.from({ length: 20 }, (_, i) => `pinned-${i}`);
        const unregs = ids.map((id) =>
          registerPinnedTab({ id, icon: Sparkles, label: id })
        );
        return () => {
          for (const u of unregs) u();
        };
      }, [registerPinnedTab]);
      return null;
    }

    let api: ReturnType<typeof useRhp> | undefined;
    function App() {
      return (
        <Providers>
          <ManyTabs />
          <ApiProbe onApi={(a) => (api = a)} />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    expect(api!.tabs.length).toBe(20);

    // Rail is the scrollable container — verify the overflow class
    // is present so it scrolls when content exceeds the viewport.
    const rail = screen.getByTestId('rhp-rail');
    expect(rail.className).toMatch(/overflow-y-auto/);

    // Focus a later tab and verify scrollIntoView fires.
    scrollSpy.mockClear();
    act(() => {
      api!.focusTab('pinned-15');
    });
    expect(scrollSpy).toHaveBeenCalled();
    const lastCall = scrollSpy.mock.calls.at(-1);
    expect(lastCall?.[0]).toMatchObject({ block: 'nearest' });

    Element.prototype.scrollIntoView = original;
  });

  it('renders a separator between pinned and detail tabs', () => {
    let api: ReturnType<typeof useRhp> | undefined;
    function App() {
      return (
        <Providers>
          <HelpRegistrar />
          <ApiProbe onApi={(a) => (api = a)} />
          <RhpShell />
        </Providers>
      );
    }
    render(<App />);
    // No separator yet — only pinned tabs.
    expect(screen.queryByTestId('rhp-rail-separator')).toBeNull();

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    expect(screen.getByTestId('rhp-rail-separator')).toBeTruthy();
  });
});
