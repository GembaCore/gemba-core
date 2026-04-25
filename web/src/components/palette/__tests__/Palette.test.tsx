import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import type { ReactNode } from 'react';
import { Palette } from '../Palette';
import { PaletteProvider } from '../PaletteContext';

// LocationProbe surfaces the current pathname into the DOM so tests
// can assert that selecting a palette item actually navigated.
function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="loc">{loc.pathname + loc.search}</div>;
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function renderPalette(opts?: {
  workItems?: Array<Record<string, unknown>>;
  agents?: Array<Record<string, unknown>>;
  sessions?: Array<Record<string, unknown>>;
  escalations?: Array<Record<string, unknown>>;
}): { qc: QueryClient } {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/board']}>
          <PaletteProvider>
            <Routes>
              <Route path="*" element={<>{children}<LocationProbe /></>} />
            </Routes>
          </PaletteProvider>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  // Seed the react-query cache directly — cleaner than stubbing fetch
  // for every endpoint and keeps the tests focused on palette UX.
  // Keys mirror workItemsKeys.list / agentsKeys.list / sessionsKeys.list /
  // escalationsKeys.list — see the corresponding hooks.
  qc.setQueryData(['beads'], opts?.workItems ?? []);
  qc.setQueryData(['agents'], opts?.agents ?? []);
  qc.setQueryData(['sessions', {}], opts?.sessions ?? []);
  qc.setQueryData(['escalations', 'all'], opts?.escalations ?? []);
  render(<Palette />, { wrapper: Wrapper });
  return { qc };
}

describe('Palette', () => {
  beforeEach(() => {
    // Stub fetch so any incidental useQuery refresh doesn't hit jsdom's
    // default ECONNREFUSED. The palette doesn't trigger fetches in the
    // happy paths these tests exercise; this is belt-and-braces.
    vi.stubGlobal('fetch', vi.fn(async () => json({ items: [], total: 0 })));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // gm-e12.6 DoD: opens via Mod+K. Second Mod+K closes (toggle).
  it('opens and closes on Mod+K', async () => {
    renderPalette();
    expect(screen.queryByTestId('command-palette-input')).toBeNull();

    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    await waitFor(() =>
      expect(screen.getByTestId('command-palette-input')).toBeTruthy()
    );

    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    await waitFor(() =>
      expect(screen.queryByTestId('command-palette-input')).toBeNull()
    );
  });

  // gm-e12.6: Ctrl+K is the cross-platform equivalent — operators on
  // Windows / Linux get the same shortcut.
  it('opens on Ctrl+K (non-mac)', async () => {
    renderPalette();
    act(() => {
      fireEvent.keyDown(document, { key: 'k', ctrlKey: true });
    });
    await waitFor(() =>
      expect(screen.getByTestId('command-palette-input')).toBeTruthy()
    );
  });

  // gm-e12.6: every primary route is reachable from the palette
  // without typing. Smoke-test by selecting the Backlog navigate item
  // and asserting the route changed.
  it('navigates when a palette item is selected', async () => {
    renderPalette();
    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    await waitFor(() =>
      expect(screen.getByTestId('command-palette-input')).toBeTruthy()
    );

    const item = screen.getByTestId('palette-item-nav-backlog');
    act(() => {
      fireEvent.click(item);
    });

    await waitFor(() =>
      expect(screen.getByTestId('loc').textContent).toBe('/backlog')
    );
    // Selecting an item closes the palette.
    expect(screen.queryByTestId('command-palette-input')).toBeNull();
  });

  // gm-e12.6: the search input filters by item keywords. Typing
  // "graph" leaves the dependency-graph navigate item visible and
  // hides unrelated ones.
  it('filters items by typed query', async () => {
    renderPalette();
    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    await waitFor(() =>
      expect(screen.getByTestId('command-palette-input')).toBeTruthy()
    );

    const input = screen.getByTestId('command-palette-input') as HTMLInputElement;
    act(() => {
      fireEvent.change(input, { target: { value: 'graph' } });
    });

    await waitFor(() =>
      expect(screen.getByTestId('palette-item-nav-graph')).toBeTruthy()
    );
    expect(screen.queryByTestId('palette-item-nav-backlog')).toBeNull();
  });

  // gm-e12.6: WorkItems in the cache surface as searchable rows. The
  // user types part of the title or short id and the result narrows.
  it('renders work items from the react-query cache', async () => {
    renderPalette({
      workItems: [
        {
          id: 'gemba/gemba/gm-foo',
          kind: 'task',
          title: 'finish the metrics endpoint',
          status: 'open',
          state_category: 'started',
        },
      ],
    });
    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    await waitFor(() =>
      expect(screen.getByTestId('command-palette-input')).toBeTruthy()
    );

    const input = screen.getByTestId('command-palette-input') as HTMLInputElement;
    act(() => {
      fireEvent.change(input, { target: { value: 'metrics' } });
    });
    await waitFor(() =>
      expect(screen.getByTestId('palette-item-wi-gemba/gemba/gm-foo')).toBeTruthy()
    );
  });

  // gm-e12.6: empty state shows when nothing matches.
  it('shows the empty state when no items match', async () => {
    renderPalette();
    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    await waitFor(() =>
      expect(screen.getByTestId('command-palette-input')).toBeTruthy()
    );

    const input = screen.getByTestId('command-palette-input') as HTMLInputElement;
    act(() => {
      fireEvent.change(input, { target: { value: 'zzzzznotamatch' } });
    });

    await waitFor(() =>
      expect(screen.getByText(/no results/i)).toBeTruthy()
    );
  });
});
