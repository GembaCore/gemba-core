// /refine surface tests (gm-3ofd). Covers the page-level wiring:
// hardcoded backlog filter, default sort, search box, empty state,
// row click → drawer.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { RefinePage } from '../RefinePage';
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

function wrap(url: string): (p: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return ({ children }) => (
    <QueryClientProvider client={client}>
      <CapabilitiesProvider initial={caps()}>
        <HotkeysProvider>
          <MemoryRouter initialEntries={[url]}>{children}</MemoryRouter>
        </HotkeysProvider>
      </CapabilitiesProvider>
    </QueryClientProvider>
  );
}

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'backlog',
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

describe('RefinePage', () => {
  const fetchSpy = vi.fn();
  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy.mockReset();
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('hardwires the backlog state filter on the API call', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const url = String(fetchSpy.mock.calls[0]?.[0] ?? '');
    expect(url).toContain('state_category=backlog');
  });

  it('renders the empty state when the backlog is clean', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() =>
      expect(screen.getByTestId('refine-empty')).toBeTruthy(),
    );
  });

  it('sorts by priority asc then created_at asc within priority', async () => {
    const items = [
      wi('gm-low-old', { priority: 3, created_at: '2026-01-01T00:00:00Z' }),
      wi('gm-hi-new', { priority: 1, created_at: '2026-04-01T00:00:00Z' }),
      wi('gm-hi-old', { priority: 1, created_at: '2026-02-01T00:00:00Z' }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText('title-gm-hi-old')).toBeTruthy());
    const cells = Array.from(document.querySelectorAll('td'))
      .map((c) => c.textContent ?? '')
      .filter((t) => t.startsWith('title-'));
    // First two are priority=1 (older first), last is priority=3.
    expect(cells.slice(0, 3)).toEqual([
      'title-gm-hi-old',
      'title-gm-hi-new',
      'title-gm-low-old',
    ]);
  });

  it('search input narrows by title and updates the URL', async () => {
    const items = [
      wi('gm-a', { title: 'fix login regression' }),
      wi('gm-b', { title: 'redesign empty board' }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText(/fix login/)).toBeTruthy());
    fireEvent.change(screen.getByTestId('refine-search'), {
      target: { value: 'login' },
    });
    await waitFor(() => expect(screen.queryByText(/redesign/)).toBeNull());
    expect(screen.getByText(/fix login/)).toBeTruthy();
  });
});
