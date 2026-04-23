import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useState } from 'react';
import { BeadDrawer } from '../BeadDrawer';
import type { WorkItem } from '@/types/core.gen';

// Representative fixture that exercises every section the drawer
// renders. Drawer DoD (gm-qai): every WorkItem field visible; none
// silently dropped.
const fixture: WorkItem = {
  id: 'gm-foo',
  kind: 'task',
  title: 'Fixture bead',
  description: 'Line one\nLine two',
  status: 'in_progress',
  state_category: 'started',
  priority: 1,
  owner: {
    id: 'gemba/crew/mike',
    name: 'mike',
    agent_kind: 'human',
  },
  assignee: {
    id: 'gemba/polecats/obsidian',
    name: 'obsidian',
    agent_kind: 'agent',
    role: 'polecat',
    dialect: 'claude',
  },
  labels: ['milestone:m1', 'surface:frontend'],
  relationships: [
    { kind: 'blocks', from: 'gm-foo', to: 'gm-child' },
    { kind: 'blocks', from: 'gm-blocker', to: 'gm-foo' },
    { kind: 'parent_child', from: 'gm-epic', to: 'gm-foo' },
    { kind: 'parent_child', from: 'gm-foo', to: 'gm-sub' },
    { kind: 'relates_to', from: 'gm-foo', to: 'gm-other' },
  ],
  evidence: [
    {
      id: 'ev-1',
      kind: 'commit',
      source: 'git',
      ref: 'abc123',
      summary: 'implements drawer',
      captured_at: '2026-04-22T10:00:00Z',
    },
  ],
  dod: {
    acceptance_criteria: ['Renders every section'],
    notes: 'see bead',
    version: '1',
  },
  sprint_id: 'sprint-1',
  created_at: '2026-04-20T00:00:00Z',
  updated_at: '2026-04-22T12:00:00Z',
  custom: {
    'beads:notes': 'polecat notes',
    'beads:issue_type': 'task',
    'beads:close_reason': 'superseded by gm-bar',
    'beads:started_at': '2026-04-21T00:00:00Z',
    'beads:closed_at': '2026-04-22T00:00:00Z',
    'beads:dependencies': ['gm-dep1', { issue_id: 'gm-dep2', kind: 'discovered-from' }],
    'beads:dependents': ['gm-child'],
    'beads:budget': { limit: 100000, used: 42000, inform: 50000, warn: 80000, stop: 95000 },
    'gt:role': 'polecat',
  },
  derived: {
    agent_claimable: false,
    human_action_required: true,
    review_pending: false,
  },
};

