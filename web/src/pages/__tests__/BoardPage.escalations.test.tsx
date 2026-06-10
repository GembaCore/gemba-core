// BoardPage escalation banner (gm-e11.3). Verifies that the banner
// rolls up scope-relevant open EscalationRequests with a click-through
// to /escalations, and stays hidden when the count is zero.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { BoardPage } from '../BoardPage';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import { HotkeyRegistry, HotkeysContext } from '@/hotkeys';
import { RhpProvider } from '@/components/rhp/RhpContext';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
import type { WorkItem } from '@/types/core.gen';
import type { EscalationRequest } from '@/api/escalations';
import type { ApiError } from '@/api/client';

vi.mock('@/hooks/useEscalations', () => ({
  useEscalations: vi.fn(),
}));

import { useEscalations } from '@/hooks/useEscalations';
const mockUseEscalations = vi.mocked(useEscalations);

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

function makeEscalationsResult(
  data: EscalationRequest[]
): UseQueryResult<EscalationRequest[], ApiError> {
  return {
    data,
    error: null,
    isError: false,
    isPending: false,
    isLoading: false,
    isSuccess: true,
    isFetching: false,
    isRefetching: false,
    isStale: false,
    isPlaceholderData: false,
    isLoadingError: false,
    isRefetchError: false,
    status: 'success',
    fetchStatus: 'idle',
    dataUpdatedAt: 0,
    errorUpdatedAt: 0,
    failureCount: 0,
    failureReason: null,
    errorUpdateCount: 0,
    isPaused: false,
    refetch: vi.fn(),
  } as unknown as UseQueryResult<EscalationRequest[], ApiError>;
}

function escalation(id: string, work_item_id?: string): EscalationRequest {
  return {
    id,
    source: 'question',
    urgency: 'advisory',
    title: id,
    prompt: '',
    state: 'open',
    created_at: '',
    work_item_id,
  } as EscalationRequest;
}

function wrap(ui: ReactNode, initialEntry = '/board') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const registry = new HotkeyRegistry();
  return (
    <MemoryRouter initialEntries={[initialEntry]}>
      <RhpProvider>
        <RhpPinnedContentProvider>
          <QueryClientProvider client={client}>
            <CapabilitiesProvider initial={caps}>
              <HotkeysContext.Provider value={registry}>
                <Routes>
                  <Route path="/board" element={ui} />
                  <Route path="/board/*" element={ui} />
                </Routes>
              </HotkeysContext.Provider>
            </CapabilitiesProvider>
          </QueryClientProvider>
        </RhpPinnedContentProvider>
      </RhpProvider>
    </MemoryRouter>
  );
}

function epic(id: string, parent?: string): WorkItem {
  return {
    id,
    kind: 'epic',
    title: id,
    status: 'open',
    state_category: 'started',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    relationships: parent ? [{ kind: 'parent_child', from: parent, to: id }] : [],
  };
}

describe('BoardPage escalation banner (gm-e11.3)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
    mockUseEscalations.mockReset();
  });

  it('shows the count when 2 open escalations target items in scope=all', async () => {
    const data: WorkItem[] = [epic('e1'), epic('e2')];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult([escalation('a', 'e1'), escalation('b', 'e2')])
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
    const banner = screen.getByTestId('board-escalation-banner');
    expect(banner).toBeTruthy();
    expect(banner.textContent).toContain('2 open escalations');
    expect(banner.getAttribute('href')).toBe('/escalations');
    expect(banner.getAttribute('data-escalation-count')).toBe('2');
  });

  it('limits the count to the active scope lineage', async () => {
    // e1 has child c1; e2 has child c2. scope=e1 should hide e2's escalation.
    const data: WorkItem[] = [
      epic('e1'),
      epic('e2'),
      {
        id: 'c1',
        kind: 'task',
        title: 'c1',
        status: 'open',
        state_category: 'started',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-22T00:00:00Z',
        relationships: [{ kind: 'parent_child', from: 'e1', to: 'c1' }],
      },
      {
        id: 'c2',
        kind: 'task',
        title: 'c2',
        status: 'open',
        state_category: 'started',
        created_at: '2026-04-20T00:00:00Z',
        updated_at: '2026-04-22T00:00:00Z',
        relationships: [{ kind: 'parent_child', from: 'e2', to: 'c2' }],
      },
    ];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult([
        escalation('a', 'c1'),
        escalation('b', 'c2'),
        escalation('c', 'e2'),
      ])
    );
    render(wrap(<BoardPage />, '/board?scope=e1'));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
    const banner = screen.getByTestId('board-escalation-banner');
    // Only c1 falls under e1's lineage; the other two count against e2.
    expect(banner.textContent).toContain('1 open escalation');
    expect(banner.getAttribute('data-escalation-count')).toBe('1');
  });

  it('does not render the banner when no escalations target items in scope', async () => {
    const data: WorkItem[] = [epic('e1')];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    mockUseEscalations.mockReturnValue(makeEscalationsResult([]));
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
    expect(screen.queryByTestId('board-escalation-banner')).toBeNull();
  });

  it('uses singular phrasing for a count of 1', async () => {
    const data: WorkItem[] = [epic('e1')];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    mockUseEscalations.mockReturnValue(makeEscalationsResult([escalation('a', 'e1')]));
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
    const banner = screen.getByTestId('board-escalation-banner');
    expect(banner.textContent).toContain('1 open escalation');
    expect(banner.textContent).not.toContain('1 open escalations');
  });
});
