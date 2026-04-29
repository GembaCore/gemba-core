// RecommendOrderDetail tests (gm-root.22.7). Covers the lifecycle states
// the detail tab renders: planner-loading, planner-error, submit panel
// (pre-consult), waiting (consult begun with no lines), populated (lines
// with apply buttons), and apply action. Mirrors the coverage from
// RecommendOrderDrawer.test.tsx, adapted to the RHP context and the new
// test-ids (recommend-order-detail-*).
//
// Tests render RecommendOrderDetailBody directly (same approach
// EpicDetail.test.tsx uses) rather than going through RhpDetailContent +
// the URL codec, which is already covered by RhpDetail.test.tsx.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { RecommendOrderDetailBody } from '../RecommendOrderDetail';

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

const coachWithBeads = {
  ...emptyCoach,
  ready_beads: [
    {
      id: 'gm-1',
      title: 'title-gm-1',
      kind: 'task',
      status: 'open',
      state_category: 'unstarted',
      repository: 'gemba',
    },
    {
      id: 'gm-2',
      title: 'title-gm-2',
      kind: 'task',
      status: 'open',
      state_category: 'unstarted',
      repository: 'gemba',
    },
  ],
};

describe('RecommendOrderDetail (gm-root.22.7)', () => {
  const fetchSpy = vi.fn();
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('shows loading state while planner is fetching', () => {
    // Never resolve so we stay in loading state.
    fetchSpy.mockImplementation(() => new Promise(() => {}));

    render(<RecommendOrderDetailBody workspace="gemba" />, { wrapper: wrapper() });

    expect(screen.getByTestId('recommend-order-detail-planner-loading')).toBeTruthy();
  });

  it('shows planner error when /api/planner/coach fails', async () => {
    fetchSpy.mockImplementation((url: string) => {
      if (url.includes('/api/planner/coach')) {
        return Promise.resolve(
          jsonResp({ error: 'boom', message: 'planner exploded' }, 500),
        );
      }
      return Promise.resolve(jsonResp({}, 404));
    });

    render(<RecommendOrderDetailBody workspace="gemba" />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail-planner-error')).toBeTruthy(),
    );
  });

  it('renders the submit panel with correct bead count when planner is loaded', async () => {
    fetchSpy.mockImplementation((url: string) => {
      if (url.includes('/api/planner/coach')) return Promise.resolve(jsonResp(coachWithBeads));
      return Promise.resolve(jsonResp({}, 404));
    });

    render(<RecommendOrderDetailBody workspace="gemba" />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail')).toBeTruthy(),
    );
    const btn = screen.getByTestId('recommend-order-detail-submit') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    expect(screen.getByText(/2 ready beads/)).toBeTruthy();
  });

  it('disables submit when planner reports no ready beads', async () => {
    fetchSpy.mockImplementation((url: string) => {
      if (url.includes('/api/planner/coach')) return Promise.resolve(jsonResp(emptyCoach));
      return Promise.resolve(jsonResp({}, 404));
    });

    render(<RecommendOrderDetailBody workspace="gemba" />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail')).toBeTruthy(),
    );
    const btn = screen.getByTestId('recommend-order-detail-submit') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it('submits a consult and transitions to the results panel', async () => {
    fetchSpy.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url.includes('/api/planner/coach')) return jsonResp(coachWithBeads);
      if (url.endsWith('/api/consults') && init?.method === 'POST') {
        return jsonResp({
          id: 'consult-detail-1',
          persona_id: 'project-manager',
          skill_id: 'epic_order',
          workspace: 'gemba',
          status: 'running',
          started_at: '2026-04-26T12:00:00Z',
          line_count: 0,
          line_error_count: 0,
        });
      }
      if (url.includes('/api/consults/consult-detail-1')) {
        return jsonResp({
          id: 'consult-detail-1',
          persona_id: 'project-manager',
          skill_id: 'epic_order',
          workspace: 'gemba',
          status: 'running',
          started_at: '2026-04-26T12:00:00Z',
          line_count: 0,
          line_error_count: 0,
          source: 'live',
          composed: { System: '', User: '' },
          composed_persisted: true,
          validated_lines: [],
          tokens: { input: 0, output: 0, total: 0 },
        });
      }
      throw new Error('unhandled fetch: ' + url);
    });

    render(<RecommendOrderDetailBody workspace="gemba" />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail-submit')).toBeTruthy(),
    );
    act(() => {
      fireEvent.click(screen.getByTestId('recommend-order-detail-submit'));
    });
    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail-results')).toBeTruthy(),
    );
    expect(screen.getByTestId('recommend-order-detail-waiting')).toBeTruthy();

    // Verify POST body shape.
    const post = fetchSpy.mock.calls.find((c) => c[1]?.method === 'POST');
    expect(post).toBeTruthy();
    const body = JSON.parse(post![1].body as string);
    expect(body.persona_id).toBe('project-manager');
    expect(body.skill_id).toBe('epic_order');
    expect(body.raw_input.candidate_epics).toHaveLength(2);
    expect(body.raw_input.candidate_epics[0].epic_id).toBe('gm-1');
  });

  it('renders validated lines and wires apply buttons', async () => {
    fetchSpy.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url.includes('/api/planner/coach')) return jsonResp(coachWithBeads);
      if (url.endsWith('/api/consults') && init?.method === 'POST') {
        return jsonResp({
          id: 'consult-detail-2',
          persona_id: 'project-manager',
          skill_id: 'epic_order',
          workspace: 'gemba',
          status: 'running',
          started_at: '2026-04-26T12:00:00Z',
          line_count: 0,
          line_error_count: 0,
        });
      }
      if (url.includes('/api/consults/consult-detail-2/apply/')) {
        return jsonResp({
          consult_id: 'consult-detail-2',
          idx: 1,
          line: { type: 'recommendation', rank: 1 },
          applied_idx: [1],
        });
      }
      if (url.includes('/api/consults/consult-detail-2')) {
        return jsonResp({
          id: 'consult-detail-2',
          persona_id: 'project-manager',
          skill_id: 'epic_order',
          workspace: 'gemba',
          status: 'running',
          started_at: '2026-04-26T12:00:00Z',
          line_count: 2,
          line_error_count: 0,
          source: 'live',
          composed: { System: '', User: '' },
          composed_persisted: true,
          validated_lines: [
            { type: 'strategy', reasoning: 'top-down by blocker depth' },
            { type: 'recommendation', rank: 1, epic_id: 'gm-1', rationale: 'unblocks the most' },
          ],
          tokens: { input: 100, output: 50, total: 150 },
        });
      }
      throw new Error('unhandled fetch: ' + url);
    });

    render(<RecommendOrderDetailBody workspace="gemba" />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail-submit')).toBeTruthy(),
    );
    act(() => {
      fireEvent.click(screen.getByTestId('recommend-order-detail-submit'));
    });
    await waitFor(() =>
      expect(screen.getByTestId('recommend-order-detail-line-1')).toBeTruthy(),
    );

    // Strategy line has no Apply button.
    expect(screen.queryByTestId('recommend-order-detail-apply-0')).toBeNull();
    // Recommendation line at idx 1 has an Apply button.
    const applyBtn = screen.getByTestId(
      'recommend-order-detail-apply-1',
    ) as HTMLButtonElement;
    expect(applyBtn.disabled).toBe(false);
    expect(screen.getByText('unblocks the most')).toBeTruthy();
  });
});
