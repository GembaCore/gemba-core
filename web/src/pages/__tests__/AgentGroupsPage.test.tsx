// AgentGroupsPage tests (gm-e12.8). Smoke coverage for:
//   - empty state renders against the noop-shape (200 + groups: [])
//   - Gas Town-shape payload renders one AgentGroupBoard per group with
//     the right mode-pill (static / pool / graph)
//   - error state surfaces the message

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { AgentGroupsPage } from '../agent-groups/AgentGroupsPage';

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

describe('AgentGroupsPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders the empty state when no groups (noop adaptor)', async () => {
    fetchSpy.mockImplementation(() => Promise.resolve(jsonResponse({ groups: [], total: 0 })));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <AgentGroupsPage />
      </Wrapper>
    );
    expect(
      await screen.findByTestId('agent-groups-empty', undefined, { timeout: 3000 })
    ).toBeTruthy();
  });

  it('renders one AgentGroupBoard per group with the right mode-pills (Gas Town)', async () => {
    const body = {
      groups: [
        {
          id: 'role:mayor',
          display_name: 'Mayor',
          mode: 'static',
          members: { static: ['gemba/hq/mayor'] },
          extension: { role: 'mayor' },
        },
        {
          id: 'role:witness',
          display_name: 'Witnesses',
          mode: 'static',
          members: { static: ['gemba/hq/witness', 'gemba/sb/witness'] },
          repository: ['hq', 'sb'],
          extension: { role: 'witness' },
        },
        {
          id: 'rig:hq/polecats',
          display_name: 'hq polecats',
          mode: 'pool',
          members: {
            pool_check_url: 'gt://polecat/list?rig=hq',
            pool_min: 0,
            pool_max: 4,
          },
          repository: ['hq'],
          extension: { role: 'polecats', rig: 'hq' },
        },
        {
          id: 'graph:lg-flow',
          display_name: 'flow',
          mode: 'graph',
          members: {
            graph_topology_id: 'flow-v1',
            graph_resolved: ['gemba/lg/a', 'gemba/lg/b'],
          },
        },
      ],
      total: 4,
    };
    fetchSpy.mockImplementation(() => Promise.resolve(jsonResponse(body)));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <AgentGroupsPage />
      </Wrapper>
    );

    // Wait for the grid (one board per group).
    const grid = await screen.findByTestId('agent-groups-grid', undefined, {
      timeout: 3000,
    });
    expect(grid.children.length).toBe(4);

    // Every declared mode shows up as a pill.
    const pills = screen.getAllByTestId('agent-group-mode-pill').map((n) => n.textContent);
    // sort order is static → pool → graph (page sorts by MODE_ORDER).
    expect(pills.slice(0, 2).every((p) => p === 'static')).toBe(true);
    expect(pills[2]).toBe('pool');
    expect(pills[3]).toBe('graph');

    // Legend chips show counts.
    const legend = screen.getByTestId('agent-groups-legend');
    expect(legend.textContent).toMatch(/static · 2/);
    expect(legend.textContent).toMatch(/pool · 1/);
    expect(legend.textContent).toMatch(/graph · 1/);

    // Mode-specific testid surfaces show up at least once each.
    expect(screen.getAllByTestId('agent-group-static-tiles').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByTestId('agent-group-pool')).toBeTruthy();
    expect(screen.getByTestId('agent-group-graph')).toBeTruthy();
  });

  it('surfaces an error banner when /api/agent-groups fails', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        jsonResponse({ error: 'adaptor_degraded', message: 'orchestration plane is down' }, 503)
      )
    );
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <AgentGroupsPage />
      </Wrapper>
    );
    expect(
      await screen.findByTestId('agent-groups-error', undefined, { timeout: 3000 })
    ).toBeTruthy();
  });
});
