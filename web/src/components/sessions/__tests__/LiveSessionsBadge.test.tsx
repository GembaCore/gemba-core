// gm-root.26 item 2 — topbar live-session count badge.
//
// Verifies the badge renders with the correct non-terminal count,
// hides when zero, and exposes a click-through link to /sessions
// with an aria-label that surfaces the count to assistive tech.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { LiveSessionsBadge } from '../LiveSessionsBadge';
import type { Session } from '@/api/sessions';

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <MemoryRouter>
      <QueryClientProvider client={client}>{ui}</QueryClientProvider>
    </MemoryRouter>
  );
}

function sess(id: string, status: Session['status']): Session {
  return {
    id,
    assignment_id: `${id}-assn`,
    agent_id: 'agent-1' as Session['agent_id'],
    status,
    started_at: '2026-04-29T00:00:00Z',
  };
}

describe('LiveSessionsBadge (gm-root.26 item 2)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('hides when there are zero non-terminal sessions', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ sessions: [], total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<LiveSessionsBadge />));
    // Wait for the query to settle, then assert nothing rendered.
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByTestId('live-sessions-badge')).toBeNull();
  });

  it('renders the live count when sessions exist', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          sessions: [
            sess('s1', 'working'),
            sess('s2', 'ready'),
            sess('s3', 'completed'), // terminal — excluded
            sess('s4', 'prompting'),
          ],
          total: 4,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );
    render(wrap(<LiveSessionsBadge />));
    const badge = await waitFor(() => screen.getByTestId('live-sessions-badge'));
    expect(badge.getAttribute('data-count')).toBe('3');
    expect(badge.getAttribute('aria-label')).toBe('3 live sessions');
    expect(badge.getAttribute('href')).toBe('/sessions');
  });

  it('uses singular "session" in aria-label when count is 1', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ sessions: [sess('s1', 'working')], total: 1 }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );
    render(wrap(<LiveSessionsBadge />));
    const badge = await waitFor(() => screen.getByTestId('live-sessions-badge'));
    expect(badge.getAttribute('aria-label')).toBe('1 live session');
  });
});
