import { describe, expect, it } from 'vitest';
import {
  API_BASE,
  orchestrationTransport,
  resolveTransport,
  routePath,
  workPlaneTransport,
} from '../transport';
import type { OrchestrationManifest, WorkPlaneManifest } from '../types';

describe('resolveTransport', () => {
  it('marks api as non-proxied', () => {
    const r = resolveTransport('api');
    expect(r).toEqual({ transport: 'api', base: API_BASE, proxied: false });
  });

  it('marks jsonl and mcp as proxied', () => {
    expect(resolveTransport('jsonl').proxied).toBe(true);
    expect(resolveTransport('mcp').proxied).toBe(true);
  });
});

describe('workPlaneTransport / orchestrationTransport', () => {
  it('returns null when manifest is null', () => {
    expect(workPlaneTransport(null)).toBeNull();
    expect(orchestrationTransport(null)).toBeNull();
  });

  it('reads transport off the manifest', () => {
    const wp: WorkPlaneManifest = {
      adaptor_name: 'beads',
      adaptor_version: '0',
      protocol_version: '1',
      transport: 'jsonl',
      state_map: { open: 'unstarted' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: false,
    };
    expect(workPlaneTransport(wp)?.proxied).toBe(true);

    const op: OrchestrationManifest = {
      adaptor_id: 'gastown',
      adaptor_version: '0',
      orchestration_api_version: '1',
      transport: 'api',
      workspace_kinds: ['worktree'],
      group_modes: ['static'],
      cost_axes: ['tokens'],
      escalation_kinds: [],
      peek_modes: ['transcript'],
    };
    expect(orchestrationTransport(op)?.transport).toBe('api');
  });
});

describe('routePath', () => {
  it('joins base + path', () => {
    expect(routePath(resolveTransport('api'), '/beads')).toBe('/api/beads');
  });

  it('rejects relative paths', () => {
    expect(() => routePath(resolveTransport('api'), 'beads')).toThrow();
  });
});
