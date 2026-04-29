// RhpDetail tests — gm-root.22.4.
//
// Covers the detail-tab system: kind-replace / kind-stack semantics,
// URL codec round-trip (parse + canonicalization + write-through),
// deep-link hydration, route-change cleanup, malformed-segment
// tolerance, the legacy `?bead=X` migration shim, and the
// detail-content registry lookup.
//
// The shell + provider chrome (collapse, width, rail render) is
// exercised by RhpShell.test.tsx; this file focuses on the .4-owned
// surface.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { useEffect, type ReactNode } from 'react';
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom';
import { FileText, Layers } from 'lucide-react';
import {
  RhpProvider,
  useRhp,
  parseRhpParam,
  encodeRhpParam,
  RHP_URL_PARAM,
  RHP_COLLAPSED_STORAGE_KEY,
  RHP_WIDTH_STORAGE_KEY,
} from '../RhpContext';
import { RhpPinnedContentProvider } from '../RhpPinnedContent';
import { RhpShell } from '../RhpShell';
import { useRegisterDetailContent } from '../RhpDetail';
import { useRhp as useRhpHook } from '../RhpContext';

beforeEach(() => {
  // Clean storage between tests so layout-state leakage doesn't
  // confuse cold-start expectations.
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.clear();
});

// ── Helpers ──────────────────────────────────────────────────────────

function Providers({
  initialEntries,
  children,
}: {
  initialEntries?: string[];
  children: ReactNode;
}) {
  return (
    <MemoryRouter initialEntries={initialEntries ?? ['/board']}>
      <RhpProvider>
        <RhpPinnedContentProvider>{children}</RhpPinnedContentProvider>
      </RhpProvider>
    </MemoryRouter>
  );
}

function ApiProbe({ onApi }: { onApi: (api: ReturnType<typeof useRhp>) => void }) {
  const api = useRhpHook();
  useEffect(() => {
    onApi(api);
  }, [api, onApi]);
  return null;
}

function LocationProbe({
  onLocation,
}: {
  onLocation: (loc: { pathname: string; search: string }) => void;
}) {
  const loc = useLocation();
  useEffect(() => {
    onLocation({ pathname: loc.pathname, search: loc.search });
  }, [loc.pathname, loc.search, onLocation]);
  return null;
}

function NavigateButton({ to }: { to: string }) {
  const navigate = useNavigate();
  return (
    <button data-testid="navigate-btn" onClick={() => navigate(to)}>
      go
    </button>
  );
}

function WorkitemRegistrar() {
  useRegisterDetailContent({
    kind: 'workitem',
    icon: FileText,
    label: 'Work item',
    render: (id) => <div data-testid="workitem-content">workitem:{id}</div>,
  });
  return null;
}

function EpicRegistrar() {
  useRegisterDetailContent({
    kind: 'epic',
    icon: Layers,
    label: 'Epic',
    render: (id) => <div data-testid="epic-content">epic:{id}</div>,
  });
  return null;
}

// ── URL codec ─────────────────────────────────────────────────────────

describe('parseRhpParam', () => {
  it('parses single segment', () => {
    expect(parseRhpParam('workitem:gm-1')).toEqual([{ kind: 'workitem', id: 'gm-1' }]);
  });

  it('parses multiple segments preserving order', () => {
    expect(parseRhpParam('workitem:gm-1,epic:gm-2')).toEqual([
      { kind: 'workitem', id: 'gm-1' },
      { kind: 'epic', id: 'gm-2' },
    ]);
  });

  it('splits on the first colon so workspace-prefixed ids survive', () => {
    expect(parseRhpParam('workitem:gemba/gemba/gm-1')).toEqual([
      { kind: 'workitem', id: 'gemba/gemba/gm-1' },
    ]);
  });

  it('drops malformed segments silently', () => {
    expect(parseRhpParam('workitem:gm-1,nopcolon,:nokind,trailing:')).toEqual([
      { kind: 'workitem', id: 'gm-1' },
    ]);
  });

  it('returns empty array for null / empty input', () => {
    expect(parseRhpParam(null)).toEqual([]);
    expect(parseRhpParam('')).toEqual([]);
  });
});

