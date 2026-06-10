// Per-Workspace.kind panel tests (gm-e12.15).
//
// One test per kind: the panel renders, declares its data-kind attr,
// and surfaces the affordance set ui-spec §5.18 prescribes for that
// kind (Attach for worktree, kubectl-exec + logs for k8s_pod, etc.).
//
// Together with the AgentDetail switch test, these prove the Definition
// of Done from gm-e12.15: each kind renders the correct affordance set
// against a sample workspace.

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { PlannerOperationalContext } from '@/api/planner';
import { AgentDetail } from '@/views/AgentDetail';

function ctxFor(kind: string, extras: Record<string, unknown> = {}): PlannerOperationalContext {
  return {
    session: {
      id: `sess-${kind}`,
      assignment_id: `asg-${kind}`,
      agent_id: 'alice',
      status: 'working',
      started_at: '2026-04-26T13:00:00Z',
    },
    agent: { id: 'alice', name: 'alice', agent_kind: 'agent', role: 'crew' },
    workspace: {
      id: `ws-${kind}`,
      kind,
      repository: 'gemba',
      branch: 'main',
      worktree_path: `/var/gemba/${kind}/sess-1`,
      isolation: { fs_scoped: true, ...((extras.isolation ?? {}) as object) },
      ...extras,
    },
  };
}

describe('AgentDetail — per Workspace.kind affordance set', () => {
  it('worktree → Attach button + repo/branch + worktree path', () => {
    render(<AgentDetail ctx={ctxFor('worktree')} />);
    const panel = screen.getByTestId('agent-detail-panel-worktree');
    expect(panel.getAttribute('data-kind')).toBe('worktree');
    expect(screen.getByTestId('agent-detail-attach')).toBeTruthy();
    expect(panel.textContent).toContain('gemba/main');
    expect(panel.textContent).toContain('/var/gemba/worktree/sess-1');
  });

  it('container → docker-exec button + streaming-logs slot', () => {
    render(<AgentDetail ctx={ctxFor('container')} />);
    const panel = screen.getByTestId('agent-detail-panel-container');
    expect(panel.getAttribute('data-kind')).toBe('container');
    expect(screen.getByTestId('agent-detail-docker-exec')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-logs')).toBeTruthy();
    // Worktree-only affordance must NOT leak into a container panel.
    expect(screen.queryByTestId('agent-detail-attach')).toBeNull();
  });

  it('k8s_pod → Peek + kubectl-exec + pod-status badge + logs', () => {
    render(<AgentDetail ctx={ctxFor('k8s_pod')} />);
    const panel = screen.getByTestId('agent-detail-panel-k8s_pod');
    expect(panel.getAttribute('data-kind')).toBe('k8s_pod');
    expect(screen.getByTestId('agent-detail-peek')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-kubectl-exec')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-pod-status')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-logs')).toBeTruthy();
    expect(screen.queryByTestId('agent-detail-docker-exec')).toBeNull();
  });

  it('vm → SSH + Console buttons; snapshot hint when isolation flag set', () => {
    render(
      <AgentDetail
        ctx={ctxFor('vm', { isolation: { fs_scoped: true, snapshot_restore: true } })}
      />
    );
    const panel = screen.getByTestId('agent-detail-panel-vm');
    expect(panel.getAttribute('data-kind')).toBe('vm');
    expect(screen.getByTestId('agent-detail-ssh')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-console')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-snapshot-hint')).toBeTruthy();
  });

  it('vm without snapshot capability omits the snapshot hint', () => {
    render(<AgentDetail ctx={ctxFor('vm')} />);
    expect(screen.queryByTestId('agent-detail-snapshot-hint')).toBeNull();
  });

  it('exec → unscoped warning + last-cmd + exit-code (placeholder when missing)', () => {
    render(<AgentDetail ctx={ctxFor('exec')} />);
    const panel = screen.getByTestId('agent-detail-panel-exec');
    expect(panel.getAttribute('data-kind')).toBe('exec');
    expect(screen.getByTestId('agent-detail-exec-warning')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-last-command').textContent).toContain('(no command run)');
    expect(screen.getByTestId('agent-detail-exit-code').textContent).toBe('—');
  });

  it('exec → renders supplied last_command + exit_code from adaptor bag', () => {
    render(
      <AgentDetail
        ctx={ctxFor('exec', {
          last_command: { command: 'go test ./...', exit_code: 0 },
        })}
      />
    );
    expect(screen.getByTestId('agent-detail-last-command').textContent).toContain('go test ./...');
    expect(screen.getByTestId('agent-detail-exit-code').textContent).toBe('0');
  });

  it('exec → renders nonzero exit code with rose tint marker', () => {
    render(
      <AgentDetail
        ctx={ctxFor('exec', {
          last_command: { command: 'pytest', exit_code: 2 },
        })}
      />
    );
    const code = screen.getByTestId('agent-detail-exit-code');
    expect(code.textContent).toBe('2');
    // The rose-tint class is the visual signal; check it's wired so a
    // CSS reshuffle that drops the colour mapping fails this test.
    expect(code.className).toMatch(/rose-/);
  });

  it('subprocess → process-tree slot renders, with hook-site copy when no tree', () => {
    render(<AgentDetail ctx={ctxFor('subprocess')} />);
    const panel = screen.getByTestId('agent-detail-panel-subprocess');
    expect(panel.getAttribute('data-kind')).toBe('subprocess');
    const tree = screen.getByTestId('agent-detail-process-tree');
    expect(tree.textContent).toMatch(/process tree/i);
    // No process-tree-row when nothing was published yet.
    expect(screen.queryByTestId('agent-detail-process-1')).toBeNull();
  });

  it('subprocess → renders process tree when adaptor publishes one', () => {
    render(
      <AgentDetail
        ctx={ctxFor('subprocess', {
          process_tree: {
            pid: 1,
            command: 'gemba-agent --persona scout',
            children: [
              { pid: 17, command: 'rg --json' },
              { pid: 18, command: 'git status --porcelain' },
            ],
          },
        })}
      />
    );
    expect(screen.getByTestId('agent-detail-process-1')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-process-17')).toBeTruthy();
    expect(screen.getByTestId('agent-detail-process-18')).toBeTruthy();
  });

  it('unknown kind → falls back to UnknownKindPanel without crashing', () => {
    render(<AgentDetail ctx={ctxFor('lambda')} />);
    const panel = screen.getByTestId('agent-detail-panel-unknown');
    expect(panel).toBeTruthy();
    expect(panel.getAttribute('data-kind')).toBe('lambda');
    // None of the canonical-kind affordances should leak in.
    expect(screen.queryByTestId('agent-detail-attach')).toBeNull();
    expect(screen.queryByTestId('agent-detail-kubectl-exec')).toBeNull();
  });

  it('no workspace → renders the no-workspace explainer instead of panels', () => {
    render(
      <AgentDetail
        ctx={{
          session: {
            id: 'sess-empty',
            assignment_id: '',
            agent_id: 'alice',
            status: 'initializing',
            started_at: '2026-04-26T13:00:00Z',
          },
          agent: { id: 'alice', name: 'alice', agent_kind: 'agent' },
          workspace: null,
        }}
      />
    );
    expect(screen.getByTestId('agent-detail-no-workspace')).toBeTruthy();
    // Sanity: header still renders so the operator sees who they're
    // looking at, even with no workspace.
    expect(screen.getByTestId('agent-detail-header')).toBeTruthy();
  });
});
