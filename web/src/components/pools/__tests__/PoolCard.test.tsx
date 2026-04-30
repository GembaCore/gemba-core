// PoolCard tests (gm-s47n.16). Pins the clamp-warning banner and
// the validation issue rendering.

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { PoolCard, type PoolCardData } from '../PoolCard';

function makeData(patch: Partial<PoolCardData> = {}): PoolCardData {
  return {
    scope: 'local',
    persona: 'engineer-claude',
    entry: {
      size: 2,
      agent_type: 'claude',
      floor: 0.4,
      recycle_after_beads: 5,
      idle_ceiling_minutes: 30,
      min_interval_per_session_seconds: 0,
      max_concurrent: 0,
    },
    ...patch,
  };
}

describe('PoolCard', () => {
  it('renders persona and size inputs', () => {
    render(
      <PoolCard
        data={makeData()}
        scopeAxis="hidden"
        scopes={['local']}
        personas={['engineer-claude', 'pm-claude']}
        agentTypes={['claude']}
        maxParallel={4}
        reservedForManual={1}
        onChange={() => {}}
        onRemove={() => {}}
        issues={[]}
      />
    );
    expect(screen.getByTestId('pool-card-persona')).toBeTruthy();
    expect(screen.getByTestId('pool-card-size')).toBeTruthy();
  });

  it('shows clamp warning when size exceeds MaxParallel - reserved', () => {
    render(
      <PoolCard
        data={makeData({ entry: { ...makeData().entry, size: 10 } })}
        scopeAxis="hidden"
        scopes={['local']}
        personas={['engineer-claude']}
        agentTypes={['claude']}
        maxParallel={4}
        reservedForManual={1}
        onChange={() => {}}
        onRemove={() => {}}
        issues={[]}
      />
    );
    expect(screen.getByTestId('pool-card-clamp-warning')).toBeTruthy();
  });

  it('renders error issues when provided', () => {
    render(
      <PoolCard
        data={makeData()}
        scopeAxis="hidden"
        scopes={['local']}
        personas={['engineer-claude']}
        agentTypes={['claude']}
        maxParallel={4}
        reservedForManual={1}
        onChange={() => {}}
        onRemove={() => {}}
        issues={[{ severity: 'error', message: 'persona unknown' }]}
      />
    );
    expect(screen.getByTestId('pool-card-issue-error')).toBeTruthy();
  });

  it('hides scope dropdown when scopeAxis=hidden', () => {
    render(
      <PoolCard
        data={makeData()}
        scopeAxis="hidden"
        scopes={['local']}
        personas={['engineer-claude']}
        agentTypes={['claude']}
        maxParallel={4}
        reservedForManual={1}
        onChange={() => {}}
        onRemove={() => {}}
        issues={[]}
      />
    );
    expect(screen.queryByTestId('pool-card-scope')).toBeNull();
  });

  it('shows scope dropdown when scopeAxis=rig', () => {
    render(
      <PoolCard
        data={makeData()}
        scopeAxis="rig"
        scopes={['gemba', 'lume']}
        personas={['engineer-claude']}
        agentTypes={['claude']}
        maxParallel={4}
        reservedForManual={1}
        onChange={() => {}}
        onRemove={() => {}}
        issues={[]}
      />
    );
    expect(screen.getByTestId('pool-card-scope')).toBeTruthy();
  });

  it('fires onRemove when Remove clicked', () => {
    const onRemove = vi.fn();
    render(
      <PoolCard
        data={makeData()}
        scopeAxis="hidden"
        scopes={['local']}
        personas={['engineer-claude']}
        agentTypes={['claude']}
        maxParallel={4}
        reservedForManual={1}
        onChange={() => {}}
        onRemove={onRemove}
        issues={[]}
      />
    );
    fireEvent.click(screen.getByTestId('pool-card-remove'));
    expect(onRemove).toHaveBeenCalled();
  });
});
