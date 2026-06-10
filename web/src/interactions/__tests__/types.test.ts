import { describe, expect, it } from 'vitest';
import {
  decodeInteractionTarget,
  encodeInteractionTarget,
  runtimeHostForScope,
} from '../types';
import type { OrchestrationManifest } from '@/capabilities';

function orch(adaptorId: string): OrchestrationManifest {
  return {
    adaptor_id: adaptorId,
    adaptor_version: '0.1.0',
    orchestration_api_version: '0.1.0',
    transport: 'api',
    workspace_kinds: ['worktree'],
    group_modes: ['static'],
    cost_axes: ['tokens'],
    escalation_kinds: ['orchestrator_pause'],
    peek_modes: ['transcript'],
  };
}

describe('interaction target codec', () => {
  it('round-trips scoped ids without losing slashes or colons', () => {
    const raw = encodeInteractionTarget({ type: 'workitem', id: 'repo/path:gm-1' });
    expect(decodeInteractionTarget(raw)).toEqual({ type: 'workitem', id: 'repo/path:gm-1' });
  });

  it('falls back to a work item scope for legacy raw ids', () => {
    expect(decodeInteractionTarget('gm-1')).toEqual({ type: 'workitem', id: 'gm-1' });
  });
});

describe('runtimeHostForScope', () => {
  it('routes Gas Town epic and milestone interactions to the mayor', () => {
    expect(runtimeHostForScope({ type: 'epic', id: 'gm-e1' }, orch('gastown'))).toMatchObject({
      host: 'gastown_mayor',
    });
    expect(
      runtimeHostForScope({ type: 'milestone', id: 'gm-m1' }, orch('gastown'))
    ).toMatchObject({
      host: 'gastown_mayor',
    });
  });

  it('routes Gas Town leaf work to the crew', () => {
    expect(runtimeHostForScope({ type: 'workitem', id: 'gm-1' }, orch('gastown'))).toMatchObject({
      host: 'gastown_crew',
    });
  });

  it('uses native when the native orchestration plane is active', () => {
    expect(runtimeHostForScope({ type: 'workitem', id: 'gm-1' }, orch('native'))).toMatchObject({
      host: 'native',
    });
  });
});

