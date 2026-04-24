import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { useState } from 'react';
import { WorkItemDrawer } from '../WorkItemDrawer';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';

// Seed a minimal CapabilitiesResponse so WorkItemDrawer's useCapabilities()
// has a manifest to read description_format from. Tests that care about
// markdown rendering pass `markdown`; the default here is undefined,
// which the renderer registry maps to the PlainDescription fallback.
function capsWith(format?: string): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'fake',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: false,
      description_format: format,
    },
    orchestration_plane: null,
  };
}

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

function wrapper(
  caps: CapabilitiesResponse = capsWith()
): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <CapabilitiesProvider initial={caps}>{children}</CapabilitiesProvider>
        </QueryClientProvider>
      </MemoryRouter>
    );
  };
}

function mockJSON(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('WorkItemDrawer', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders every tab for a fixture bead', async () => {
    fetchSpy.mockResolvedValueOnce(mockJSON(fixture));

    render(<WorkItemDrawer openId="gm-foo" onClose={() => {}} />, { wrapper: wrapper() });

    await waitFor(() => expect(screen.getByTestId('section-overview')).toBeTruthy());

    // Header: id + title + copy button. The title appears in both the
    // Dialog header and the editable TitleEditor (gm-root.8 slice 3),
    // so getAllByText is the correct shape.
    expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0);
    expect(screen.getByTestId('work-item-drawer-id').textContent).toBe('gm-foo');
    expect(screen.getByTestId('work-item-drawer-copy')).toBeTruthy();

    // Overview is always visible (pinned above the tabs). Labels chip
    // lives here too.
    expect(screen.getByTestId('section-overview')).toBeTruthy();
    expect(screen.getByText('milestone:m1')).toBeTruthy();

    // Default tab: Description. Close-reason also lives here because
    // it's a description-like narrative field.
    expect(screen.getByTestId('section-description')).toBeTruthy();
    expect(screen.getByTestId('section-close-reason')).toBeTruthy();
    expect(screen.getByText(/Line one/)).toBeTruthy();
    expect(screen.getByText('superseded by gm-bar')).toBeTruthy();

    // Edges tab: relationships split by direction + extension edges
    // from Custom["beads:*"]. Inactive tabs must not have mounted
    // their section, so asserting before click ensures the partition
    // really is working.
    expect(screen.queryByTestId('section-relationships')).toBeNull();
    act(() => {
      screen.getByTestId('drawer-tab-edges').click();
    });
    expect(screen.getByTestId('section-relationships')).toBeTruthy();
    expect(screen.getByTestId('relgroup-blocks')).toBeTruthy();
    expect(screen.getByTestId('relgroup-blocked-by')).toBeTruthy();
    expect(screen.getByTestId('relgroup-parent')).toBeTruthy();
    expect(screen.getByTestId('relgroup-children')).toBeTruthy();
    expect(screen.getByTestId('relgroup-relates-to')).toBeTruthy();
    expect(screen.getByTestId('relgroup-extension-edges')).toBeTruthy();
    expect(screen.getByText('gm-dep1')).toBeTruthy();
    expect(screen.getByText('gm-dep2')).toBeTruthy();

    // Evidence tab
    act(() => {
      screen.getByTestId('drawer-tab-evidence').click();
    });
    expect(screen.getByTestId('section-evidence')).toBeTruthy();
    expect(screen.getByText('implements drawer')).toBeTruthy();

    // DoD tab: section + informational banner + acceptance criterion
    act(() => {
      screen.getByTestId('drawer-tab-dod').click();
    });
    expect(screen.getByTestId('section-dod')).toBeTruthy();
    expect(screen.getByTestId('work-item-dod-banner')).toBeTruthy();
    expect(screen.getByText('Renders every section')).toBeTruthy();

    // Sprint tab
    act(() => {
      screen.getByTestId('drawer-tab-sprint').click();
    });
    expect(screen.getByTestId('section-sprint')).toBeTruthy();
    expect(screen.getByText('sprint-1')).toBeTruthy();

    // Activity tab: timestamps + derived signals live together here.
    act(() => {
      screen.getByTestId('drawer-tab-activity').click();
    });
    expect(screen.getByTestId('section-timestamps')).toBeTruthy();
    expect(screen.getByTestId('section-derived')).toBeTruthy();
    expect(screen.getByText(/human-action-required/)).toBeTruthy();

    // Extensions tab: conditional on the bead actually having custom
    // fields. The fixture has beads:* and gt:* so the tab is offered.
    act(() => {
      screen.getByTestId('drawer-tab-extensions').click();
    });
    expect(screen.getByTestId('section-custom')).toBeTruthy();
    expect(screen.getByText('beads')).toBeTruthy();
    expect(screen.getByText('gt')).toBeTruthy();
  });

  it('relationship click navigates to target bead and back', async () => {
    fetchSpy.mockImplementation((input: RequestInfo) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/work-items/gm-foo')) return Promise.resolve(mockJSON(fixture));
      if (url.endsWith('/work-items/gm-child')) return Promise.resolve(mockJSON(navigateTarget));
      return Promise.resolve(mockJSON({ error: 'session_not_found', message: '' }, 404));
    });

    render(<WorkItemDrawer openId="gm-foo" onClose={() => {}} />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0));

    // Relationships now live on the Edges tab (gm-e12.5).
    act(() => {
      screen.getByTestId('drawer-tab-edges').click();
    });

    // Click a relationship link — gm-child appears under "blocks".
    const link = screen.getAllByText('gm-child')[0];
    act(() => {
      link.click();
    });

    await waitFor(() => expect(screen.getAllByText('Navigated child').length).toBeGreaterThan(0));
    // Back button now exists (stack length > 1).
    const back = screen.getByTestId('work-item-drawer-back');
    act(() => {
      back.click();
    });
    await waitFor(() => expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0));
  });

  it('closing the drawer fires onClose', async () => {
    fetchSpy.mockResolvedValueOnce(mockJSON(fixture));
    const onClose = vi.fn();

    function Harness() {
      const [id, setId] = useState<string | null>('gm-foo');
      return (
        <WorkItemDrawer
          openId={id}
          onClose={() => {
            setId(null);
            onClose();
          }}
        />
      );
    }

    render(<Harness />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0));

    act(() => {
      screen.getByTestId('work-item-drawer-close').click();
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('renders nothing while closed', () => {
    render(<WorkItemDrawer openId={null} onClose={() => {}} />, { wrapper: wrapper() });
    expect(screen.queryByTestId('work-item-drawer-content')).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('shows loading then error when the fetch fails', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({ error: 'adaptor_degraded', message: 'reconnecting' }, 503)
    );
    render(<WorkItemDrawer openId="gm-foo" onClose={() => {}} />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('work-item-drawer-error')).toBeTruthy());
    expect(screen.getByTestId('work-item-drawer-error').textContent).toMatch(/reconnecting/);
  });

  // Description renderer is chosen from the manifest. When the adaptor
  // declares description_format="markdown", headings and lists must
  // render as real HTML, not literal `#` / `-` characters.
  it('renders markdown description when the adaptor declares it', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({
        ...fixture,
        description: '# Goal\n\n- first\n- second',
      })
    );
    render(<WorkItemDrawer openId="gm-foo" onClose={() => {}} />, {
      wrapper: wrapper(capsWith('markdown')),
    });
    // Lazy markdown chunk resolves → renders the <div data-testid="description-markdown">.
    // DoD notes also render through the same renderer, so there can be
    // more than one; assert on the first (Description section).
    const md = (await screen.findAllByTestId('description-markdown'))[0];
    expect(md.querySelector('h1')?.textContent).toBe('Goal');
    expect(md.querySelectorAll('li')).toHaveLength(2);
    expect(screen.queryByTestId('description-plain')).toBeNull();
  });

  it('falls back to plain description when format is missing / unknown', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({ ...fixture, description: '# not-a-heading' })
    );
    render(<WorkItemDrawer openId="gm-foo" onClose={() => {}} />, {
      wrapper: wrapper(capsWith(undefined)),
    });
    // DoD notes render through the same (plain) renderer, so there
    // can be more than one; assert on the first.
    await waitFor(() =>
      expect(screen.getAllByTestId('description-plain').length).toBeGreaterThan(0)
    );
    expect(screen.getAllByTestId('description-plain')[0].textContent).toBe('# not-a-heading');
  });
});
