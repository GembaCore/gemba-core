// gm-root.26 item 1 — onboarder CTA capability gate.
//
// When the /api/v1/onboarder/probe endpoint reports `available: false`
// the EmptyState renders a disabled-style note + a docs link instead
// of the active "Plan with the Onboarder →" CTA. This protects
// first-time users from clicking through to a 503 they can't
// interpret.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { BoardPage } from '../BoardPage';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import { HotkeyRegistry, HotkeysContext } from '@/hotkeys';
import { RhpProvider } from '@/components/rhp/RhpContext';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
import { WorkItemDetailRegistration } from '@/components/rhp/details/WorkItemDetailRegistration';
import { RhpShell } from '@/components/rhp/RhpShell';
import type { EscalationRequest } from '@/api/escalations';
import type { ApiError } from '@/api/client';

vi.mock('@/hooks/useEscalations', () => ({
  useEscalations: vi.fn(),
}));
import { useEscalations } from '@/hooks/useEscalations';
const mockUseEscalations = vi.mocked(useEscalations);

function emptyEscalationsResult(): UseQueryResult<EscalationRequest[], ApiError> {
  return {
    data: [],
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

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const registry = new HotkeyRegistry();
  return (
    <MemoryRouter initialEntries={['/board']}>
      <RhpProvider>
        <RhpPinnedContentProvider>
          <QueryClientProvider client={client}>
            <CapabilitiesProvider initial={caps}>
              <HotkeysContext.Provider value={registry}>
                <Routes>
                  <Route path="/board" element={ui} />
                </Routes>
                <WorkItemDetailRegistration />
                <RhpShell />
              </HotkeysContext.Provider>
            </CapabilitiesProvider>
          </QueryClientProvider>
        </RhpPinnedContentProvider>
      </RhpProvider>
    </MemoryRouter>
  );
}

// fetchSpy serves three endpoints used by EmptyState:
//   /api/work-items      → empty list
//   /api/v1/onboarder/probe → mocked per test
//   anything else        → 200 / empty
function makeFetchSpy(probeBody: { available: boolean; reason?: string }) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : (input as Request).url ?? String(input);
    if (url.includes('/api/work-items')) {
      return new Response(JSON.stringify({ items: [], total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url.includes('/api/v1/onboarder/probe')) {
      return new Response(JSON.stringify(probeBody), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    return new Response('{}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
}

describe('BoardPage EmptyState — onboarder CTA gate (gm-root.26 item 1)', () => {
  beforeEach(() => {
    mockUseEscalations.mockReturnValue(emptyEscalationsResult());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    mockUseEscalations.mockReset();
  });

  it('renders the active CTA when probe.available=true', async () => {
    vi.stubGlobal('fetch', makeFetchSpy({ available: true }));
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
    await waitFor(() => expect(screen.getByTestId('board-empty-plan')).toBeTruthy());
    expect(screen.queryByTestId('board-empty-plan-disabled')).toBeNull();
  });

  it('renders the disabled note + docs link when probe.available=false', async () => {
    vi.stubGlobal(
      'fetch',
      makeFetchSpy({ available: false, reason: 'No [llm] table configured' })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
    await waitFor(() =>
      expect(screen.getByTestId('board-empty-plan-disabled')).toBeTruthy()
    );
    // No active CTA — the operator can't click through to a 503.
    expect(screen.queryByTestId('board-empty-plan')).toBeNull();
    // Reason is exposed via the title/tooltip for assistive tech.
    const note = screen.getByTestId('board-empty-plan-disabled');
    expect(note.getAttribute('title')).toBe('No [llm] table configured');
    expect(note.getAttribute('aria-disabled')).toBe('true');
    // Docs link is present and points at the configuration guide.
    const docs = screen.getByTestId('board-empty-plan-docs');
    expect(docs.getAttribute('href')).toContain('configuration');
  });
});
