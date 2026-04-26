import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { AgentContextCard } from '../AgentContextCard';
import type { PlannerOperationalContext } from '@/api/planner';

function ctx(overrides: Partial<PlannerOperationalContext> = {}): PlannerOperationalContext {
  return {
    session: {
      id: 'sess-1',
      assignment_id: 'asg-1',
      agent_id: 'alice',
      status: 'working',
      started_at: '2026-04-26T13:00:00Z',
    },
    agent: {
      id: 'alice',
      name: 'alice',
      agent_kind: 'agent',
      role: 'crew',
    },
    workspace: {
      id: 'ws-1',
      kind: 'worktree',
      repository: 'gemba',
      branch: 'main',
      worktree_path: '/var/gemba/worktrees/sess-1',
      isolation: { fs_scoped: true },
    },
    profile: {
      session_id: 'sess-1',
      concepts: { auth: 0.9, 'spa-routing': 0.4, 'e2e-fixture': 0.2 },
      context_pct: 0.42,
    },
    health: {
      context_pressure: 0.42,
      concept_drift: 0.12,
      time_on_task_ns: 90 * 60 * 1e9,
    },
    ...overrides,
  };
}

describe('AgentContextCard', () => {
  it('renders agent identity, status, and workspace fields', () => {
    render(<AgentContextCard ctx={ctx()} />);
    // 'alice' appears in both the agent.id badge and agent.name heading.
    expect(screen.getAllByText('alice').length).toBeGreaterThan(0);
    expect(screen.getByText('working')).toBeTruthy();
    expect(screen.getByText('gemba/main')).toBeTruthy();
    // Worktree path is left-truncated; the trailing segment must
    // remain visible.
    const card = screen.getByTestId('agent-context-sess-1');
    expect(within(card).getByText(/sess-1/)).toBeTruthy();
  });

  it('renders top concepts sorted by weight desc', () => {
    render(<AgentContextCard ctx={ctx()} />);
    const concepts = screen.getByTestId('agent-context-sess-1-concepts');
    // First chip should be the highest-weighted (auth 0.9).
    expect(concepts.textContent).toMatch(/^auth/);
  });

  it('renders pressure / drift / time-on-task in the health row', () => {
    render(<AgentContextCard ctx={ctx()} />);
    const health = screen.getByTestId('agent-context-sess-1-health');
    expect(health.textContent).toContain('0.42'); // pressure
    expect(health.textContent).toContain('0.12'); // drift
    expect(health.textContent).toContain('90m');  // time-on-task
  });

  it('emits data-in-conflict when inConflict prop is true', () => {
    render(<AgentContextCard ctx={ctx()} inConflict />);
    expect(screen.getByTestId('agent-context-sess-1').getAttribute('data-in-conflict')).toBe('true');
  });

  it('omits data-in-conflict when inConflict is false', () => {
    render(<AgentContextCard ctx={ctx()} />);
    expect(screen.getByTestId('agent-context-sess-1').getAttribute('data-in-conflict')).toBeNull();
  });

  it('renders the affinity badge when affinityForHovered is set', () => {
    render(
      <AgentContextCard
        ctx={ctx()}
        affinityForHovered={{ beadId: 'gm-42', combined: 0.73 }}
      />
    );
    const badge = screen.getByTestId('agent-context-sess-1-affinity');
    expect(badge.textContent).toContain('gm-42');
    expect(badge.textContent).toContain('0.73');
  });

  it('omits the affinity badge when affinityForHovered is null', () => {
    render(<AgentContextCard ctx={ctx()} affinityForHovered={null} />);
    expect(screen.queryByTestId('agent-context-sess-1-affinity')).toBeNull();
  });

  it('hides the workspace block when workspace is null', () => {
    render(<AgentContextCard ctx={ctx({ workspace: null })} />);
    expect(screen.queryByText('gemba/main')).toBeNull();
  });

  it('hides the health row when health is null', () => {
    render(<AgentContextCard ctx={ctx({ health: null })} />);
    expect(screen.queryByTestId('agent-context-sess-1-health')).toBeNull();
  });

  it('hides concepts when the profile has none', () => {
    render(<AgentContextCard ctx={ctx({ profile: null })} />);
    expect(screen.queryByTestId('agent-context-sess-1-concepts')).toBeNull();
  });

  it('renders isolation flags when set', () => {
    const c = ctx({
      workspace: {
        id: 'ws-1',
        kind: 'container',
        repository: 'gemba',
        branch: 'main',
        isolation: { fs_scoped: true, net_isolated: true, cpu_limited: true },
      },
    });
    render(<AgentContextCard ctx={c} />);
    expect(screen.getByText(/fs\+net\+cpu/)).toBeTruthy();
  });

  it('respects testidPrefix when provided', () => {
    render(<AgentContextCard ctx={ctx()} testidPrefix="custom-card" />);
    expect(screen.getByTestId('custom-card')).toBeTruthy();
    // Default sess-id testid is shadowed by the override.
    expect(screen.queryByTestId('agent-context-sess-1')).toBeNull();
  });

  it('survives a session with no agent + no workspace + no profile', () => {
    render(
      <AgentContextCard
        ctx={{
          session: {
            id: 'sess-naked',
            assignment_id: '',
            agent_id: '',
            status: 'initializing',
            started_at: '2026-04-26T13:00:00Z',
          },
        }}
      />
    );
    expect(screen.getByTestId('agent-context-sess-naked')).toBeTruthy();
  });
});
