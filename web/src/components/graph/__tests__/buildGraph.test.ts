// Tests for buildGraph (gm-e12.16). Pins the manifest-gating contract:
// extension edges only land when the manifest declares them, core
// edges always do, edges to nodes we don't have are dropped.

import { describe, expect, it } from 'vitest';
import { buildGraph } from '../buildGraph';
import type { CapabilityManifest, WorkItem } from '@/types/core.gen';

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-25T00:00:00Z',
    updated_at: '2026-04-25T00:00:00Z',
    ...patch,
  };
}

function manifest(patch: Partial<CapabilityManifest> = {}): CapabilityManifest {
  return {
    adaptor_name: 'beads',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'unstarted' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
    read_only: false,
    ...patch,
  };
}

describe('buildGraph', () => {
  it('emits all three core edge kinds from WorkItem.relationships', () => {
    const items: WorkItem[] = [
      wi('a', {
        relationships: [
          { kind: 'blocks', from: 'a', to: 'b' },
          { kind: 'parent_child', from: 'a', to: 'c' },
          { kind: 'relates_to', from: 'a', to: 'd' },
        ],
      }),
      wi('b'),
      wi('c'),
      wi('d'),
    ];
    const g = buildGraph(items, manifest());
    expect(g.edges).toHaveLength(3);
    expect(g.edges.map((e) => e.kind).sort()).toEqual([
      'blocks',
      'parent_child',
      'relates_to',
    ]);
  });

  it('drops core edges to nodes that are not in the input set', () => {
    const items: WorkItem[] = [
      wi('a', { relationships: [{ kind: 'blocks', from: 'a', to: 'unknown' }] }),
    ];
    const g = buildGraph(items, manifest());
    expect(g.edges).toHaveLength(0);
  });

  it('emits extension edges only when the manifest declares them', () => {
    const items: WorkItem[] = [
      wi('a', {
        custom: {
          'beads:dependencies': [
            { type: 'discovered_from', from_bead_id: 'a', to_bead_id: 'b' },
          ],
        },
      }),
      wi('b'),
    ];
    // Without an edge_extensions declaration → drop and report.
    const without = buildGraph(items, manifest());
    expect(without.edges.filter((e) => e.isExtension)).toHaveLength(0);
    expect(without.droppedUndeclared).toBe(1);
    // With the declaration → emit.
    const withExt = buildGraph(
      items,
      manifest({
        edge_extensions: [{ name: 'discovered_from', directed: true }],
      })
    );
    expect(withExt.edges.filter((e) => e.isExtension)).toHaveLength(1);
    expect(withExt.droppedUndeclared).toBe(0);
    expect(withExt.declaredExtensionEdgeKinds).toEqual(['discovered_from']);
  });

  it('reports declaredExtensionEdgeKinds even when no item carries one', () => {
    const g = buildGraph(
      [wi('a')],
      manifest({ edge_extensions: [{ name: 'discovered_from', directed: true }] })
    );
    expect(g.declaredExtensionEdgeKinds).toEqual(['discovered_from']);
  });

  it('classifies blocks + parent_child as structural; relates_to as not', () => {
    const items: WorkItem[] = [
      wi('a', {
        relationships: [
          { kind: 'blocks', from: 'a', to: 'b' },
          { kind: 'parent_child', from: 'a', to: 'c' },
          { kind: 'relates_to', from: 'a', to: 'd' },
        ],
      }),
      wi('b'),
      wi('c'),
      wi('d'),
    ];
    const g = buildGraph(items, manifest());
    expect(g.structuralEdges).toHaveLength(2);
    expect(
      g.structuralEdges.some((e) => e.from === 'a' && e.to === 'd')
    ).toBe(false);
  });

  it('handles a null manifest (no adaptor wired) by drawing only core edges', () => {
    const items: WorkItem[] = [
      wi('a', { relationships: [{ kind: 'blocks', from: 'a', to: 'b' }] }),
      wi('b'),
    ];
    const g = buildGraph(items, null);
    expect(g.edges).toHaveLength(1);
    expect(g.declaredExtensionEdgeKinds).toEqual([]);
    expect(g.droppedUndeclared).toBe(0);
  });
});
