// EpicDrawer Stage / Start workers actions (gm-vzy). Covers the four
// enablement corners of the capability × state gate:
//
//   agent_claimable | state    | Stage | Start
//   ----------------|----------|-------|-------
//   false           | any      | off   | off
//   true            | unstarted| on    | off
//   true            | staged   | off   | on
//   true            | started  | off   | off
//
// plus a positive test that clicking Stage fires PATCH with the
// X-GEMBA-Confirm nonce header via useUpdateWorkItem.

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { EpicDrawer } from '../EpicDrawer';
import { workItemsKeys } from '@/hooks/useWorkItems';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import type { StateCategory, WorkItem } from '@/types/core.gen';

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

function mount(epic: WorkItem) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // Preload detail + list so the drawer has the epic without fetching.
  client.setQueryData(workItemsKeys.detail(epic.id), epic);
  client.setQueryData(workItemsKeys.list(), [epic]);
  const ui: ReactNode = (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>
          <EpicDrawer openId={epic.id} onClose={() => {}} />
        </CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  return { ...render(ui), client };
}

describe('EpicDrawer — Stage / Start workers actions (gm-vzy)', () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders both buttons in the drawer header', async () => {
    mount(epicFixture());
    await waitFor(() => expect(screen.getByTestId('epic-drawer-stage')).toBeTruthy());
    expect(screen.getByTestId('epic-drawer-start')).toBeTruthy();
  });

  it('disables both when derived.agent_claimable is false', async () => {
    mount(
      epicFixture({
        state_category: 'unstarted',
        derived: { agent_claimable: false, human_action_required: false, review_pending: false },
      })
    );
    const stage = await screen.findByTestId('epic-drawer-stage');
    const start = screen.getByTestId('epic-drawer-start');
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
    const stage = await screen.findByTestId('epic-drawer-stage');
    expect(stage.hasAttribute('disabled')).toBe(false);
    const start = screen.getByTestId('epic-drawer-start');
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
    const stage = await screen.findByTestId('epic-drawer-stage');
    const start = screen.getByTestId('epic-drawer-start');
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
    const start = await screen.findByTestId('epic-drawer-start');
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
    const stage = await screen.findByTestId('epic-drawer-stage');
    fireEvent.click(stage);
    await Promise.resolve();
    // React Query may fire background GETs on mount despite the preload;
    // the invariant we care about is that NO PATCH was dispatched.
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
    const stage = await screen.findByTestId('epic-drawer-stage');
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
    // Slashes in the bd-prefixed id are percent-encoded by the api
    // wrapper (encodeURIComponent) — assert the encoded form.
    expect(url).toBe('/api/work-items/demo%2Fpc-e-test');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBeTruthy();
    const body = JSON.parse(init.body as string);
    expect(body.state_category).toBe('staged');
  });
});
