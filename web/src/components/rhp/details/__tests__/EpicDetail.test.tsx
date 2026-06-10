// EpicDetail tests — gm-root.22.6.
//
// Covers:
//   - Loading / error / body render states.
//   - Action-button enablement gates (Stage / Start / Dispatch / New child).
//   - Stage button fires PATCH with X-GEMBA-Confirm header.
//   - Registration: EpicDetailRegistration registers kind='epic' with icon + label.
//
// The RHP shell, URL codec, and route-change scoping are exercised by
// RhpDetail.test.tsx — this file focuses on EpicDetail's own surface.

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { EpicDetail, EpicDetailRegistration } from '../EpicDetail';
import { workItemsKeys } from '@/hooks/useWorkItems';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import { KIND_MILESTONE, type StateCategory, type WorkItem } from '@/types/core.gen';
import { RhpProvider, useRhpDetailRegistry } from '@/components/rhp/RhpContext';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
import { useEffect, useState } from 'react';

const caps: CapabilitiesResponse = {
  work_plane: {
    adaptor_name: 'fake',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'unstarted' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  },
  orchestration_plane: null,
};

function epicFixture(
  overrides: Partial<WorkItem> & { state_category?: StateCategory } = {}
): WorkItem {
  const { state_category = 'unstarted', ...rest } = overrides;
  return {
    id: 'demo/pc-e-test',
    kind: 'epic',
    title: 'Test epic',
    status: 'open',
    state_category,
    priority: 1,
    created_at: '2026-04-10T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...rest,
  };
}

