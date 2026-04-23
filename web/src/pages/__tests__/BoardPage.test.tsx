import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { BoardPage } from '../BoardPage';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import { STATE_CATEGORIES, type WorkItem } from '@/types/core.gen';

// Seed a CapabilitiesProvider so BeadDrawer's useCapabilities() resolves.
// The board itself doesn't consult the manifest, but the drawer (rendered
// inside BoardPage) does for description_format.
const caps: CapabilitiesResponse = {
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

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={client}>
      <CapabilitiesProvider initial={caps}>{ui}</CapabilitiesProvider>
    </QueryClientProvider>
  );
}

function bead(id: string, category: WorkItem['state_category'], extra: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title ${id}`,
    status: 'open',
    state_category: category,
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...extra,
  };
}

describe('BoardPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('shows skeleton columns while loading', () => {
    fetchSpy.mockImplementation(() => new Promise(() => {}));
    render(wrap(<BoardPage />));
    expect(screen.getByTestId('board-loading')).toBeTruthy();
    expect(screen.getAllByTestId('board-skeleton-card').length).toBeGreaterThan(0);
  });

  // Mocks emit the real server wire shape — {items,total} — so the
  // test exercises listBeads' envelope unwrap (gm-root.1.8). Iterating
  // the envelope object directly would throw "TypeError: i is not
  // iterable" on line 26 of BoardPage; this is the integration test
  // called out in the bug's Definition of Done.
  it('renders 5 columns with beads grouped by state_category', async () => {
    const data: WorkItem[] = [
      bead('gm-a', 'started'),
      bead('gm-b', 'unstarted'),
      bead('gm-c', 'unstarted'),
      bead('gm-d', 'completed'),
    ];
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board')).toBeTruthy());

    for (const cat of STATE_CATEGORIES) {
      expect(screen.getByTestId(`board-column-${cat}`)).toBeTruthy();
    }
    const unstartedCol = screen.getByTestId('board-column-unstarted');
    expect(unstartedCol.querySelectorAll('[data-bead-id]')).toHaveLength(2);
  });

  it('shows empty state when the adaptor returns zero beads', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [], total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
  });

  // Integration: clicking a card fires /api/beads/{id} and surfaces the
  // BeadDrawer's loaded content (gm-qai wire-up). This pins the BoardPage
  // ↔ BeadDrawer contract so a future refactor can't accidentally
  // unmount one without the other.
  it('opens the drill-in drawer when a card is clicked', async () => {
    const listed = bead('gm-a', 'started');
    const detail: WorkItem = {
      ...listed,
      description: 'Deep dive into gm-a.',
    };
    fetchSpy.mockImplementation((url: string) => {
      if (url === '/api/beads') {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [listed], total: 1 }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      if (url === '/api/beads/gm-a') {
        return Promise.resolve(
          new Response(JSON.stringify(detail), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    render(wrap(<BoardPage />));
    const card = await waitFor(() => screen.getByRole('button', { name: /open bead gm-a/i }));
    fireEvent.click(card);

    // Drawer mounts, fetches /api/beads/gm-a, renders description.
    await waitFor(() => expect(screen.getByTestId('bead-drawer-content')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Deep dive into gm-a.')).toBeTruthy());
    expect(fetchSpy).toHaveBeenCalledWith('/api/beads/gm-a', expect.anything());
  });

  it('shows error state with a retry button that re-fetches', async () => {
    // 503/adaptor_degraded does not auto-retry (see useBeads.retry),
    // so the error surfaces immediately and the manual retry click
    // triggers exactly one additional fetch.
    fetchSpy
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'adaptor_degraded', message: 'reconnecting' }), {
          status: 503,
          headers: { 'Content-Type': 'application/json' },
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ items: [], total: 0 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-error')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });
});