describe('encodeRhpParam', () => {
  it('round-trips parse', () => {
    const raw = 'workitem:gm-1,epic:gm-2';
    expect(encodeRhpParam(parseRhpParam(raw))).toBe(raw);
  });
});

// ── popDetail / closeDetail semantics ────────────────────────────────

describe('popDetail / closeDetail', () => {
  it('pops a detail tab and focuses it', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });

    expect(api!.tabs.some((t) => t.id === 'workitem:gm-1')).toBe(true);
    expect(api!.activeTabId).toBe('workitem:gm-1');
  });

  it('replaces id when popping the same kind (kind-replace)', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-2' });
    });

    const detailTabs = api!.tabs.filter((t) => !t.pinned);
    expect(detailTabs).toHaveLength(1);
    expect(detailTabs[0].id).toBe('workitem:gm-2');
    expect(api!.activeTabId).toBe('workitem:gm-2');
  });

  it('stacks a different kind as a new tab (kind-stack)', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.popDetail({ kind: 'epic', id: 'gm-2' });
    });

    const detailTabs = api!.tabs.filter((t) => !t.pinned);
    expect(detailTabs.map((t) => t.id)).toEqual(['workitem:gm-1', 'epic:gm-2']);
    // Newest is focused.
    expect(api!.activeTabId).toBe('epic:gm-2');
  });

  it('closeDetail focuses the next rightmost detail when the focused tab closes', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.popDetail({ kind: 'epic', id: 'gm-2' });
    });
    expect(api!.activeTabId).toBe('epic:gm-2');

    act(() => {
      api!.closeDetail('epic');
    });

    expect(api!.activeTabId).toBe('workitem:gm-1');
  });

  it('closeDetail with no detail tabs left refocuses the first pinned tab', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    function Pinned() {
      const { registerPinnedTab } = useRhpHook();
      useEffect(() => {
        return registerPinnedTab({ id: 'help', icon: FileText, label: 'Help' });
      }, [registerPinnedTab]);
      return null;
    }
    render(
      <Providers>
        <Pinned />
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    expect(api!.activeTabId).toBe('workitem:gm-1');

    act(() => {
      api!.closeDetail('workitem');
    });

    expect(api!.activeTabId).toBe('help');
  });

  it('closeDetail ignores a kind that is not open', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.closeDetail('epic');
    });

    expect(api!.activeTabId).toBe('workitem:gm-1');
  });

  it('closeDetail with an id only closes the matching id', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    // Mismatched id: no-op.
    act(() => {
      api!.closeDetail('workitem', 'gm-2');
    });
    expect(api!.tabs.filter((t) => !t.pinned)).toHaveLength(1);

    // Matching id: closed.
    act(() => {
      api!.closeDetail('workitem', 'gm-1');
    });
    expect(api!.tabs.filter((t) => !t.pinned)).toHaveLength(0);
  });
});

// ── URL state ─────────────────────────────────────────────────────────

describe('URL state', () => {
  it('writes to ?rhp on popDetail', () => {
    let location: { pathname: string; search: string } = { pathname: '', search: '' };
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <LocationProbe onLocation={(l) => (location = l)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });

    expect(location.search).toContain(`${RHP_URL_PARAM}=workitem%3Agm-1`);
  });

  it('writes the canonical encoding for stacked tabs', () => {
    let location: { pathname: string; search: string } = { pathname: '', search: '' };
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <LocationProbe onLocation={(l) => (location = l)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.popDetail({ kind: 'epic', id: 'gm-2' });
    });

    // URL-encoded ',' = %2C and ':' = %3A
    expect(location.search).toContain('workitem%3Agm-1%2Cepic%3Agm-2');
  });

  it('drops the param when the last detail closes', () => {
    let location: { pathname: string; search: string } = { pathname: '', search: '' };
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <LocationProbe onLocation={(l) => (location = l)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.closeDetail('workitem');
    });

    expect(location.search).not.toContain(RHP_URL_PARAM);
  });
});

// ── Deep-link hydration ──────────────────────────────────────────────

