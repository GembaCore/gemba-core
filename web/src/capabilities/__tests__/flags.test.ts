import { describe, expect, it } from 'vitest';
import { workPlaneFlags } from '../types';
import type { WorkPlaneManifest } from '../types';

// These tests are the TS-side counterpart of internal/core/manifest_test.go.
// The two Flags() implementations must stay byte-for-byte equivalent so
// gating decisions made on the Go side (doctor, capability router) match
// what the SPA shows. If a projection rule changes here but not in Go
// (or vice versa), that divergence is a bug.

function base(): WorkPlaneManifest {
  return {
    adaptor_name: 'beads',
    adaptor_version: '0.1.0',
    protocol_version: '1',
    transport: 'api',
    state_map: { open: 'unstarted' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  };
}

describe('workPlaneFlags', () => {
  it('returns null when the manifest is null', () => {
    expect(workPlaneFlags(null)).toBeNull();
  });

  it('projects all flags false for a minimal manifest (except declared state)', () => {
    const flags = workPlaneFlags(base());
    expect(flags).toEqual({
      has_sprints: false,
      has_budget: false,
      has_evidence: false,
      has_declared_state: true,
      has_extension_edges: false,
      has_extension_fields: false,
      has_extension_rel_fields: false,
    });
  });

  it('mirrors sprint_native → has_sprints', () => {
    const m = base();
    m.sprint_native = true;
    expect(workPlaneFlags(m)!.has_sprints).toBe(true);
  });

  it('mirrors token_budget_enforced → has_budget', () => {
    const m = base();
    m.token_budget_enforced = true;
    expect(workPlaneFlags(m)!.has_budget).toBe(true);
  });

  it('mirrors evidence_synthesis_required → has_evidence', () => {
    const m = base();
    m.evidence_synthesis_required = true;
    expect(workPlaneFlags(m)!.has_evidence).toBe(true);
  });

  it('derives has_declared_state from state_map non-emptiness', () => {
    const m = base();
    m.state_map = {};
    expect(workPlaneFlags(m)!.has_declared_state).toBe(false);
  });

  it('derives has_extension_edges from edge_extensions presence', () => {
    const m = base();
    expect(workPlaneFlags(m)!.has_extension_edges).toBe(false);
    m.edge_extensions = [{ name: 'duplicates', directed: true }];
    expect(workPlaneFlags(m)!.has_extension_edges).toBe(true);
  });

  it('derives has_extension_fields from field_extensions presence', () => {
    const m = base();
    expect(workPlaneFlags(m)!.has_extension_fields).toBe(false);
    m.field_extensions = [{ name: 'story_points', type: 'number' }];
    expect(workPlaneFlags(m)!.has_extension_fields).toBe(true);
  });

  it('derives has_extension_rel_fields from relationship_extensions presence', () => {
    const m = base();
    expect(workPlaneFlags(m)!.has_extension_rel_fields).toBe(false);
    m.relationship_extensions = [{ name: 'confidence', type: 'number' }];
    expect(workPlaneFlags(m)!.has_extension_rel_fields).toBe(true);
  });

  it('is deterministic — two calls return equal projections', () => {
    const m = base();
    m.sprint_native = true;
    expect(workPlaneFlags(m)).toEqual(workPlaneFlags(m));
  });
});
