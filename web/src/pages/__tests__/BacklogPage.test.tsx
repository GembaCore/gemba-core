// Backlog page tests (gm-e12.9.1). Exercises the filter → query-string
// wire shape, the client-side search filter, row-click drawer opening,
// and the empty state.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { BacklogPage } from '../BacklogPage';
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

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps()}>
          <MemoryRouter>{children}</MemoryRouter>
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

describe('BacklogPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    // Reset URL-hash + localStorage so filter state doesn't leak
    // across tests via usePersistedFilter (gm-e12.3.2).
    window.localStorage.clear();
    window.history.replaceState(null, '', '/');
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('requests default state_category=backlog&unstarted on mount', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse({ items: [], total: 0 }));
    render(<BacklogPage />, { wrapper: wrapper() });

    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const [url] = fetchSpy.mock.calls[0] as [string];
    // Repeated params are the canonical wire shape — same field twice.
    const m = url.match(/state_category=([^&]+)/g) ?? [];
    const values = m.map((s) => s.replace('state_category=', '')).sort();
    expect(values).toEqual(['backlog', 'unstarted']);
  });

  it('renders rows for the returned items and shows the count', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResponse({
        items: [wi('gm-1'), wi('gm-2', { title: 'second bead' })],
        total: 2,
      })
    );
    render(<BacklogPage />, { wrapper: wrapper() });

    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());
    expect(screen.getByTestId('grid-row-gm-1')).toBeTruthy();
    expect(screen.getByTestId('grid-row-gm-2')).toBeTruthy();
    expect(screen.getByTestId('backlog-count').textContent).toMatch(/2 items/);
  });

  it('client-side search narrows by title', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResponse({
        items: [
          wi('gm-1', { title: 'alpha' }),
          wi('gm-2', { title: 'beta' }),
          wi('gm-3', { title: 'gamma' }),
        ],
        total: 3,
      })
    );
    render(<BacklogPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());

    const box = screen.getByTestId('backlog-search');
    fireEvent.change(box, { target: { value: 'be' } });

    await waitFor(() => {
      expect(screen.queryByTestId('grid-row-gm-1')).toBeNull();
      expect(screen.getByTestId('grid-row-gm-2')).toBeTruthy();
      expect(screen.queryByTestId('grid-row-gm-3')).toBeNull();
    });
    expect(screen.getByTestId('backlog-count').textContent).toMatch(/3 items.*1 shown/);
  });

  it('toggling a state chip refetches with the updated filter', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    render(<BacklogPage />, { wrapper: wrapper() });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());

    fetchSpy.mockClear();
    act(() => {
      screen.getByTestId('backlog-state-completed').click();
    });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const [url] = fetchSpy.mock.calls[0] as [string];
    expect(url).toMatch(/state_category=completed/);
  });

  it('clicking a row opens the WorkItemDrawer', async () => {
    const item = wi('gm-77', { title: 'drill-in target' });
    fetchSpy.mockResolvedValueOnce(jsonResponse({ items: [item], total: 1 }));
    // WorkItemDrawer will fetch /work-items/gm-77 on open.
    fetchSpy.mockResolvedValueOnce(jsonResponse(item));
    render(<BacklogPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());

    act(() => {
      screen.getByTestId('grid-row-gm-77').click();
    });

    const drawer = await screen.findByTestId('work-item-drawer-content');
    expect(within(drawer).getByTestId('work-item-drawer-id').textContent).toBe('gm-77');
  });

  it('shows the empty state when the adaptor returns zero items', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse({ items: [], total: 0 }));
    render(<BacklogPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('backlog-empty')).toBeTruthy());
  });

  it('surfaces the server error message', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResponse({ error: 'adaptor_degraded', message: 'Dolt unreachable' }, 503)
    );
    render(<BacklogPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('backlog-error')).toBeTruthy());
    expect(screen.getByTestId('backlog-error').textContent).toMatch(/Dolt unreachable/);
  });
});