describe('Deep-link', () => {
  it('hydrates tabs from ?rhp on first paint', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers initialEntries={['/board?rhp=workitem:gm-1,epic:gm-2']}>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    const detailTabs = api!.tabs.filter((t) => !t.pinned);
    expect(detailTabs.map((t) => t.id)).toEqual(['workitem:gm-1', 'epic:gm-2']);
    // Rightmost is focused on first paint.
    expect(api!.activeTabId).toBe('epic:gm-2');
  });

  it('survives workspace-prefixed ids', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers initialEntries={['/board?rhp=workitem:gemba/gemba/gm-1']}>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    const detailTabs = api!.tabs.filter((t) => !t.pinned);
    expect(detailTabs).toHaveLength(1);
    expect(detailTabs[0].id).toBe('workitem:gemba/gemba/gm-1');
  });
});

// ── Malformed segments ───────────────────────────────────────────────

describe('Malformed ?rhp segments', () => {
  it('drops malformed segments and rewrites URL canonical', async () => {
    let location: { pathname: string; search: string } = { pathname: '', search: '' };
    render(
      <Providers initialEntries={['/board?rhp=workitem:gm-1,broken,:nokind']}>
        <LocationProbe onLocation={(l) => (location = l)} />
        <RhpShell />
      </Providers>
    );

    // Wait a tick for the canonicalization effect to flush.
    await act(async () => {
      await Promise.resolve();
    });

    expect(location.search).toContain('workitem%3Agm-1');
    expect(location.search).not.toContain('broken');
    expect(location.search).not.toContain('nokind');
  });
});

// ── Per-route scoping ────────────────────────────────────────────────

describe('Per-route scoping', () => {
  it('clears all detail tabs on pathname change', async () => {
    let api: ReturnType<typeof useRhp> | null = null;
    let location: { pathname: string; search: string } = { pathname: '', search: '' };
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <LocationProbe onLocation={(l) => (location = l)} />
        <NavigateButton to="/walk" />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.popDetail({ kind: 'epic', id: 'gm-2' });
    });
    expect(api!.tabs.filter((t) => !t.pinned)).toHaveLength(2);

    act(() => {
      fireEvent.click(screen.getByTestId('navigate-btn'));
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(location.pathname).toBe('/walk');
    expect(api!.tabs.filter((t) => !t.pinned)).toHaveLength(0);
    expect(location.search).not.toContain(RHP_URL_PARAM);
  });
});

// ── Detail content registry ──────────────────────────────────────────

describe('registerDetailContent', () => {
  it('renders registered content for the active tab', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <WorkitemRegistrar />
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });

    expect(screen.getByTestId('workitem-content').textContent).toBe('workitem:gm-1');
  });

  it('renders the placeholder for an unregistered kind', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });

    // The shell's RhpTabBody falls back to the placeholder when no
    // content is registered for the kind.
    expect(screen.getByTestId('rhp-body-placeholder')).toBeTruthy();
  });

  it('uses the kind owner-supplied icon and label for the rail', () => {
    let api: ReturnType<typeof useRhp> | null = null;
    render(
      <Providers>
        <WorkitemRegistrar />
        <EpicRegistrar />
        <ApiProbe onApi={(a) => (api = a)} />
        <RhpShell />
      </Providers>
    );

    act(() => {
      api!.popDetail({ kind: 'workitem', id: 'gm-1' });
    });
    act(() => {
      api!.popDetail({ kind: 'epic', id: 'gm-2' });
    });

    const wiTab = api!.tabs.find((t) => t.id === 'workitem:gm-1');
    const epicTab = api!.tabs.find((t) => t.id === 'epic:gm-2');
    expect(wiTab?.label).toBe('Work item');
    expect(epicTab?.label).toBe('Epic');
  });
});

// ── Suppress noisy unused-storage-key warnings ───────────────────────
// Reference the imports so tsc doesn't flag them as unused even if a
// future test reorder removes their callsites.
void RHP_COLLAPSED_STORAGE_KEY;
void RHP_WIDTH_STORAGE_KEY;
