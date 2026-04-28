// CoachPage tests (gm-szz0.1).
//
// Pre-gm-szz0.1, /coach mounted the PM persona's epic_order drawer
// AND a "Recommend order" button in its header. That conflated the
// dispatch surface ("what should I work on next?") with planning
// ("how should the next sprint be composed?"). gm-szz0.1 split the
// surfaces: the drawer + button moved to /sprints, and /coach
// keeps a single one-line link pointing operators at the planner.
//
// The dispatch grid + conflict highlights + agent strip are
// covered by the visual / e2e suites; this file pins the surface-
// split contract so a future refactor can't silently re-introduce
// the drawer here.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { CoachPage } from '../CoachPage';
import type { PlannerCoachResponse } from '@/api/planner';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

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

function emptyCoach(): PlannerCoachResponse {
  return {
    sessions: [],
    ready_beads: [],
    conflicts: [],
    workspace: [],
    semantic: [],
    affinity: [],
    batches: [],
    notices: [],
  };
}

const fetchSpy = vi.fn();

beforeEach(() => {
  vi.stubGlobal('fetch', fetchSpy);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchSpy.mockReset();
});

describe('CoachPage (gm-szz0.1 surface split)', () => {
  it('renders the planning link to /sprints', async () => {
    fetchSpy.mockResolvedValue(jsonResponse(emptyCoach()));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <CoachPage />
      </Wrapper>
    );
    const link = await screen.findByTestId('coach-planning-link', undefined, { timeout: 3000 });
    const anchor = link.querySelector('a');
    expect(anchor).not.toBeNull();
    expect(anchor?.getAttribute('href')).toBe('/sprints');
    expect(anchor?.textContent).toMatch(/Open the planner/);
  });

  it('does NOT mount the recommend-order button or drawer', async () => {
    fetchSpy.mockResolvedValue(jsonResponse(emptyCoach()));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <CoachPage />
      </Wrapper>
    );
    // Wait until the page has resolved its query (planning link
    // is rendered post-load).
    await screen.findByTestId('coach-planning-link', undefined, { timeout: 3000 });
    // The pre-gm-szz0.1 button / drawer testids must not appear.
    expect(screen.queryByTestId('coach-recommend-order')).toBeNull();
    expect(screen.queryByTestId('recommend-order-drawer')).toBeNull();
  });
});
