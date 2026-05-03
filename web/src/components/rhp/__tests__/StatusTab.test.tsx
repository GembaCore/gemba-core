import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useEffect } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import { RhpProvider, useRhp } from '../RhpContext';
import { RhpPinnedContentProvider, useRhpPinnedContent } from '../RhpPinnedContent';
import { StatusBody, StatusTab } from '../StatusTab';
import type { WorkItem } from '@/types/core.gen';

const caps: CapabilitiesResponse = {
  work_plane: {
    adaptor_name: 'bd',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'backlog', closed: 'completed' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  },
  orchestration_plane: {
    adaptor_id: 'gastown',
    adaptor_version: '0.1.0',
    orchestration_api_version: '0.1.0',
    transport: 'api',
    workspace_kinds: ['worktree'],
    group_modes: ['static'],
    cost_axes: ['tokens'],
    escalation_kinds: ['orchestrator_pause'],
    peek_modes: ['transcript'],
  },
};

const workItem: WorkItem = {
  id: 'gm-1',
  kind: 'task',
  title: 'Implement converter',
  status: 'open',
  state_category: 'completed',
  created_at: '2026-05-01T00:00:00Z',
  updated_at: '2026-05-01T00:00:00Z',
};

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function wrapper(): (props: { children: React.ReactNode }) => JSX.Element {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <CapabilitiesProvider initial={caps}>
            <RhpProvider>
              <RhpPinnedContentProvider>{children}</RhpPinnedContentProvider>
            </RhpProvider>
          </CapabilitiesProvider>
        </QueryClientProvider>
      </MemoryRouter>
    );
  };
}

function mockStatusFetch(fetchSpy: ReturnType<typeof vi.fn>) {
  fetchSpy.mockImplementation((input: RequestInfo | URL) => {
    const url = String(input);
    if (url === '/api/sessions') {
      return Promise.resolve(
        jsonResponse({
          sessions: [
            {
              id: 's-1',
              assignment_id: 'gm-1',
              agent_id: 'agent-1',
              status: 'working',
              started_at: '2026-05-01T12:00:00Z',
              provider_metadata: {
                agent_type: 'codex',
                bead_id: 'gm-1',
                usage: { total_tokens: 1234 },
              },
            },
          ],
          total: 1,
        })
      );
    }
    if (url === '/api/escalations') {
      return Promise.resolve(
        jsonResponse({
          escalations: [
            {
              id: 'esc-1',
              source: 'orchestrator_pause',
              urgency: 'blocking',
              title: 'Need approval',
              prompt: 'Approve?',
              state: 'open',
              created_at: '2026-05-01T12:05:00Z',
            },
          ],
          total: 1,
        })
      );
    }
    if (url.startsWith('/api/work-items')) {
      return Promise.resolve(jsonResponse({ items: [workItem], total: 1 }));
    }
    if (url === '/api/adaptors') {
      return Promise.resolve(
        jsonResponse({ adaptors: [{ name: 'bd', plane: 'work', healthy: true }] })
      );
    }
    if (url === '/api/beads/health') {
      return Promise.resolve(
        jsonResponse({
          source: { kind: 'beads-dir', label: 'gemba', detail: '/tmp/gemba' },
          current_db: 'gemba',
          remote_configured: false,
          remote_kind: 'Local worktree',
          remote_status_label: 'Local DB',
          adaptor: { name: 'bd', plane: 'work', healthy: true },
          actions: [
            {
              id: 'refresh',
              label: 'Refresh health',
              description: 'Re-run the Beads health probe.',
            },
          ],
        })
      );
    }
    if (url === '/api/agents') {
      return Promise.resolve(jsonResponse({ agents: [], total: 0 }));
    }
    return Promise.resolve(jsonResponse({}));
  });
}

describe('StatusTab', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    fetchSpy.mockReset();
  });

  it('registers the Status pinned tab and content', async () => {
    function Probe() {
      const { tabs } = useRhp();
      const { resolve } = useRhpPinnedContent();
      useEffect(() => {}, [tabs]);
      const hasContent = !!resolve('status');
      return (
        <div data-testid="probe">
          {tabs.map((t) => t.id).join(',')}|{hasContent ? 'content' : 'empty'}
        </div>
      );
    }

    render(
      <MemoryRouter>
        <RhpProvider>
          <RhpPinnedContentProvider>
            <StatusTab />
            <Probe />
          </RhpPinnedContentProvider>
        </RhpProvider>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByTestId('probe').textContent).toContain('status'));
    expect(screen.getByTestId('probe').textContent).toContain('content');
  });

  it('renders metrics, active sessions, escalations, and runtime status', async () => {
    mockStatusFetch(fetchSpy);

    render(<StatusBody />, { wrapper: wrapper() });

    await waitFor(() => expect(screen.getByTestId('rhp-status-metrics')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('1.2k')).toBeTruthy());
    expect(screen.getByText('codex')).toBeTruthy();
    expect(screen.getByText('gm-1')).toBeTruthy();
    expect(screen.getByText('Need approval')).toBeTruthy();
    expect(screen.getAllByText('bd').length).toBeGreaterThan(0);
    expect(screen.getAllByText('gastown').length).toBeGreaterThan(0);
    expect(screen.getByTestId('rhp-status-beads-health')).toBeTruthy();
    expect(screen.getByText('Current DB')).toBeTruthy();
    expect(screen.getByText('Adaptors healthy')).toBeTruthy();
  });
});
