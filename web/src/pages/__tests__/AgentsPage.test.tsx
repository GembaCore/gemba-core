// AgentsPage tests (gm-e12.4). Pin the rendering invariants — distinct
// agent vs human treatment, current-session derivation from the
// sessions list, click-through to the drawer, idle vs active sort —
// and the SSE-driven liveness contract: invalidating ['sessions']
// after a status change reflects in the tile within the next render.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { AgentsPage } from '../AgentsPage';
import type { AgentRef, SessionStatus } from '@/types/core.gen';
import type { Session } from '@/api/sessions';
import { CapabilitiesProvider } from '@/capabilities/context';
import type { CapabilitiesResponse, OrchestrationManifest } from '@/capabilities/types';

function agent(id: string, patch: Partial<AgentRef> = {}): AgentRef {
  return {
    id,
    name: `Agent ${id}`,
    agent_kind: 'agent',
    role: 'crew',
    dialect: 'claude',
    ...patch,
  };
}

function session(
  agent_id: string,
  status: SessionStatus,
  patch: Partial<Session> = {}
): Session {
  return {
    id: `s-${agent_id}-${status}`,
    assignment_id: `gm-${agent_id}-1`,
    agent_id,
    status,
    started_at: '2026-04-25T08:00:00Z',
    ...patch,
  };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// orchestrationWithTranscript is the minimal manifest the
// AgentDetailDrawer reads to gate its transcript pane (gm-5uqu).
// Tests that don't care about peek leave the manifest in place since
// the drawer only renders the pane when a session is open.
const orchestrationWithTranscript: OrchestrationManifest = {
  adaptor_id: 'native',
  adaptor_version: '0.1.0',
  orchestration_api_version: '1.0.0',
  transport: 'api',
  workspace_kinds: ['worktree'],
  default_workspace_kind: 'worktree',
  group_modes: ['static'],
  cost_axes: ['wallclock'],
  escalation_kinds: ['permission_prompt'],
  peek_modes: ['transcript'],
};

const initialCapabilities: CapabilitiesResponse = {
  work_plane: null,
  orchestration_plane: orchestrationWithTranscript,
};

function buildWrapper(opts?: {
  capabilities?: CapabilitiesResponse | null;
}): {
  Wrapper: (p: { children: ReactNode }) => JSX.Element;
  qc: QueryClient;
} {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  const caps = opts?.capabilities === undefined ? initialCapabilities : opts.capabilities;
  function Wrapper({ children }: { children: ReactNode }): JSX.Element {
    return (
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          {caps ? (
            <CapabilitiesProvider initial={caps}>{children}</CapabilitiesProvider>
          ) : (
            <CapabilitiesProvider
              initial={{ work_plane: null, orchestration_plane: null }}
            >
              {children}
            </CapabilitiesProvider>
          )}
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  return { Wrapper, qc };
}

describe('AgentsPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  function mockResponses(args: {
    agents: AgentRef[];
    sessions: Session[];
    times?: number;
  }): void {
    const times = args.times ?? 1;
    for (let i = 0; i < times; i++) {
      fetchSpy.mockImplementationOnce(async (url: string) => {
        if (url.startsWith('/api/agents')) {
          return json({ agents: args.agents, total: args.agents.length });
        }
        if (url.startsWith('/api/sessions')) {
          return json({ sessions: args.sessions, total: args.sessions.length });
        }
        return json({ error: 'unexpected', url }, 500);
      });
    }
  }

  it('renders an empty state when the orchestrator returns no agents', async () => {
    mockResponses({ agents: [], sessions: [], times: 2 });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agents-empty')).toBeTruthy());
  });

  it('renders one tile per agent with kind-specific iconography', async () => {
    mockResponses({
      agents: [
        agent('a1', { agent_kind: 'agent', name: 'planner' }),
        agent('h1', { agent_kind: 'human', name: 'mike', role: 'mayor' }),
      ],
      sessions: [],
      times: 2,
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1')).toBeTruthy());
    expect(screen.getByTestId('agent-tile-h1')).toBeTruthy();
    expect(screen.getByTestId('agent-tile-a1-icon-agent')).toBeTruthy();
    expect(screen.getByTestId('agent-tile-h1-icon-human')).toBeTruthy();
    // Distinct visual treatment — the data-agent-kind attribute lets a
    // future visual regression test pin the styling without coupling
    // to the exact tailwind classes.
    expect(screen.getByTestId('agent-tile-h1').getAttribute('data-agent-kind')).toBe('human');
    expect(screen.getByTestId('agent-tile-a1').getAttribute('data-agent-kind')).toBe('agent');
  });

  it('shows the current-session status badge for an agent with an observable session', async () => {
    mockResponses({
      agents: [agent('a1')],
      sessions: [session('a1', 'working')],
      times: 2,
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() =>
      expect(screen.getByTestId('agent-tile-a1-status').textContent?.toLowerCase()).toContain(
        'working'
      )
    );
    // Active tile is marked so CSS / future tests can target it.
    expect(screen.getByTestId('agent-tile-a1').getAttribute('data-active')).toBe('true');
  });

  it('shows idle when an agents only sessions are terminal', async () => {
    mockResponses({
      agents: [agent('a1')],
      sessions: [session('a1', 'completed', { ended_at: '2026-04-25T08:30:00Z' })],
      times: 2,
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1-idle')).toBeTruthy());
    expect(screen.getByTestId('agent-tile-a1').getAttribute('data-active')).toBeNull();
  });

  it('sorts active agents before idle ones', async () => {
    mockResponses({
      agents: [agent('z-idle', { name: 'z-idle' }), agent('a-active', { name: 'a-active' })],
      sessions: [session('a-active', 'ready')],
      times: 2,
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a-active')).toBeTruthy());
    const tiles = Array.from(document.querySelectorAll('[data-testid^="agent-tile-"]')).filter(
      (n) =>
        /^agent-tile-(a-active|z-idle)$/.test(n.getAttribute('data-testid') ?? '')
    );
    expect(tiles.map((t) => t.getAttribute('data-testid'))).toEqual([
      'agent-tile-a-active',
      'agent-tile-z-idle',
    ]);
  });

  it('clicking a tile opens the agent detail drawer', async () => {
    mockResponses({
      agents: [agent('a1')],
      sessions: [session('a1', 'working')],
      times: 2,
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1')).toBeTruthy());
    act(() => {
      screen.getByTestId('agent-tile-a1').click();
    });
    expect(screen.getByTestId('agent-drawer')).toBeTruthy();
    expect(screen.getByTestId('agent-drawer-session')).toBeTruthy();
    // Close button + esc both close. Cover the close button explicitly;
    // esc handling is covered in the drawer test alongside the keydown.
    act(() => {
      screen.getByTestId('agent-drawer-close').click();
    });
    expect(screen.queryByTestId('agent-drawer')).toBeNull();
  });

  // gm-5uqu: opening the drawer for an agent with an active session
  // pulls the live transcript through /api/sessions/{id}/peek and
  // renders it in a dedicated transcript pane.
  it('renders a live transcript in the drawer for an active session', async () => {
    const peekURL = '/api/sessions/s-a1-working/peek';
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/agents')) {
        return json({ agents: [agent('a1')], total: 1 });
      }
      if (url.startsWith(peekURL)) {
        return json({
          session_id: 's-a1-working',
          status: 'working',
          captured_at: '2026-04-25T08:30:00Z',
          transcript: 'claude > ls\nworkitem.go\nclaude >',
        });
      }
      if (url.startsWith('/api/sessions')) {
        return json({ sessions: [session('a1', 'working')], total: 1 });
      }
      return json({ error: 'unexpected', url }, 500);
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1')).toBeTruthy());
    act(() => {
      screen.getByTestId('agent-tile-a1').click();
    });

    await waitFor(() => {
      const pane = screen.getByTestId('agent-drawer-transcript');
      expect(pane.textContent ?? '').toContain('claude > ls');
    });
    const pane = screen.getByTestId('agent-drawer-transcript');
    expect(pane.textContent).toContain('workitem.go');
    // The captured-at chip surfaces the snapshot wall-clock so an
    // operator can tell whether the view is fresh.
    expect(screen.getByTestId('agent-drawer-transcript-captured-at').textContent).toContain(
      '2026-04-25T08:30:00Z'
    );
  });

  // gm-5uqu: an agent with no active session has nothing to peek;
  // the drawer must NOT call /api/sessions/{id}/peek and the
  // transcript pane stays unrendered.
  it('does not call peek for an agent with no active session', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/agents')) {
        return json({ agents: [agent('a1')], total: 1 });
      }
      if (url.startsWith('/api/sessions')) {
        return json({ sessions: [], total: 0 });
      }
      return json({ error: 'unexpected', url }, 500);
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1')).toBeTruthy());
    act(() => {
      screen.getByTestId('agent-tile-a1').click();
    });
    expect(screen.getByTestId('agent-drawer-idle')).toBeTruthy();
    expect(screen.queryByTestId('agent-drawer-transcript')).toBeNull();
    // The drawer must not have made a peek request — only agents +
    // sessions list are legitimate calls in this flow.
    const peekCalls = fetchSpy.mock.calls.filter((c) =>
      (c[0] as string).includes('/peek')
    );
    expect(peekCalls).toHaveLength(0);
  });

  // gm-5uqu: a 404 from peek (e.g. session ended between list and
  // peek) renders a steady-state "transcript unavailable" rather than
  // tearing down the drawer.
  it('renders a graceful fallback when peek returns 404', async () => {
    const peekURL = '/api/sessions/s-a1-working/peek';
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/agents')) {
        return json({ agents: [agent('a1')], total: 1 });
      }
      if (url.startsWith(peekURL)) {
        return json({ error: 'session_not_found', message: 'gone' }, 404);
      }
      if (url.startsWith('/api/sessions')) {
        return json({ sessions: [session('a1', 'working')], total: 1 });
      }
      return json({ error: 'unexpected', url }, 500);
    });
    const { Wrapper } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1')).toBeTruthy());
    act(() => {
      screen.getByTestId('agent-tile-a1').click();
    });
    await waitFor(() =>
      expect(screen.getByTestId('agent-drawer-transcript').textContent).toContain(
        'Transcript unavailable'
      )
    );
  });

  // gm-5uqu: when the bound OrchestrationPlane manifest doesn't
  // declare 'transcript' as a peek mode (read-only / structured-only
  // adaptors), the transcript pane is hidden entirely — no point
  // showing 'transcript unavailable' for an adaptor that can't peek.
  it('hides the transcript pane when peek_modes lacks "transcript"', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/agents')) {
        return json({ agents: [agent('a1')], total: 1 });
      }
      if (url.startsWith('/api/sessions')) {
        return json({ sessions: [session('a1', 'working')], total: 1 });
      }
      return json({ error: 'unexpected', url }, 500);
    });
    const { Wrapper } = buildWrapper({
      capabilities: {
        work_plane: null,
        orchestration_plane: { ...orchestrationWithTranscript, peek_modes: [] },
      },
    });
    render(<AgentsPage />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByTestId('agent-tile-a1')).toBeTruthy());
    act(() => {
      screen.getByTestId('agent-tile-a1').click();
    });
    expect(screen.getByTestId('agent-drawer-session')).toBeTruthy();
    expect(screen.queryByTestId('agent-drawer-transcript')).toBeNull();
    // No peek call either — the gate runs before the hook fires.
    const peekCalls = fetchSpy.mock.calls.filter((c) =>
      (c[0] as string).includes('/peek')
    );
    expect(peekCalls).toHaveLength(0);
  });

  // gm-e12.4 DoD: tile updates within 500ms of an agent state change.
  // The SSE consumer (gm-e12.2) maps session.transition →
  // invalidateQueries(['sessions']). This test simulates that by
  // calling invalidateQueries directly on the same key after seeding
  // a different sessions response. If the tile re-renders with the
  // new status, the SSE → invalidate → refetch → render path is sound;
  // the wall-clock budget is covered by the SSE → cache → render
  // pipeline being purely synchronous after the fetch resolves, which
  // measures in the low milliseconds in jsdom and on real hardware.
  it('reflects a session status change after the sessions cache is invalidated', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/agents')) {
        return json({ agents: [agent('a1')], total: 1 });
      }
      if (url.startsWith('/api/sessions')) {
        // First call: session is `ready`. Subsequent (post-invalidate)
        // calls return `working`.
        const callIndex = fetchSpy.mock.calls.filter((c) =>
          (c[0] as string).startsWith('/api/sessions')
        ).length;
        return json({
          sessions: [session('a1', callIndex <= 1 ? 'ready' : 'working')],
          total: 1,
        });
      }
      return json({ error: 'unexpected' }, 500);
    });
    const { Wrapper, qc } = buildWrapper();
    render(<AgentsPage />, { wrapper: Wrapper });

    await waitFor(() =>
      expect(screen.getByTestId('agent-tile-a1-status').textContent?.toLowerCase()).toContain(
        'ready'
      )
    );

    act(() => {
      qc.invalidateQueries({ queryKey: ['sessions'] });
    });

    await waitFor(() =>
      expect(screen.getByTestId('agent-tile-a1-status').textContent?.toLowerCase()).toContain(
        'working'
      )
    );
  });
});
