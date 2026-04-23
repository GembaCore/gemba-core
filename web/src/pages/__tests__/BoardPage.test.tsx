import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { BoardPage } from '../BoardPage';
import { STATE_CATEGORIES, type WorkItem } from '@/types/core.gen';

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
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

  it('renders 5 columns with beads grouped by state_category', async () => {
    const data: WorkItem[] = [
      bead('gm-a', 'started'),
      bead('gm-b', 'unstarted'),
      bead('gm-c', 'unstarted'),
      bead('gm-d', 'completed'),
    ];
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(data), {
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
      new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
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
        new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } })
      );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-error')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });
});