function mount(epic: WorkItem, allItems: WorkItem[] = [epic]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(workItemsKeys.detail(epic.id), epic);
  client.setQueryData(workItemsKeys.list(), allItems);
  const ui: ReactNode = (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>
          <RhpProvider>
            <RhpPinnedContentProvider>
              <EpicDetail id={epic.id} />
            </RhpPinnedContentProvider>
          </RhpProvider>
        </CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  return { ...render(ui), client };
}

describe('EpicDetail — rendering', () => {
  it('renders the id label in the header', async () => {
    mount(epicFixture());
    await waitFor(() => expect(screen.getByTestId('epic-detail-id')).toBeTruthy());
    expect(screen.getByTestId('epic-detail-id').textContent).toBe('demo/pc-e-test');
  });

  it('renders the state section with state_category', async () => {
    mount(epicFixture({ state_category: 'staged', status: 'staged' }));
    const section = await screen.findByTestId('epic-section-state');
    expect(section.textContent).toContain('staged');
  });

  it('renders the description section when present', async () => {
    mount(epicFixture({ description: 'An important epic.' }));
    await waitFor(() => expect(screen.getByTestId('epic-section-description')).toBeTruthy());
    expect(screen.getByText('An important epic.')).toBeTruthy();
  });

  it('renders children section with count 0 when no children', async () => {
    mount(epicFixture());
    const section = await screen.findByTestId('epic-section-children');
    expect(section.textContent).toContain('Children (0)');
  });

  it('shows the milestone > epic breadcrumb when the epic has a milestone ancestor', async () => {
    const milestone: WorkItem = {
      ...epicFixture(),
      id: 'demo/pc-m1',
      kind: KIND_MILESTONE,
      title: 'M1 Foundation',
      relationships: [],
    };
    const epic = epicFixture({
      relationships: [{ kind: 'parent_child', from: milestone.id, to: 'demo/pc-e-test' }],
    });

    mount(epic, [milestone, epic]);

    const breadcrumb = await screen.findByTestId('workitem-detail-breadcrumb');
    expect(breadcrumb.textContent).toContain('Milestone');
    expect(breadcrumb.textContent).toContain('M1 Foundation');
    expect(breadcrumb.textContent).toContain('Epic');
    expect(breadcrumb.textContent).toContain('Test epic');
  });
});

describe('EpicDetail — Stage / Start workers actions (gm-vzy)', () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders Stage, Start, Dispatch, and New child buttons', async () => {
    mount(epicFixture());
    await waitFor(() => expect(screen.getByTestId('epic-detail-stage')).toBeTruthy());
    expect(screen.getByTestId('epic-detail-start')).toBeTruthy();
    expect(screen.getByTestId('epic-detail-dispatch')).toBeTruthy();
    expect(screen.getByTestId('epic-detail-new-child')).toBeTruthy();
  });

  it('disables both Stage and Start when derived.agent_claimable is false', async () => {
    mount(
      epicFixture({
        state_category: 'unstarted',
        derived: { agent_claimable: false, human_action_required: false, review_pending: false },
      })
    );
    const stage = await screen.findByTestId('epic-detail-stage');
    const start = screen.getByTestId('epic-detail-start');
    expect(stage.hasAttribute('disabled')).toBe(true);
    expect(start.hasAttribute('disabled')).toBe(true);
    expect(stage.getAttribute('title')).toMatch(/agent-claimable/i);
  });

  it('enables Stage but not Start when claimable + unstarted', async () => {
    mount(
      epicFixture({
        state_category: 'unstarted',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      })
    );
    const stage = await screen.findByTestId('epic-detail-stage');
    expect(stage.hasAttribute('disabled')).toBe(false);
    const start = screen.getByTestId('epic-detail-start');
    expect(start.hasAttribute('disabled')).toBe(true);
    expect(start.getAttribute('title')).toMatch(/stage the epic/i);
  });

  it('enables Start but not Stage when claimable + staged', async () => {
    mount(
      epicFixture({
        state_category: 'staged',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      })
    );
    const stage = await screen.findByTestId('epic-detail-stage');
    const start = screen.getByTestId('epic-detail-start');
    expect(start.hasAttribute('disabled')).toBe(false);
    expect(stage.hasAttribute('disabled')).toBe(true);
    expect(stage.getAttribute('title')).toMatch(/already staged/i);
  });

  it('disables Start when already started', async () => {
    mount(
      epicFixture({
        state_category: 'started',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      })
    );
    const start = await screen.findByTestId('epic-detail-start');
    expect(start.hasAttribute('disabled')).toBe(true);
    expect(start.getAttribute('title')).toMatch(/already started/i);
  });

  it('clicking a disabled Stage is a no-op (no PATCH fired)', async () => {
    mount(
      epicFixture({
        state_category: 'unstarted',
        derived: { agent_claimable: false, human_action_required: false, review_pending: false },
      })
    );
    const stage = await screen.findByTestId('epic-detail-stage');
    fireEvent.click(stage);
    await Promise.resolve();
    const patches = fetchSpy.mock.calls.filter(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
    );
    expect(patches).toHaveLength(0);
  });

  it('clicking Stage when enabled PATCHes /api/work-items/:id with X-GEMBA-Confirm', async () => {
    fetchSpy.mockResolvedValue(
      new Response(
        JSON.stringify({
          ...epicFixture({
            state_category: 'staged',
            derived: { agent_claimable: true, human_action_required: false, review_pending: false },
          }),
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );
    mount(
      epicFixture({
        state_category: 'unstarted',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      })
    );
    const stage = await screen.findByTestId('epic-detail-stage');
    fireEvent.click(stage);
    await waitFor(() =>
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
        )
      ).toBe(true)
    );
    const patchCall = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
    )!;
    const [url, init] = patchCall;
    expect(url).toBe('/api/work-items/demo%2Fpc-e-test');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBeTruthy();
    const body = JSON.parse(init.body as string);
    expect(body.state_category).toBe('staged');
  });
});

describe('EpicDetailRegistration', () => {
  it('registers kind=epic in the RHP detail registry', async () => {
    function RegistryProbe() {
      const registry = useRhpDetailRegistry();
      const [kind, setKind] = useState<string | undefined>(undefined);
      useEffect(() => {
        // Poll the registry after registration.
        const reg = registry.resolve('epic');
        setKind(reg?.kind);
      }, [registry]);
      return <div data-testid="probe">{kind ?? 'none'}</div>;
    }

    render(
      <MemoryRouter>
        <RhpProvider>
          <RhpPinnedContentProvider>
            <EpicDetailRegistration />
            <RegistryProbe />
          </RhpPinnedContentProvider>
        </RhpProvider>
      </MemoryRouter>
    );

    await waitFor(() => {
      const probe = screen.getByTestId('probe');
      expect(probe.textContent).toBe('epic');
    });
  });
});
