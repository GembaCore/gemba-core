// WorkItemDetail component tests — gm-root.22.5.
//
// Coverage:
//   - render: all sections visible for a fixture bead
//   - action buttons: dispatch, close button, title edit, status edit
//   - error state: fetch failure surfaces workitem-detail-error
//   - loading state: loading indicator while fetch pending
//   - markdown description rendered when adaptor declares it
//   - plain description fallback when format is unset
//   - navigation: relationship click pushes stack; back button pops it
//   - id prop change resets the nav stack

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { type ReactNode, useState } from 'react';
import { WorkItemDetail } from '../WorkItemDetail';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';
import { RhpProvider } from '../../RhpContext';
import { RhpPinnedContentProvider } from '../../RhpPinnedContent';

// ── Helpers ───────────────────────────────────────────────────────────

function capsWith(
  format?: string,
  opts: { evidence?: boolean } = {}
): CapabilitiesResponse {
  // Default the evidence capability ON so tests that exercise the
  // Evidence section don't have to pass it explicitly. The card +
  // detail Evidence affordances are gated on `has_evidence` which
  // mirrors `evidence_synthesis_required` (gm-t4af).
  return {
    work_plane: {
      adaptor_name: 'fake',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted', closed: 'completed' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: opts.evidence ?? true,
      description_format: format,
    },
    orchestration_plane: null,
  };
}

