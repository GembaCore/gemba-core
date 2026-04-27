// AgentGroupBoard tests (gm-e12.8). One test per declared mode; each
// asserts the mode-specific testid surface so the dispatch in
// ModeBody is exercised end-to-end.

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AgentGroupBoard } from '../AgentGroupBoard';
import type { AgentGroup } from '@/types/core.gen';

const baseStatic: AgentGroup = {
  id: 'role:witness',
  display_name: 'Witnesses',
  mode: 'static',
  members: {
    static: [
      'gemba/hq/witness',
      'gemba/lume_spark_api/witness',
      'gemba/sb/witness',
    ],
  },
  repository: ['hq', 'lume_spark_api', 'sb'],
  extension: { role: 'witness' },
};

const basePool: AgentGroup = {
  id: 'rig:hq/polecats',
  display_name: 'hq polecats',
  mode: 'pool',
  members: {
    pool_check_url: 'gt://polecat/list?rig=hq',
    pool_min: 1,
    pool_max: 5,
  },
  repository: ['hq'],
  extension: { role: 'polecats', rig: 'hq' },
};

const baseGraph: AgentGroup = {
  id: 'graph:langgraph-flow-1',
  display_name: 'order processing flow',
  mode: 'graph',
  members: {
    graph_topology_id: 'order-processing-v3',
    graph_resolved: ['gemba/lg/intake', 'gemba/lg/router', 'gemba/lg/finalize'],
  },
  extension: { role: 'graph' },
};

describe('AgentGroupBoard', () => {
  it('renders static mode as fixed-member tiles', () => {
    render(<AgentGroupBoard group={baseStatic} />);
    expect(screen.getByTestId('agent-group-board').dataset.groupMode).toBe('static');
    expect(screen.getByTestId('agent-group-static-tiles')).toBeTruthy();
    const tiles = screen.getAllByTestId('agent-group-static-tile');
    expect(tiles).toHaveLength(3);
    expect(screen.getByText('gemba/hq/witness')).toBeTruthy();
    // Mode pill present
    expect(screen.getByTestId('agent-group-mode-pill').textContent).toBe('static');
  });

  it('renders static mode empty state when no members declared', () => {
    const empty: AgentGroup = { ...baseStatic, members: { static: [] } };
    render(<AgentGroupBoard group={empty} />);
    expect(screen.getByTestId('agent-group-static-empty')).toBeTruthy();
  });

  it('renders pool mode with desired-vs-actual gauge and check log', () => {
    render(
      <AgentGroupBoard
        group={basePool}
        poolCheck={{
          desired: 3,
          actual: 2,
          checked_at: '2026-04-27T12:00:00Z',
          log: [
            { at: '12:00', desired: 3, actual: 2 },
            { at: '11:55', desired: 3, actual: 3, note: 'reaper trimmed 1' },
          ],
        }}
      />
    );
    expect(screen.getByTestId('agent-group-board').dataset.groupMode).toBe('pool');
    expect(screen.getByTestId('agent-group-pool')).toBeTruthy();
    expect(screen.getByTestId('agent-group-pool-gauge')).toBeTruthy();
    expect(screen.getByTestId('pool-gauge-actual').textContent).toBe('2');
    expect(screen.getByTestId('pool-gauge-desired').textContent).toBe('3');
    // log present with both entries
    expect(screen.getByTestId('agent-group-pool-log')).toBeTruthy();
    expect(screen.getByText(/reaper trimmed 1/)).toBeTruthy();
    expect(screen.getByTestId('agent-group-mode-pill').textContent).toBe('pool');
  });

  it('renders pool mode falling back to pool_max when no check sample', () => {
    render(<AgentGroupBoard group={basePool} resolved={[]} />);
    // desired falls back to pool_max=5; actual falls back to resolved.length=0
    expect(screen.getByTestId('pool-gauge-actual').textContent).toBe('0');
    expect(screen.getByTestId('pool-gauge-desired').textContent).toBe('5');
  });

  it('renders graph mode as opaque box with member list', () => {
    render(<AgentGroupBoard group={baseGraph} />);
    expect(screen.getByTestId('agent-group-board').dataset.groupMode).toBe('graph');
    expect(screen.getByTestId('agent-group-graph-box')).toBeTruthy();
    // topology id surfaces inside the opaque box
    expect(screen.getByText('order-processing-v3')).toBeTruthy();
    const members = screen.getAllByTestId('agent-group-graph-member');
    expect(members).toHaveLength(3);
    expect(screen.getByTestId('agent-group-mode-pill').textContent).toBe('graph');
  });

  it('graph mode prefers resolved prop over members.graph_resolved', () => {
    render(
      <AgentGroupBoard
        group={baseGraph}
        resolved={['gemba/lg/intake-v2']}
      />
    );
    const members = screen.getAllByTestId('agent-group-graph-member');
    expect(members).toHaveLength(1);
    expect(members[0].textContent).toBe('gemba/lg/intake-v2');
  });

  it('header surfaces role badge when extension.role is present', () => {
    render(<AgentGroupBoard group={baseStatic} />);
    expect(screen.getByTestId('agent-group-role').textContent).toBe('witness');
  });
});
