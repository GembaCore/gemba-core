// Tests for graphAnalysis (gm-e12.16). Pure function — small,
// deterministic cases pin the cycle / critical-path contracts so
// regressions in the iterative Tarjan port don't slip through.

import { describe, expect, it } from 'vitest';
import {
  criticalPath,
  detectCycles,
  edgeKey,
  type DirectedEdge,
} from '../graphAnalysis';

const e = (from: string, to: string): DirectedEdge => ({ from, to });

describe('detectCycles', () => {
  it('reports an empty graph as cycle-free', () => {
    expect(detectCycles([], [])).toEqual({
      nodeIds: new Set(),
      edgeKeys: new Set(),
      sccs: [],
    });
  });

  it('reports a pure DAG as cycle-free', () => {
    const cycles = detectCycles(['a', 'b', 'c'], [e('a', 'b'), e('b', 'c')]);
    expect(cycles.nodeIds.size).toBe(0);
    expect(cycles.edgeKeys.size).toBe(0);
    expect(cycles.sccs).toEqual([]);
  });

  it('flags a 2-node cycle and the two intra-cycle edges', () => {
    const cycles = detectCycles(['a', 'b'], [e('a', 'b'), e('b', 'a')]);
    expect(cycles.nodeIds).toEqual(new Set(['a', 'b']));
    expect(cycles.edgeKeys.has(edgeKey({ from: 'a', to: 'b' }))).toBe(true);
    expect(cycles.edgeKeys.has(edgeKey({ from: 'b', to: 'a' }))).toBe(true);
  });

  it('flags a self-loop', () => {
    const cycles = detectCycles(['a'], [e('a', 'a')]);
    expect(cycles.nodeIds).toEqual(new Set(['a']));
    expect(cycles.edgeKeys.has(edgeKey({ from: 'a', to: 'a' }))).toBe(true);
  });

  it('does not mark an edge that spans two distinct SCCs', () => {
    // a → b → c → b ; the bridge edge a→b is NOT in a cycle.
    const cycles = detectCycles(
      ['a', 'b', 'c'],
      [e('a', 'b'), e('b', 'c'), e('c', 'b')]
    );
    expect(cycles.nodeIds).toEqual(new Set(['b', 'c']));
    expect(cycles.edgeKeys.has(edgeKey({ from: 'a', to: 'b' }))).toBe(false);
    expect(cycles.edgeKeys.has(edgeKey({ from: 'b', to: 'c' }))).toBe(true);
    expect(cycles.edgeKeys.has(edgeKey({ from: 'c', to: 'b' }))).toBe(true);
  });

  it('handles disjoint subgraphs independently', () => {
    const cycles = detectCycles(
      ['a', 'b', 'x', 'y'],
      [e('a', 'b'), e('x', 'y'), e('y', 'x')]
    );
    expect(cycles.nodeIds).toEqual(new Set(['x', 'y']));
    expect(cycles.sccs.length).toBe(1);
  });

  it('survives a 1k-node line graph without blowing the call stack', () => {
    const ids = Array.from({ length: 1000 }, (_, i) => `n${i}`);
    const edges: DirectedEdge[] = [];
    for (let i = 0; i < ids.length - 1; i++) edges.push(e(ids[i], ids[i + 1]));
    const cycles = detectCycles(ids, edges);
    expect(cycles.sccs).toEqual([]);
    expect(cycles.nodeIds.size).toBe(0);
  });
});

describe('criticalPath', () => {
  it('returns an empty path for an empty graph', () => {
    const cp = criticalPath([], []);
    expect(cp.nodeIds.size).toBe(0);
    expect(cp.length).toBe(0);
  });

  it('finds the longest hop chain through a DAG', () => {
    // a → b → c → d ; e is isolated. Critical path is the 3-hop chain.
    const cp = criticalPath(
      ['a', 'b', 'c', 'd', 'e'],
      [e('a', 'b'), e('b', 'c'), e('c', 'd')]
    );
    expect(cp.length).toBe(3);
    expect(cp.nodeIds).toEqual(new Set(['a', 'b', 'c', 'd']));
    expect(cp.edgeKeys.has(edgeKey({ from: 'a', to: 'b' }))).toBe(true);
    expect(cp.edgeKeys.has(edgeKey({ from: 'b', to: 'c' }))).toBe(true);
    expect(cp.edgeKeys.has(edgeKey({ from: 'c', to: 'd' }))).toBe(true);
  });

  it('prefers the longer of two competing paths', () => {
    //   a → b → d
    //   a → c → c2 → d   (longer)
    const cp = criticalPath(
      ['a', 'b', 'c', 'c2', 'd'],
      [e('a', 'b'), e('b', 'd'), e('a', 'c'), e('c', 'c2'), e('c2', 'd')]
    );
    expect(cp.length).toBe(3);
    expect(cp.nodeIds).toEqual(new Set(['a', 'c', 'c2', 'd']));
  });

  it('treats a cycle group as a single hop on the chain', () => {
    // a → (b ↔ c) → d ; the b/c cycle collapses, so the chain is
    // a → SCC(b,c) → d → length 2.
    const cp = criticalPath(
      ['a', 'b', 'c', 'd'],
      [e('a', 'b'), e('b', 'c'), e('c', 'b'), e('c', 'd')]
    );
    expect(cp.length).toBe(2);
    expect(cp.nodeIds).toEqual(new Set(['a', 'b', 'c', 'd']));
    // Intra-cycle edges along the path are highlighted so the
    // visual chain doesn't break in the middle of the SCC.
    expect(cp.edgeKeys.has(edgeKey({ from: 'b', to: 'c' }))).toBe(true);
    expect(cp.edgeKeys.has(edgeKey({ from: 'c', to: 'b' }))).toBe(true);
  });
});
