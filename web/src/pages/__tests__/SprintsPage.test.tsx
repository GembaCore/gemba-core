// SprintsPage + SprintDetailPage tests (gm-e11.5, gm-szz0.1).
//
// Coverage:
//   - empty list when adaptor returns no sprints
//   - error state when /api/sprints fails
//   - happy path with sprints + work-items computes "X / Y done" per
//     sprint using the state_category contract
//   - detail page renders the sprint + work-item roster scoped via
//     /api/work-items?sprint_id
//
// gm-szz0.1: /sprints became the home of the PM persona's
// epic_order drawer (moved off /coach as part of the dispatch-vs-
// planning surface split). The drawer's own behaviour is covered
// by RecommendOrderDrawer.test.tsx; here we only assert that
// SprintsPage exposes the planner header + 'Recommend order'
// trigger and surfaces the drawer overlay on click.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { ReactNode } from 'react';
import { SprintsPage } from '../SprintsPage';
import { SprintDetailPage } from '../SprintDetailPage';

// emptyCoach is the planner/coach response shape with no ready
// beads or sessions — the gm-szz0.1 planner header on SprintsPage
// fetches /api/planner/coach so test routes need to satisfy it
// even when the test isn't asserting on the planner surface.
const emptyCoach = {
  sessions: [],
  ready_beads: [],
  conflicts: [],
  workspace: [],
  semantic: [],
  affinity: [],
  batches: [],
  notices: [],
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function wrapper(initial = '/'): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[initial]}>
          <Routes>
            <Route path="/sprints" element={children} />
            <Route path="/sprints/:id" element={children} />
            <Route path="/" element={children} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
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

function routeFetch(routes: Record<string, unknown>): void {
  fetchSpy.mockImplementation((req: RequestInfo | URL) => {
    const url = typeof req === 'string' ? req : req instanceof URL ? req.toString() : req.url;
    for (const path of Object.keys(routes)) {
      if (url.includes(path)) {
        return Promise.resolve(jsonResponse(routes[path]));
      }
    }
    return Promise.resolve(jsonResponse({ error: 'not_routed', message: url }, 404));
  });
}

describe('SprintsPage', () => {
  it('renders the empty state when no sprints', async () => {
    routeFetch({
      '/api/planner/coach': emptyCoach,
      '/api/sprints': { sprints: [], total: 0 },
      '/api/work-items': { items: [], total: 0 },
    });
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SprintsPage />
      </Wrapper>
    );
    expect(await screen.findByTestId('sprints-empty', undefined, { timeout: 3000 })).toBeTruthy();
  });

  it('lists sprints with computed X/Y done counts', async () => {
    routeFetch({
      '/api/planner/coach': emptyCoach,
      '/api/sprints': {
        sprints: [
          {
            id: 's1',
            name: 'Sprint Alpha',
            starts_at: '2026-04-01T00:00:00Z',
            ends_at: '2026-04-14T00:00:00Z',
          },
        ],
        total: 1,
      },
      '/api/work-items': {
        items: [
          { id: 'gm-1', title: 'A', sprint_id: 's1', status: 'open', state_category: 'started' },
          { id: 'gm-2', title: 'B', sprint_id: 's1', status: 'done', state_category: 'completed' },
          { id: 'gm-3', title: 'C', sprint_id: 's2', status: 'done', state_category: 'completed' },
        ],
        total: 3,
      },
    });
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SprintsPage />
      </Wrapper>
    );
    const row = await screen.findByTestId('sprint-row-s1', undefined, { timeout: 3000 });
    expect(row.textContent).toContain('Sprint Alpha');
    // Sprint Alpha has 2 items total, 1 completed; gm-3 is in s2 so excluded.
    expect(row.textContent).toContain('1 / 2 done');
  });

  // gm-szz0.1: the page hosts the PM persona's epic_order drawer
  // that used to live on /coach. Pin the planner header testids
  // and the drawer-open path so a future change can't silently
  // unmount the planning surface.
  it('renders the planner header with the Recommend order trigger', async () => {
    routeFetch({
      '/api/planner/coach': {
        ...emptyCoach,
        ready_beads: [
          {
            id: 'gm-1',
            title: 'A',
            kind: 'task',
            status: 'open',
            state_category: 'unstarted',
            repository: 'gemba',
          },
        ],
      },
      '/api/sprints': { sprints: [], total: 0 },
      '/api/work-items': { items: [], total: 0 },
    });
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SprintsPage />
      </Wrapper>
    );
    expect(
      await screen.findByTestId('sprints-planner-header', undefined, { timeout: 3000 })
    ).toBeTruthy();
    const btn = (await screen.findByTestId(
      'sprints-recommend-order',
      undefined,
      { timeout: 3000 }
    )) as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    // Drawer is closed by default; clicking the button mounts it.
    expect(screen.queryByTestId('recommend-order-drawer')).toBeNull();
    act(() => {
      fireEvent.click(btn);
    });
    expect(screen.getByTestId('recommend-order-drawer')).toBeTruthy();
  });

  it('shows error state on fetch failure', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse({ error: 'boom', message: 'sprints exploded' }, 500))
    );
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <SprintsPage />
      </Wrapper>
    );
    expect(await screen.findByTestId('sprints-error', undefined, { timeout: 3000 })).toBeTruthy();
  });
});

describe('SprintDetailPage', () => {
  it('renders the sprint with its scoped work items + done count', async () => {
    routeFetch({
      '/api/sprints': {
        sprints: [
          {
            id: 's1',
            name: 'Sprint Alpha',
            goal: 'Ship the cap-aware dispatcher',
            starts_at: '2026-04-01T00:00:00Z',
            ends_at: '2026-04-14T00:00:00Z',
          },
        ],
        total: 1,
      },
      '/api/work-items': {
        items: [
          { id: 'gm-1', title: 'A', sprint_id: 's1', status: 'open', state_category: 'started' },
          { id: 'gm-2', title: 'B', sprint_id: 's1', status: 'done', state_category: 'completed' },
        ],
        total: 2,
      },
    });
    const Wrapper = wrapper('/sprints/s1');
    render(
      <Wrapper>
        <SprintDetailPage />
      </Wrapper>
    );
    expect(await screen.findByTestId('sprint-detail-page', undefined, { timeout: 3000 })).toBeTruthy();
    expect(screen.getByTestId('row-gm-1')).toBeTruthy();
    expect(screen.getByTestId('row-gm-2')).toBeTruthy();
    // 1 of 2 completed.
    expect(screen.getByText('1')).toBeTruthy();
    expect(screen.getByText('/ 2')).toBeTruthy();
  });

  it('renders not-found when the id is unknown', async () => {
    routeFetch({
      '/api/sprints': { sprints: [], total: 0 },
      '/api/work-items': { items: [], total: 0 },
    });
    const Wrapper = wrapper('/sprints/nope');
    render(
      <Wrapper>
        <SprintDetailPage />
      </Wrapper>
    );
    expect(
      await screen.findByTestId('sprint-detail-not-found', undefined, { timeout: 3000 })
    ).toBeTruthy();
  });
});
