// AgentDetailPage tests (gm-e12.15). Cover the route container's
// fetch / select / render path independent of the per-kind panels
// (which have their own tests). The page is wired to the planner-coach
// endpoint; we stub fetch and verify routing + selection + each kind
// renders end-to-end.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { ReactNode } from 'react';
import { AgentDetailPage, findContext } from '../AgentDetailPage';
import type { PlannerOperationalContext, PlannerCoachResponse } from '@/api/planner';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function wrap(initialPath: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[initialPath]}>
          <Routes>
            <Route path="/agents/:id" element={children} />
            <Route path="/sessions" element={<div data-testid="stub-sessions" />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  };
}

function ctxFor(
  sessionId: string,
  agentId: string,
  kind: string
): PlannerOperationalContext {
  return {
    session: {
      id: sessionId,
      assignment_id: `asg-${sessionId}`,
      agent_id: agentId,
      status: 'working',
      started_at: '2026-04-26T13:00:00Z',
    },
    agent: { id: agentId, name: agentId, agent_kind: 'agent', role: 'crew' },
    workspace: {
      id: `ws-${sessionId}`,
      kind,
      repository: 'gemba',
      branch: 'main',
      worktree_path: `/tmp/${sessionId}`,
      isolation: { fs_scoped: true },
    },
  };
}

function coachResponse(sessions: PlannerOperationalContext[]): PlannerCoachResponse {
  return {
    sessions,
    ready_beads: [],
    conflicts: [],
    workspace: [],
    semantic: [],
    affinity: [],
    batches: [],
  };
}

describe('AgentDetailPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('resolves :id against session.id and renders the matching kind panel', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        jsonResponse(coachResponse([ctxFor('sess-aaa', 'alice', 'k8s_pod')]))
      )
    );
    const Wrapper = wrap('/agents/sess-aaa');
    render(
      <Wrapper>
        <AgentDetailPage />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('agent-detail-page'));
    expect(screen.getByTestId('agent-detail-panel-k8s_pod')).toBeTruthy();
  });

  it('falls back to agent.id when session.id does not match', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        jsonResponse(coachResponse([ctxFor('sess-xyz', 'bob', 'worktree')]))
      )
    );
    const Wrapper = wrap('/agents/bob');
    render(
      <Wrapper>
        <AgentDetailPage />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('agent-detail-page'));
    expect(screen.getByTestId('agent-detail-panel-worktree')).toBeTruthy();
  });

  it('renders the not-found explainer when the id matches nothing', async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        jsonResponse(coachResponse([ctxFor('sess-1', 'alice', 'worktree')]))
      )
    );
    const Wrapper = wrap('/agents/ghost');
    render(
      <Wrapper>
        <AgentDetailPage />
      </Wrapper>
    );
    await waitFor(() => screen.getByTestId('agent-detail-not-found'));
  });

  it('shows the loading state while the coach query is in flight', () => {
    // Never-resolving fetch keeps the query in-flight.
    fetchSpy.mockImplementation(() => new Promise(() => {}));
    const Wrapper = wrap('/agents/sess-1');
    render(
      <Wrapper>
        <AgentDetailPage />
      </Wrapper>
    );
    expect(screen.getByTestId('agent-detail-loading')).toBeTruthy();
  });

  it('exercises every canonical Workspace.kind end-to-end', async () => {
    const kinds = ['worktree', 'container', 'k8s_pod', 'vm', 'exec', 'subprocess'] as const;
    for (const k of kinds) {
      fetchSpy.mockImplementation(() =>
        Promise.resolve(jsonResponse(coachResponse([ctxFor(`sess-${k}`, 'alice', k)])))
      );
      const Wrapper = wrap(`/agents/sess-${k}`);
      const { unmount } = render(
        <Wrapper>
          <AgentDetailPage />
        </Wrapper>
      );
      await waitFor(() => screen.getByTestId(`agent-detail-panel-${k}`));
      unmount();
      fetchSpy.mockReset();
    }
  });
});

describe('findContext', () => {
  const sessions: PlannerOperationalContext[] = [
    ctxFor('sess-1', 'alice', 'worktree'),
    ctxFor('sess-2', 'bob', 'k8s_pod'),
  ];

  it('matches session.id first', () => {
    expect(findContext(sessions, 'sess-2')?.agent?.id).toBe('bob');
  });

  it('matches agent.id when session.id misses', () => {
    expect(findContext(sessions, 'alice')?.session?.id).toBe('sess-1');
  });

  it('matches assignment_id last', () => {
    expect(findContext(sessions, 'asg-sess-2')?.agent?.id).toBe('bob');
  });

  it('returns undefined when nothing matches', () => {
    expect(findContext(sessions, 'nope')).toBeUndefined();
  });

  it('returns undefined for an empty id', () => {
    expect(findContext(sessions, '')).toBeUndefined();
  });
});
