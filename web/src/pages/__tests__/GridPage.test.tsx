// GridPage smoke tests (gm-e12.3.3). Heavy lifting is in
// WorkItemGrid.test.tsx and BacklogPage.test.tsx — here we just
// confirm the route mounts, uses its own storage key, and exposes
// the presets menu (which the Backlog version does not).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { GridPage } from '../GridPage';
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
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

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('GridPage', () => {
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

  it('renders the grid with the presets button exposed', async () => {
    fetchSpy.mockResolvedValueOnce(json({ items: [wi('gm-g1')], total: 1 }));
    render(<GridPage />, { wrapper: wrapper() });

    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());
    // Presets toggle only shows up when the grid is configured
    // with a presets storage key — the Grid route enables it.
    expect(screen.getByTestId('grid-presets-toggle')).toBeTruthy();
  });

  it('uses its own storage key so it does not read Backlog filter state', async () => {
    // Seed the Backlog key with a filter the Grid should NOT pick up.
    window.localStorage.setItem(
      'gemba.backlog.filter',
      JSON.stringify({ state_category: ['canceled'], kind: [], search: '' })
    );
    fetchSpy.mockResolvedValueOnce(json({ items: [], total: 0 }));
    render(<GridPage />, { wrapper: wrapper() });

    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const [url] = fetchSpy.mock.calls[0] as [string];
    // Default state filter (backlog + unstarted) should apply, NOT canceled.
    expect(url).not.toContain('canceled');
    expect(url).toContain('state_category=backlog');
  });

  it('reads user presets from its own localStorage key', async () => {
    // Seed a user preset into the Grid key; Backlog must not see it.
    window.localStorage.setItem(
      'gemba.grid.column-presets',
      JSON.stringify([{ id: 'user:x', name: 'Pinned', visibility: { title: true, id: true } }])
    );
    fetchSpy.mockResolvedValueOnce(json({ items: [wi('gm-g1')], total: 1 }));
    render(<GridPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('work-item-grid')).toBeTruthy());

    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    expect(screen.getByTestId('grid-preset-user:x')).toBeTruthy();
  });
});
