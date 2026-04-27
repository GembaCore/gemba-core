// InsightsPersonasPage tests (gm-twp2). Covers the three render
// branches: empty (no consults yet), populated (per-persona rollup),
// and 503 (dispatcher not configured). The query layer is faked
// via fetch stubs — same pattern the other page tests use.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { InsightsPersonasPage } from '../InsightsPersonasPage';

function wrapper(): (p: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }): JSX.Element {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    );
  };
}

function jsonResp(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('InsightsPersonasPage (gm-twp2)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // route the fake fetch by URL substring so the order tests fire
  // doesn't matter (useQuery's two queries fire in parallel).
  function routeBy(handlers: Record<string, () => Response>) {
    fetchSpy.mockImplementation(async (url: string) => {
      for (const [match, h] of Object.entries(handlers)) {
        if (url.includes(match)) return h();
      }
      throw new Error('unhandled fetch: ' + url);
    });
  }

  it('renders the loading state on first paint', () => {
    routeBy({
      '/api/consults': () => jsonResp({ consults: [], total: 0 }),
      '/api/skills': () => jsonResp({ skills: [], total: 0 }),
    });
    render(<InsightsPersonasPage />, { wrapper: wrapper() });
    expect(screen.getByTestId('insights-personas-loading')).toBeTruthy();
  });

  it('renders the empty state when no consults are present', async () => {
    routeBy({
      '/api/consults': () => jsonResp({ consults: [], total: 0 }),
      '/api/skills': () => jsonResp({ skills: [{ id: 'epic_order', name: 'Epic order', description: '', has_output_schema: true }], total: 1 }),
    });
    render(<InsightsPersonasPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('insights-personas-empty')).toBeTruthy());
    // Skill count hint surfaces.
    expect(screen.getByText(/1 skill registered/i)).toBeTruthy();
  });

  it('renders one row per persona aggregated from consults', async () => {
    routeBy({
      '/api/consults': () => jsonResp({
        consults: [
          {
            id: 'c1', persona_id: 'project-manager', skill_id: 'epic_order',
            workspace: 'gemba', status: 'completed',
            started_at: '2026-04-26T10:00:00Z', ended_at: '2026-04-26T10:01:00Z',
            line_count: 3, line_error_count: 0,
          },
          {
            id: 'c2', persona_id: 'project-manager', skill_id: 'epic_order',
            workspace: 'gemba', status: 'running',
            started_at: '2026-04-26T11:00:00Z',
            line_count: 1, line_error_count: 0,
          },
          {
            id: 'c3', persona_id: 'documentarian', skill_id: 'epic_order',
            workspace: 'gemba', status: 'failed',
            started_at: '2026-04-26T09:00:00Z', ended_at: '2026-04-26T09:00:30Z',
            line_count: 0, line_error_count: 0, error: 'spawn failed',
          },
        ],
        total: 3,
      }),
      '/api/skills': () => jsonResp({ skills: [], total: 0 }),
    });
    render(<InsightsPersonasPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('insights-personas-table')).toBeTruthy());

    // PM has 2 consults (one running), shown first because of inflight.
    const rows = screen.getAllByTestId(/^insights-persona-row-/);
    expect(rows[0].getAttribute('data-testid')).toBe('insights-persona-row-project-manager');
    expect(rows[1].getAttribute('data-testid')).toBe('insights-persona-row-documentarian');

    // Inflight badge on PM row, failed badge on documentarian.
    expect(screen.getByTestId('insights-personas-inflight')).toBeTruthy();
    expect(screen.getByTestId('insights-personas-failed')).toBeTruthy();
  });

  it('shows the dispatcher-unavailable banner on 503', async () => {
    routeBy({
      '/api/consults': () => jsonResp(
        { error: 'persona dispatcher not configured', code: 'unavailable', message: 'persona dispatcher not configured' },
        503,
      ),
      '/api/skills': () => jsonResp(
        { error: 'persona dispatcher not configured', code: 'unavailable', message: 'persona dispatcher not configured' },
        503,
      ),
    });
    render(<InsightsPersonasPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('insights-personas-503')).toBeTruthy());
  });

  it('shows an error block on non-503 fetch failures', async () => {
    routeBy({
      '/api/consults': () => jsonResp(
        { error: 'internal', code: 'oops', message: 'database fire' },
        500,
      ),
      '/api/skills': () => jsonResp({ skills: [], total: 0 }),
    });
    render(<InsightsPersonasPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('insights-personas-error')).toBeTruthy());
    // The dispatcher-503 banner must not fire — that branch is for
    // the lazy-attach 503 specifically.
    expect(screen.queryByTestId('insights-personas-503')).toBeNull();
  });
});