const navigateTarget: WorkItem = {
  id: 'gm-child',
  kind: 'task',
  title: 'Navigated child',
  status: 'open',
  state_category: 'unstarted',
  created_at: '2026-04-22T00:00:00Z',
  updated_at: '2026-04-22T00:00:00Z',
};

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function mockJSON(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('BeadDrawer', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders every section for a fixture bead', async () => {
    fetchSpy.mockResolvedValueOnce(mockJSON(fixture));

    render(<BeadDrawer openId="gm-foo" onClose={() => {}} />, { wrapper: wrapper() });

    await waitFor(() => expect(screen.getByTestId('section-overview')).toBeTruthy());

    // Header: id + title + copy button
    expect(screen.getByText('Fixture bead')).toBeTruthy();
    expect(screen.getByTestId('bead-drawer-id').textContent).toBe('gm-foo');
    expect(screen.getByTestId('bead-drawer-copy')).toBeTruthy();

    // All top-level sections render
    expect(screen.getByTestId('section-overview')).toBeTruthy();
    expect(screen.getByTestId('section-description')).toBeTruthy();
    expect(screen.getByTestId('section-close-reason')).toBeTruthy();
    expect(screen.getByTestId('section-relationships')).toBeTruthy();
    expect(screen.getByTestId('section-evidence')).toBeTruthy();
    expect(screen.getByTestId('section-dod')).toBeTruthy();
    expect(screen.getByTestId('section-sprint')).toBeTruthy();
    expect(screen.getByTestId('section-derived')).toBeTruthy();
    expect(screen.getByTestId('section-timestamps')).toBeTruthy();
    expect(screen.getByTestId('section-custom')).toBeTruthy();

    // Relationship groups split correctly by direction
    expect(screen.getByTestId('relgroup-blocks')).toBeTruthy();
    expect(screen.getByTestId('relgroup-blocked-by')).toBeTruthy();
    expect(screen.getByTestId('relgroup-parent')).toBeTruthy();
    expect(screen.getByTestId('relgroup-children')).toBeTruthy();
    expect(screen.getByTestId('relgroup-relates-to')).toBeTruthy();
    expect(screen.getByTestId('relgroup-extension-edges')).toBeTruthy();

    // Extension edges surfaced from Custom["beads:*"]
    expect(screen.getByText('gm-dep1')).toBeTruthy();
    expect(screen.getByText('gm-dep2')).toBeTruthy();

    // Body fields propagated (testing-library normalizes whitespace,
    // so assert on each line separately).
    expect(screen.getByText(/Line one/)).toBeTruthy();
    expect(screen.getByText('superseded by gm-bar')).toBeTruthy();
    expect(screen.getByText('milestone:m1')).toBeTruthy();
    expect(screen.getByText('implements drawer')).toBeTruthy();
    expect(screen.getByText('Renders every section')).toBeTruthy();
    expect(screen.getByText('sprint-1')).toBeTruthy();

    // Derived pills
    expect(screen.getByText(/human-action-required/)).toBeTruthy();

    // Custom field namespaces rendered
    expect(screen.getByText('beads')).toBeTruthy();
    expect(screen.getByText('gt')).toBeTruthy();
  });

  it('relationship click navigates to target bead and back', async () => {
    fetchSpy.mockImplementation((input: RequestInfo) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/beads/gm-foo')) return Promise.resolve(mockJSON(fixture));
      if (url.endsWith('/beads/gm-child')) return Promise.resolve(mockJSON(navigateTarget));
      return Promise.resolve(mockJSON({ error: 'session_not_found', message: '' }, 404));
    });

    render(<BeadDrawer openId="gm-foo" onClose={() => {}} />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByText('Fixture bead')).toBeTruthy());

    // Click a relationship link — gm-child appears under "blocks".
    const link = screen.getAllByText('gm-child')[0];
    act(() => {
      link.click();
    });

    await waitFor(() => expect(screen.getByText('Navigated child')).toBeTruthy());
    // Back button now exists (stack length > 1).
    const back = screen.getByTestId('bead-drawer-back');
    act(() => {
      back.click();
    });
    await waitFor(() => expect(screen.getByText('Fixture bead')).toBeTruthy());
  });

  it('closing the drawer fires onClose', async () => {
    fetchSpy.mockResolvedValueOnce(mockJSON(fixture));
    const onClose = vi.fn();

    function Harness() {
      const [id, setId] = useState<string | null>('gm-foo');
      return (
        <BeadDrawer
          openId={id}
          onClose={() => {
            setId(null);
            onClose();
          }}
        />
      );
    }

    render(<Harness />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByText('Fixture bead')).toBeTruthy());

    act(() => {
      screen.getByTestId('bead-drawer-close').click();
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('renders nothing while closed', () => {
    render(<BeadDrawer openId={null} onClose={() => {}} />, { wrapper: wrapper() });
    expect(screen.queryByTestId('bead-drawer-content')).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('shows loading then error when the fetch fails', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({ error: 'adaptor_degraded', message: 'reconnecting' }, 503)
    );
    render(<BeadDrawer openId="gm-foo" onClose={() => {}} />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('bead-drawer-error')).toBeTruthy());
    expect(screen.getByTestId('bead-drawer-error').textContent).toMatch(/reconnecting/);
  });
});
