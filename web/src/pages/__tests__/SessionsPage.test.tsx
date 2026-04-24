// SessionsPage tests (gm-native.15). Smoke coverage for:
//   - empty state renders when the list is empty
//   - populated list renders one row per session with the bead id
//   - the End button fires a DELETE against the sessions API

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { SessionsPage } from '../SessionsPage';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    );
  };
}

describe('SessionsPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders the empty state when no sessions', async () => {
    fetchSpy.mockImplementation(() => Promise.resolve(jsonResponse({ sessions: [], total: 0 })));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SessionsPage />
      </Wrapper>
    );
    expect(await screen.findByText(/No live sessions/i, undefined, { timeout: 3000 })).toBeTruthy();
  });

  it('renders one row per session', async () => {
    const body = {
      sessions: [
        {
          id: 's-1',
          assignment_id: 'gm-foo',
          agent_id: 'gemba/polecats/obsidian',
          status: 'working',
          started_at: '2026-04-24T12:00:00Z',
          provider_metadata: { bead_id: 'gm-foo', agent_type: 'claude', worktree: '/tmp/wt' },
        },
        {
          id: 's-2',
          assignment_id: 'gm-bar',
          agent_id: 'gemba/polecats/topaz',
          status: 'ready',
          started_at: '2026-04-24T12:05:00Z',
          provider_metadata: { bead_id: 'gm-bar', agent_type: 'claude' },
        },
      ],
      total: 2,
    };
    fetchSpy.mockImplementation(() => Promise.resolve(jsonResponse(body)));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SessionsPage />
      </Wrapper>
    );
    expect(await screen.findByText('gm-foo', undefined, { timeout: 3000 })).toBeTruthy();
    expect(screen.getByText('gm-bar')).toBeTruthy();
    // Both rows should show an End button since neither is terminal.
    expect(screen.getAllByRole('button', { name: /End/ })).toHaveLength(2);
  });

  it('End button fires DELETE against /api/sessions/<id>', async () => {
    // First call: initial list. Second call: DELETE. Third: refetch.
    const liveBody = {
      sessions: [
        {
          id: 's-1',
          assignment_id: 'gm-foo',
          agent_id: 'gemba/polecats/obsidian',
          status: 'working',
          started_at: '2026-04-24T12:00:00Z',
          provider_metadata: { bead_id: 'gm-foo' },
        },
      ],
      total: 1,
    };
    fetchSpy.mockImplementation((url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        return Promise.resolve(
          jsonResponse({
            id: 's-1',
            assignment_id: 'gm-foo',
            agent_id: 'gemba/polecats/obsidian',
            status: 'completed',
            started_at: '2026-04-24T12:00:00Z',
          })
        );
      }
      return Promise.resolve(jsonResponse(liveBody));
    });

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SessionsPage />
      </Wrapper>
    );
    await screen.findByText('gm-foo', undefined, { timeout: 3000 });
    fireEvent.click(screen.getByRole('button', { name: /End/ }));
    await waitFor(() => {
      const deleteCall = fetchSpy.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'DELETE'
      );
      expect(deleteCall).toBeTruthy();
      expect(String(deleteCall?.[0])).toContain('/api/sessions/s-1');
      expect(String(deleteCall?.[0])).toContain('mode=canceled');
    });
  });
});
