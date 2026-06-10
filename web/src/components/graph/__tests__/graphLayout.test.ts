// graphLayout tests — pin the top-down orientation (gm-e12.20).
//
// The layered placer maps the longest-chain depth from any root to
// the y axis (top = roots, bottom = descendants). Within a layer,
// nodes spread along x. These tests fail loudly if the axes get
// swapped back to the legacy left-to-right shape.

import { describe, expect, it } from 'vitest';
import { layoutLayered } from '../graphLayout';
import type { DirectedEdge } from '../graphAnalysis';

function edge(from: string, to: string): DirectedEdge {
  return { from, to };
}

describe('layoutLayered — top-down orientation', () => {
  it('places a chain A → B → C with strictly increasing y at the same x', () => {
    const result = layoutLayered(
      [{ id: 'A' }, { id: 'B' }, { id: 'C' }],
      [edge('A', 'B'), edge('B', 'C')]
    );
    const a = result.positions.get('A')!;
    const b = result.positions.get('B')!;
    const c = result.positions.get('C')!;
    expect(a.y).toBeLessThan(b.y);
    expect(b.y).toBeLessThan(c.y);
    // Single-occupant layers all sit at the layer's leftmost column,
    // so x is identical down the chain.
    expect(a.x).toBe(b.x);
    expect(b.x).toBe(c.x);
  });

  it('keeps both roots of a two-root graph at the same y (the top row)', () => {
    // R1 and R2 are roots; both feed into the same descendant D.
    const result = layoutLayered(
      [{ id: 'R1' }, { id: 'R2' }, { id: 'D' }],
      [edge('R1', 'D'), edge('R2', 'D')]
    );
    const r1 = result.positions.get('R1')!;
    const r2 = result.positions.get('R2')!;
    const d = result.positions.get('D')!;
    expect(r1.y).toBe(r2.y);
    expect(r1.y).toBeLessThan(d.y);
    // Roots sort by id, so R1 sits to the left of R2.
    expect(r1.x).toBeLessThan(r2.x);
  });

  it('reports a bounding box whose height grows with chain depth', () => {
    const chain = layoutLayered(
      [{ id: 'A' }, { id: 'B' }, { id: 'C' }, { id: 'D' }],
      [edge('A', 'B'), edge('B', 'C'), edge('C', 'D')]
    );
    const wide = layoutLayered(
      [{ id: 'A' }, { id: 'B' }, { id: 'C' }, { id: 'D' }],
      // No edges at all — every node sits in layer 0, so the box is
      // wide-and-short, the inverse of the chain case.
      []
    );
    expect(chain.height).toBeGreaterThan(chain.width);
    expect(wide.width).toBeGreaterThan(wide.height);
  });

  it('returns an empty result for an empty input', () => {
    const r = layoutLayered([], []);
    expect(r.positions.size).toBe(0);
    expect(r.width).toBe(0);
    expect(r.height).toBe(0);
  });

  it('ignores edges referencing unknown nodes', () => {
    // Stale edges (e.g. a referenced node was filtered upstream) MUST
    // NOT crash or mis-layer the surviving nodes.
    const r = layoutLayered(
      [{ id: 'A' }, { id: 'B' }],
      [edge('A', 'B'), edge('A', 'GHOST'), edge('GHOST', 'B')]
    );
    const a = r.positions.get('A')!;
    const b = r.positions.get('B')!;
    expect(a.y).toBeLessThan(b.y);
  });
});
