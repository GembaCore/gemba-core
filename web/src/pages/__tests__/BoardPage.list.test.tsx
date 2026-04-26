// BoardPage list-mode tests (gm-e12.19.1). Covers the slice of
// BacklogPage's behavior that survived the collapse: filter → query
// shape, client-side search, row-click drawer, empty + error states,
// and Backlog-preset state-category defaults from the URL.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { BoardPage } from '../BoardPage';
import { HotkeysProvider } from '@/hotkeys';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';

function caps(): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'fake',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: false,
    },
    orchestration_plane: null,
  };
}

function wrapper(initialUrl: string): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps()}>
          <HotkeysProvider>
            <MemoryRouter initialEntries={[initialUrl]}>{children}</MemoryRouter>
          </HotkeysProvider>
        </CapabilitiesProvider>
      </QueryClientProvider>
    );
  };
}

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-24T00:00:00Z',
    updated_at: '2026-04-24T00:00:00Z',
    ...patch,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const LIST_BACKLOG = '/board?view=list&preset=backlog';

describe('BoardPage list mode (gm-e12.19.1)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    window.localStorage.clear();
    window.history.replaceState(null, '', '/');
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('preset=backlog seeds the list with state_category={backlog,unstarted}', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });

    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    // The list view's effective filter — preset state_category in
    // {backlog, unstarted} — drives the work-items query. Other
    // calls (capabilities, etc.) may interleave, so scan all.
    const urls = fetchSpy.mock.calls.map((c) => c[0] as string);
    const wiCall = urls.find((u) => u.includes('/api/work-items') && u.includes('state_category='));
    expect(wiCall).toBeTruthy();
    const matches = (wiCall ?? '').match(/state_category=([^&]+)/g) ?? [];
    const values = matches.map((s) => s.replace('state_category=', '')).sort();
    expect(values).toEqual(['backlog', 'unstarted']);
  });

  it('renders rows for the returned items and shows the count', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        items: [wi('gm-1'), wi('gm-2', { title: 'second bead' })],
        total: 2,
      })
    );
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });

    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());
    expect(screen.getByTestId('grid-row-gm-1')).toBeTruthy();
    expect(screen.getByTestId('grid-row-gm-2')).toBeTruthy();
    expect(screen.getByTestId('board-list-count').textContent).toMatch(/2 items/);
  });

  it('client-side search narrows by title', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        items: [
          wi('gm-1', { title: 'alpha' }),
          wi('gm-2', { title: 'beta' }),
          wi('gm-3', { title: 'gamma' }),
        ],
        total: 3,
      })
    );
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });
    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());

    fireEvent.change(screen.getByTestId('board-list-search'), { target: { value: 'be' } });

    await waitFor(() => {
      expect(screen.queryByTestId('grid-row-gm-1')).toBeNull();
      expect(screen.getByTestId('grid-row-gm-2')).toBeTruthy();
      expect(screen.queryByTestId('grid-row-gm-3')).toBeNull();
    });
    expect(screen.getByTestId('board-list-count').textContent).toMatch(/3 items.*1 shown/);
  });

  it('toggling a state chip refetches with the explicit filter', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());

    fetchSpy.mockClear();
    act(() => {
      screen.getByTestId('board-list-state-completed').click();
    });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const urls = fetchSpy.mock.calls.map((c) => c[0] as string);
    const wiCall = urls.find((u) => u.includes('/api/work-items') && u.includes('state_category='));
    expect(wiCall).toBeTruthy();
    expect(wiCall ?? '').toMatch(/state_category=completed/);
  });

  it('clicking a row opens the WorkItemDrawer', async () => {
    const item = wi('gm-77', { title: 'drill-in target' });
    fetchSpy.mockResolvedValueOnce(jsonResponse({ items: [item], total: 1 }));
    fetchSpy.mockResolvedValueOnce(jsonResponse(item));
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });
    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());

    act(() => {
      screen.getByTestId('grid-row-gm-77').click();
    });

    const drawer = await screen.findByTestId('work-item-drawer-content');
    expect(within(drawer).getByTestId('work-item-drawer-id').textContent).toBe('gm-77');
  });

  it('shows the empty state when the adaptor returns zero items', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });
    await waitFor(() => expect(screen.getByTestId('board-list-empty')).toBeTruthy());
  });

  it('surfaces the server error message', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ error: 'adaptor_degraded', message: 'Dolt unreachable' }, 503)
    );
    render(<BoardPage />, { wrapper: wrapper(LIST_BACKLOG) });
    await waitFor(() => expect(screen.getByTestId('board-list-error')).toBeTruthy());
    expect(screen.getByTestId('board-list-error').textContent).toMatch(/Dolt unreachable/);
  });
});
