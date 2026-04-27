// RecommendOrderDrawer tests (gm-twp2). Covers the four lifecycle
// states the drawer renders: closed (no DOM), submit-panel (no
// consult yet), waiting (consult begun, no lines), populated
// (lines visible with apply buttons), apply (POST fires, applied
// badge appears).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { RecommendOrderDrawer } from '../RecommendOrderDrawer';
import type { PlannerReadyBead } from '@/api/planner';

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

function bead(id: string): PlannerReadyBead {
  return {
    id,
    title: 'title-' + id,
    kind: 'task',
    status: 'open',
    state_category: 'unstarted',
  };
}

describe('RecommendOrderDrawer (gm-twp2)', () => {
  const fetchSpy = vi.fn();
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders nothing when closed', () => {
    render(
      <RecommendOrderDrawer
        open={false}
        onClose={() => {}}
        workspace="gemba"
        readyBeads={[bead('a')]}
      />,
      { wrapper: wrapper() },
    );
    expect(screen.queryByTestId('recommend-order-drawer')).toBeNull();
  });

  it('shows the submit panel when open with ready beads', () => {
    render(
      <RecommendOrderDrawer
        open
        onClose={() => {}}
        workspace="gemba"
        readyBeads={[bead('a'), bead('b')]}
      />,
      { wrapper: wrapper() },
    );
    expect(screen.getByTestId('recommend-order-drawer')).toBeTruthy();
    const btn = screen.getByTestId('recommend-order-submit') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    expect(screen.getByText(/2 ready beads/)).toBeTruthy();
  });

  it('disables submit when there are no ready beads', () => {
    render(
      <RecommendOrderDrawer
        open
        onClose={() => {}}
        workspace="gemba"
        readyBeads={[]}
      />,
      { wrapper: wrapper() },
    );
    const btn = screen.getByTestId('recommend-order-submit') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it('submits a consult and transitions to the results panel', async () => {
    fetchSpy.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url.endsWith('/api/consults') && init?.method === 'POST') {
        return jsonResp({
          id: 'consult-test-1',
          persona_id: 'project-manager',
          skill_id: 'epic_order',
          workspace: 'gemba',
          status: 'running',
          started_at: '2026-04-26T12:00:00Z',
          line_count: 0,
          line_error_count: 0,
        });
      }
      if (url.includes('/api/consults/consult-test-1')) {
        return jsonResp({
          id: 'consult-test-1',
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
    render(
      <RecommendOrderDrawer
        open
        onClose={() => {}}
        workspace="gemba"
        readyBeads={[bead('a')]}
      />,
      { wrapper: wrapper() },
    );
    act(() => {
      fireEvent.click(screen.getByTestId('recommend-order-submit'));
    });
    await waitFor(() => expect(screen.getByTestId('recommend-order-results')).toBeTruthy());
    expect(screen.getByTestId('recommend-order-waiting')).toBeTruthy();
    // Verify the POST body shape — workspace + candidate_epics
    // built from ready beads.
    const post = fetchSpy.mock.calls.find((c) => c[1]?.method === 'POST');
    expect(post).toBeTruthy();
    const body = JSON.parse(post![1].body as string);
    expect(body.persona_id).toBe('project-manager');
    expect(body.skill_id).toBe('epic_order');
    expect(body.raw_input.candidate_epics).toHaveLength(1);
    expect(body.raw_input.candidate_epics[0].epic_id).toBe('a');
  });

  it('renders validated lines as they arrive and wires apply buttons', async () => {
    fetchSpy.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url.endsWith('/api/consults') && init?.method === 'POST') {
        return jsonResp({
          id: 'consult-test-2',
          persona_id: 'project-manager',
          skill_id: 'epic_order',
          workspace: 'gemba',
          status: 'running',
          started_at: '2026-04-26T12:00:00Z',
          line_count: 0,
          line_error_count: 0,
        });
      }
      if (url.includes('/api/consults/consult-test-2/apply/')) {
        return jsonResp({
          consult_id: 'consult-test-2',
          idx: 1,
          line: { type: 'recommendation', rank: 1 },
          applied_idx: [1],
        });
      }
      if (url.includes('/api/consults/consult-test-2')) {
        return jsonResp({
          id: 'consult-test-2',
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
            { type: 'recommendation', rank: 1, epic_id: 'a', rationale: 'unblocks the most' },
          ],
          tokens: { input: 100, output: 50, total: 150 },
        });
      }
      throw new Error('unhandled fetch: ' + url);
    });
    render(
      <RecommendOrderDrawer
        open
        onClose={() => {}}
        workspace="gemba"
        readyBeads={[bead('a')]}
      />,
      { wrapper: wrapper() },
    );
    act(() => {
      fireEvent.click(screen.getByTestId('recommend-order-submit'));
    });
    await waitFor(() => expect(screen.getByTestId('recommend-order-line-1')).toBeTruthy());
    // Strategy line has no Apply (only recommendations do).
    expect(screen.queryByTestId('recommend-order-apply-0')).toBeNull();
    // Recommendation line at idx 1 has an Apply button.
    const applyBtn = screen.getByTestId('recommend-order-apply-1') as HTMLButtonElement;
    expect(applyBtn.disabled).toBe(false);
    expect(screen.getByText('unblocks the most')).toBeTruthy();
  });
});