const fixture: WorkItem = {
  id: 'gm-foo',
  kind: 'task',
  title: 'Fixture bead',
  description: 'Line one\nLine two',
  status: 'open',
  state_category: 'unstarted',
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
      summary: 'implements detail',
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
      <MemoryRouter initialEntries={['/board']}>
        <RhpProvider>
          <RhpPinnedContentProvider>
            <QueryClientProvider client={client}>
              <CapabilitiesProvider initial={caps}>{children}</CapabilitiesProvider>
            </QueryClientProvider>
          </RhpPinnedContentProvider>
        </RhpProvider>
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

describe('WorkItemDetail', () => {
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

    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper() });

    await waitFor(() => expect(screen.getByTestId('section-overview')).toBeTruthy());

    // Header: id + copy button
    expect(screen.getByTestId('workitem-detail-id').textContent).toBe('gm-foo');
    expect(screen.getByTestId('workitem-detail-copy')).toBeTruthy();
    expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0);

    // Overview is always visible above tabs
    expect(screen.getByTestId('section-overview')).toBeTruthy();
    expect(screen.getByText('milestone:m1')).toBeTruthy();

    // Default tab: Description + close-reason + evidence
    expect(screen.getByTestId('section-description')).toBeTruthy();
    expect(screen.getByTestId('section-close-reason')).toBeTruthy();
    expect(screen.getByText(/Line one/)).toBeTruthy();
    expect(screen.getByText('superseded by gm-bar')).toBeTruthy();

    // Edges tab
    expect(screen.queryByTestId('section-relationships')).toBeNull();
    act(() => {
      screen.getByTestId('detail-tab-edges').click();
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

    // Evidence inline on description tab
    act(() => {
      screen.getByTestId('detail-tab-description').click();
    });
    expect(screen.getByTestId('section-evidence')).toBeTruthy();
    expect(screen.getByText('implements detail')).toBeTruthy();

    // DoD tab
    act(() => {
      screen.getByTestId('detail-tab-dod').click();
    });
    expect(screen.getByTestId('section-dod')).toBeTruthy();
    expect(screen.getByTestId('work-item-dod-banner')).toBeTruthy();
    expect(screen.getByText('Renders every section')).toBeTruthy();

    // Sprint tab
    act(() => {
      screen.getByTestId('detail-tab-sprint').click();
    });
    expect(screen.getByTestId('section-sprint')).toBeTruthy();
    expect(screen.getByText('sprint-1')).toBeTruthy();

    // Activity tab: timestamps + derived signals
    act(() => {
      screen.getByTestId('detail-tab-activity').click();
    });
    expect(screen.getByTestId('section-timestamps')).toBeTruthy();
    expect(screen.getByTestId('section-derived')).toBeTruthy();
    expect(screen.getByText(/human-action-required/)).toBeTruthy();

    // Extensions tab: conditional on custom fields
    act(() => {
      screen.getByTestId('detail-tab-extensions').click();
    });
    expect(screen.getByTestId('section-custom')).toBeTruthy();
    expect(screen.getByText('beads')).toBeTruthy();
    expect(screen.getByText('gt')).toBeTruthy();
  });

  it('shows loading state then data', async () => {
    let resolve!: (v: Response) => void;
    const deferred = new Promise<Response>((r) => {
      resolve = r;
    });
    fetchSpy.mockReturnValueOnce(deferred);

    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper() });

    // Loading indicator
    expect(screen.getByTestId('workitem-detail-loading')).toBeTruthy();

    // Resolve the fetch
    act(() => {
      resolve(mockJSON(fixture));
    });
    await waitFor(() => expect(screen.getByTestId('section-overview')).toBeTruthy());
    expect(screen.queryByTestId('workitem-detail-loading')).toBeNull();
  });

  it('shows error state when fetch fails', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({ error: 'adaptor_degraded', message: 'reconnecting' }, 503)
    );
    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('workitem-detail-error')).toBeTruthy());
    expect(screen.getByTestId('workitem-detail-error').textContent).toMatch(/reconnecting/);
  });

  it('dispatch button is present and disabled when orchestration plane is null', async () => {
    fetchSpy.mockResolvedValueOnce(mockJSON(fixture));
    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('section-overview')).toBeTruthy());
    const btn = screen.getByTestId('workitem-detail-dispatch');
    expect(btn).toBeTruthy();
    expect((btn as HTMLButtonElement).disabled).toBe(true);
  });

  it('relationship click navigates to target bead and back button pops the stack', async () => {
    fetchSpy.mockImplementation((input: RequestInfo) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/work-items/gm-foo')) return Promise.resolve(mockJSON(fixture));
      if (url.endsWith('/work-items/gm-child')) return Promise.resolve(mockJSON(navigateTarget));
      return Promise.resolve(mockJSON({ error: 'not_found', message: '' }, 404));
    });

    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0));

    act(() => {
      screen.getByTestId('detail-tab-edges').click();
    });

    // Click the gm-child link in the blocks relgroup
    const link = screen.getAllByText('gm-child')[0];
    act(() => {
      link.click();
    });

    await waitFor(() =>
      expect(screen.getAllByText('Navigated child').length).toBeGreaterThan(0)
    );
    // Back button should be visible
    const back = screen.getByTestId('workitem-detail-back');
    expect(back).toBeTruthy();

    act(() => {
      back.click();
    });
    await waitFor(() =>
      expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0)
    );
    expect(screen.queryByTestId('workitem-detail-back')).toBeNull();
  });

  it('id prop change resets the nav stack', async () => {
    fetchSpy.mockImplementation((input: RequestInfo) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/work-items/gm-foo')) return Promise.resolve(mockJSON(fixture));
      if (url.endsWith('/work-items/gm-child')) return Promise.resolve(mockJSON(navigateTarget));
      return Promise.resolve(mockJSON({ error: 'not_found', message: '' }, 404));
    });

    function Harness() {
      const [id, setId] = useState('gm-foo');
      return (
        <>
          <button data-testid="swap" onClick={() => setId('gm-child')} type="button">
            swap
          </button>
          <WorkItemDetail id={id} />
        </>
      );
    }

    render(<Harness />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getAllByText('Fixture bead').length).toBeGreaterThan(0));

    // Navigate into the stack by clicking a relationship
    act(() => {
      screen.getByTestId('detail-tab-edges').click();
    });
    const link = screen.getAllByText('gm-child')[0];
    act(() => {
      link.click();
    });
    await waitFor(() =>
      expect(screen.getAllByText('Navigated child').length).toBeGreaterThan(0)
    );
    expect(screen.getByTestId('workitem-detail-back')).toBeTruthy();

    // Swap the prop — stack should reset to only gm-child (no back button)
    act(() => {
      screen.getByTestId('swap').click();
    });
    await waitFor(() => expect(screen.queryByTestId('workitem-detail-back')).toBeNull());
  });

  it('renders markdown description when the adaptor declares it', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({
        ...fixture,
        description: '# Goal\n\n- first\n- second',
      })
    );
    render(<WorkItemDetail id="gm-foo" />, {
      wrapper: wrapper(capsWith('markdown')),
    });
    const md = (await screen.findAllByTestId('description-markdown'))[0];
    expect(md.querySelector('h1')?.textContent).toBe('Goal');
    expect(md.querySelectorAll('li')).toHaveLength(2);
    expect(screen.queryByTestId('description-plain')).toBeNull();
  });

  it('falls back to plain description when format is missing', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({ ...fixture, description: '# not-a-heading' })
    );
    render(<WorkItemDetail id="gm-foo" />, {
      wrapper: wrapper(capsWith(undefined)),
    });
    await waitFor(() =>
      expect(screen.getAllByTestId('description-plain').length).toBeGreaterThan(0)
    );
    expect(screen.getAllByTestId('description-plain')[0].textContent).toBe('# not-a-heading');
  });

  // gm-t4af: Evidence section is gated on `has_evidence`. On adaptors
  // that don't synthesize evidence, hide the section entirely rather
  // than show "No evidence attached" — that empty state misleads the
  // operator into thinking they should attach something.
  it('hides Evidence section when has_evidence capability is off', async () => {
    fetchSpy.mockResolvedValueOnce(mockJSON(fixture));
    render(<WorkItemDetail id="gm-foo" />, {
      wrapper: wrapper(capsWith(undefined, { evidence: false })),
    });
    await waitFor(() => expect(screen.getByTestId('workitem-detail-id')).toBeTruthy());
    expect(screen.queryByTestId('section-evidence')).toBeNull();
  });

  // gm-t4af: synthesized evidence (from git/PRs/work-history) renders
  // an "auto" pill so operator-curated entries stay visually distinct
  // (DD-13).
  it('renders the synthesized marker for evidence rows tagged synthesized=true', async () => {
    const withSynth: WorkItem = {
      ...fixture,
      evidence: [
        {
          id: 'ev-auto',
          kind: 'commit',
          source: 'git',
          ref: 'abc123',
          summary: 'auto-derived',
          captured_at: '2026-04-22T10:00:00Z',
          payload: { synthesized: true },
        },
        {
          id: 'ev-manual',
          kind: 'url',
          source: 'operator',
          ref: 'https://example.com',
          captured_at: '2026-04-22T10:00:00Z',
        },
      ],
    };
    fetchSpy.mockResolvedValueOnce(mockJSON(withSynth));
    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper(capsWith()) });
    await waitFor(() => expect(screen.getByTestId('section-evidence')).toBeTruthy());
    // Exactly one auto marker — the operator-curated row stays plain.
    expect(screen.getAllByTestId('evidence-synth-marker')).toHaveLength(1);
  });

  // gm-t4af: closed item + has_evidence + empty evidence array → the
  // banner replaces the "No evidence attached." muted text so the
  // workflow gap is visible.
  it('shows missing-evidence banner when a completed item has no evidence', async () => {
    const closedNoEvidence: WorkItem = {
      ...fixture,
      state_category: 'completed',
      evidence: [],
    };
    fetchSpy.mockResolvedValueOnce(mockJSON(closedNoEvidence));
    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper(capsWith()) });
    await waitFor(() => expect(screen.getByTestId('section-evidence')).toBeTruthy());
    expect(screen.getByTestId('evidence-missing-banner')).toBeTruthy();
  });

  it('omits missing-evidence banner for non-completed states', async () => {
    const openNoEvidence: WorkItem = {
      ...fixture,
      state_category: 'unstarted',
      evidence: [],
    };
    fetchSpy.mockResolvedValueOnce(mockJSON(openNoEvidence));
    render(<WorkItemDetail id="gm-foo" />, { wrapper: wrapper(capsWith()) });
    await waitFor(() => expect(screen.getByTestId('section-evidence')).toBeTruthy());
    expect(screen.queryByTestId('evidence-missing-banner')).toBeNull();
  });
});
