// Sidebar tests (gm-root.17.12, gm-e11.8.2).
//
// Coverage:
//   - active project: workspace-scoped items render as interactive links
//   - cold-start (no projects): workspace-scoped items render as muted
//     spans with aria-disabled="true" and a tooltip; clicking is a no-op
//   - Settings + Capability Browser remain interactive on cold-start
//   - initial-fetch loading state does not flash the muted UI
//   - escalation badge: renders correct count, hides on 0/loading/error,
//     caps at "99+", absent from cold-start muted variant

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider, type UseQueryResult } from '@tanstack/react-query';
import { Sidebar } from '../Sidebar';
import { ProjectPickerProvider } from '../projectpicker/ProjectPickerContext';
import type { EscalationRequest } from '@/api/escalations';
import type { ApiError } from '@/api/client';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';

// Mock useEscalations so badge tests don't depend on network.
vi.mock('@/hooks/useEscalations', () => ({
  useEscalations: vi.fn(),
}));

import { useEscalations } from '@/hooks/useEscalations';
const mockUseEscalations = vi.mocked(useEscalations);

function makeEscalationsResult(
  overrides: Partial<UseQueryResult<EscalationRequest[], ApiError>>
): UseQueryResult<EscalationRequest[], ApiError> {
  return {
    data: undefined,
    error: null,
    isError: false,
    isPending: true,
    isLoading: true,
    isSuccess: false,
    isFetching: false,
    isRefetching: false,
    isStale: false,
    isPlaceholderData: false,
    isLoadingError: false,
    isRefetchError: false,
    status: 'pending',
    fetchStatus: 'fetching',
    dataUpdatedAt: 0,
    errorUpdatedAt: 0,
    failureCount: 0,
    failureReason: null,
    errorUpdateCount: 0,
    isPaused: false,
    refetch: vi.fn(),
    ...overrides,
  } as UseQueryResult<EscalationRequest[], ApiError>;
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const caps: CapabilitiesResponse = {
  work_plane: {
    adaptor_name: 'fake',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'unstarted', in_progress: 'started', closed: 'completed' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  },
  orchestration_plane: null,
};

function renderSidebar(initialEntry = '/new', initialCaps: CapabilitiesResponse = caps) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={initialCaps}>
          <ProjectPickerProvider>
            <Sidebar />
          </ProjectPickerProvider>
        </CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

// Five-item left rail (gm-e12.19, second amendment + 2026-04-29
// Insights removal). Capability Browser and Drift folded into
// Settings / Escalations panes; Insights removed pending the
// Prometheus-proxy chain (gm-sf51 / gm-flij). Only Settings remains
// workspace-agnostic (so a fresh install can reach global config
// from a cold start).
const WORKSPACE_SCOPED = [
  'Plan',
  'Graph',
  'Refine',
  'Review',
  'Triage',
  'Sessions',
];

const ALWAYS_OPERATIONAL = ['Settings'];

const beadsOnlyCaps: CapabilitiesResponse = {
  ...caps,
  runtime_mode: 'beads_only',
  beads_only: true,
  orchestration_plane: null,
};

describe('Sidebar', () => {
  let fetchSpy: ReturnType<typeof vi.fn>;
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    fetchSpy = vi.fn();
    globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch;
    // Default: query loading (no badge rendered).
    mockUseEscalations.mockReturnValue(makeEscalationsResult({}));
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('renders all workspace-scoped items as links when a project is active', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [{ name: 'demo', path: '/tmp/demo', active: true }],
        total: 1,
      })
    );
    renderSidebar();

    for (const label of WORKSPACE_SCOPED) {
      const link = await screen.findByRole('link', { name: label });
      expect(link).toBeTruthy();
      expect(link.getAttribute('href')).toBeTruthy();
    }
    expect(screen.queryByRole('link', { name: 'Recent' })).toBeNull();
  });

  it('marks parent panes active for their rolled-up routes', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [{ name: 'demo', path: '/tmp/demo', active: true }],
        total: 1,
      })
    );
    renderSidebar('/graph');

    const graph = await screen.findByRole('link', { name: 'Graph' });
    expect(graph.getAttribute('data-active')).toBe('true');
    expect(graph.getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('link', { name: 'Plan' }).getAttribute('data-active')).toBeNull();
    expect(screen.getByRole('link', { name: 'Refine' }).getAttribute('data-active')).toBeNull();
  });

  it('marks Sessions active for agent detail routes', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        projects: [{ name: 'demo', path: '/tmp/demo', active: true }],
        total: 1,
      })
    );
    renderSidebar('/agents/session-1');

    const sessions = await screen.findByRole('link', { name: 'Sessions' });
    expect(sessions.getAttribute('data-active')).toBe('true');
    expect(sessions.getAttribute('aria-current')).toBe('page');
  });

  it('cold-start: workspace-scoped items render as aria-disabled spans', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderSidebar();

    // Wait for the picker's initial fetch to land — Settings is always
    // a link, so its presence signals the sidebar has rendered at all.
    await screen.findByRole('link', { name: 'Settings' });

    for (const label of WORKSPACE_SCOPED) {
      // The item still shows the label (greyed, not removed).
      const labelEl = screen.getByText(label, { selector: 'span' });
      expect(labelEl).toBeTruthy();
      // Find the wrapping aria-disabled element.
      const wrap = labelEl.closest('[aria-disabled="true"]');
      expect(wrap).toBeTruthy();
      expect(wrap?.getAttribute('title')).toMatch(/Available after/);
      // Crucially NOT a real link.
      expect(wrap?.tagName.toLowerCase()).toBe('span');
    }
  });

  it('cold-start: the current workspace pane is still visibly active', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderSidebar('/graph');

    await screen.findByRole('link', { name: 'Settings' });
    const graph = screen.getByTestId('sidebar-item-graph');
    expect(graph.tagName.toLowerCase()).toBe('span');
    expect(graph.getAttribute('aria-disabled')).toBe('true');
    expect(graph.getAttribute('data-active')).toBe('true');
    expect(graph.getAttribute('aria-current')).toBe('page');
  });

  it('beads-only mode exposes Graph as a first-order navigation item', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderSidebar('/board', beadsOnlyCaps);

    const graph = await screen.findByRole('link', { name: 'Graph' });
    expect(graph.getAttribute('href')).toBe('/graph');
    expect(graph.getAttribute('data-disabled')).toBeNull();
    expect(screen.queryByRole('link', { name: 'Review' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Triage' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Sessions' })).toBeNull();
  });

  it('cold-start: Settings stays interactive', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    renderSidebar();

    for (const label of ALWAYS_OPERATIONAL) {
      const link = await screen.findByRole('link', { name: label });
      expect(link).toBeTruthy();
      expect(link.getAttribute('href')).toBeTruthy();
    }
  });

  it('does not flash muted UI during the initial fetch', async () => {
    // Hold the fetch open so isLoading remains true throughout.
    let resolve: (v: Response) => void = () => {};
    fetchSpy.mockReturnValue(
      new Promise<Response>((r) => {
        resolve = r;
      })
    );
    renderSidebar();

    // While loading, no items should be aria-disabled (we suppress
    // muting until the first fetch lands so the UI doesn't flicker).
    expect(document.querySelector('[aria-disabled="true"]')).toBeNull();

    // Resolve to drain the fixture.
    resolve(jsonResponse({ projects: [], total: 0 }));
  });

  // ── Escalation badge (gm-e11.8.2) ──────────────────────────────────

  it('escalation badge: shows count when query resolves with items', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ projects: [{ name: 'demo', path: '/tmp/demo', active: true }], total: 1 })
    );
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult({
        isSuccess: true,
        isPending: false,
        isLoading: false,
        status: 'success',
        data: [
          { id: 'e1', source: 'question', urgency: 'advisory', title: 'Q1', prompt: '', state: 'open', created_at: '' },
          { id: 'e2', source: 'question', urgency: 'advisory', title: 'Q2', prompt: '', state: 'open', created_at: '' },
          { id: 'e3', source: 'question', urgency: 'advisory', title: 'Q3', prompt: '', state: 'open', created_at: '' },
        ] as EscalationRequest[],
      })
    );
    renderSidebar();
    await screen.findByRole('link', { name: /Triage/ });
    const badge = screen.getByTestId('sidebar-escalations-badge');
    expect(badge).toBeTruthy();
    expect(badge.textContent).toBe('3');
  });

  it('escalation badge: hidden when count is 0', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ projects: [{ name: 'demo', path: '/tmp/demo', active: true }], total: 1 })
    );
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult({
        isSuccess: true,
        isPending: false,
        isLoading: false,
        status: 'success',
        data: [] as EscalationRequest[],
      })
    );
    renderSidebar();
    await screen.findByRole('link', { name: 'Triage' });
    expect(screen.queryByTestId('sidebar-escalations-badge')).toBeNull();
  });

  it('escalation badge: caps at 99+ for counts >= 100', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ projects: [{ name: 'demo', path: '/tmp/demo', active: true }], total: 1 })
    );
    const manyItems = Array.from({ length: 100 }, (_, i) => ({
      id: `e${i}`,
      source: 'question' as const,
      urgency: 'advisory' as const,
      title: `Q${i}`,
      prompt: '',
      state: 'open' as const,
      created_at: '',
    }));
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult({
        isSuccess: true,
        isPending: false,
        isLoading: false,
        status: 'success',
        data: manyItems as EscalationRequest[],
      })
    );
    renderSidebar();
    await screen.findByRole('link', { name: /Triage/ });
    const badge = screen.getByTestId('sidebar-escalations-badge');
    expect(badge.textContent).toBe('99+');
  });

  it('escalation badge: hidden while loading', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ projects: [{ name: 'demo', path: '/tmp/demo', active: true }], total: 1 })
    );
    // Default mock (from beforeEach) has isLoading: true, isSuccess: false.
    renderSidebar();
    await screen.findByRole('link', { name: 'Settings' });
    expect(screen.queryByTestId('sidebar-escalations-badge')).toBeNull();
  });

  it('escalation badge: hidden on error', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ projects: [{ name: 'demo', path: '/tmp/demo', active: true }], total: 1 })
    );
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult({
        isSuccess: false,
        isPending: false,
        isLoading: false,
        isError: true,
        status: 'error',
        error: { message: 'network error' } as ApiError,
      })
    );
    renderSidebar();
    await screen.findByRole('link', { name: 'Settings' });
    expect(screen.queryByTestId('sidebar-escalations-badge')).toBeNull();
  });

  it('escalation badge: absent from cold-start muted variant', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ projects: [], total: 0 }));
    mockUseEscalations.mockReturnValue(
      makeEscalationsResult({
        isSuccess: true,
        isPending: false,
        isLoading: false,
        status: 'success',
        data: [
          { id: 'e1', source: 'question', urgency: 'advisory', title: 'Q1', prompt: '', state: 'open', created_at: '' },
        ] as EscalationRequest[],
      })
    );
    renderSidebar();
    // Wait for the project picker to resolve into cold-start mode.
    await waitFor(() => expect(screen.getByTestId('sidebar-item-escalations').getAttribute('aria-disabled')).toBe('true'));
    const wrap = screen.getByTestId('sidebar-item-escalations');
    // No badge inside the muted span.
    expect(wrap?.querySelector('[data-testid="sidebar-escalations-badge"]')).toBeNull();
    // No badge anywhere in the document.
    expect(screen.queryByTestId('sidebar-escalations-badge')).toBeNull();
  });
});
