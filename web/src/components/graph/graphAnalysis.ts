// Pure graph algorithms used by GraphPage (gm-e12.16). No React, no
// React Flow — every function takes plain edges and returns plain
// data so we can unit-test without mounting the renderer. Two
// algorithms ship here:
//
//   * Tarjan strongly-connected-components → drives cycle highlighting.
//     Any SCC of size ≥ 2 is a dependency cycle (a self-loop also
//     qualifies, which is rare but worth flagging).
//   * Longest-path-by-hop on the SCC-condensation DAG → drives the
//     critical-path mode. Edge weights are 1 (we don't have effort
//     estimates yet); the longest hop chain is the closest stand-in
//     for "longest critical sequence" we can compute today.
//
// Both algorithms are O(V + E) — in our domain V is bounded by the
// WorkItem count (a few thousand) and E by the relationship count
// (handful per item), so we can afford to run both on every render
// without memoisation overhead. The page memoises anyway to avoid
// recomputing on unrelated state changes.

export interface DirectedEdge {
  from: string;
  to: string;
}

export interface CycleInfo {
  // nodeIds is the set of node ids that participate in any cycle —
  // either an SCC of size ≥ 2 or a self-loop.
  nodeIds: Set<string>;
  // edgeKeys is the set of "from→to" keys for edges whose endpoints
  // are both in the same cycle. A spanning edge between two SCCs is
  // not in this set; only intra-cycle edges are.
  edgeKeys: Set<string>;
  // sccs is the ordered list of cyclic SCCs (each is a list of node
  // ids). Useful for surfacing a "you have N cycles" badge.
  sccs: string[][];
}

// detectCycles runs Tarjan's SCC algorithm and reports which nodes /
// edges sit inside a cycle. Nodes referenced by edges but not present
// in the explicit nodes list are still considered (the algorithm
// builds its node universe from edges + nodes ∪) so a graph drawn
// from a partial WorkItem fetch doesn't silently miss cycles.
export function detectCycles(nodes: string[], edges: DirectedEdge[]): CycleInfo {
  const adj = new Map<string, string[]>();
  const seen = new Set<string>();
  const ensure = (id: string) => {
    if (!adj.has(id)) adj.set(id, []);
    seen.add(id);
  };
  for (const n of nodes) ensure(n);
  for (const e of edges) {
    ensure(e.from);
    ensure(e.to);
    adj.get(e.from)!.push(e.to);
  }

  // Tarjan state. Iterative with an explicit stack so a deep DAG
  // (1000-node line) doesn't blow the JS stack.
  let index = 0;
  const idx = new Map<string, number>();
  const low = new Map<string, number>();
  const onStack = new Set<string>();
  const stack: string[] = [];
  const sccs: string[][] = [];

  type Frame = { node: string; iter: number };
  const callStack: Frame[] = [];

  const visit = (start: string) => {
    callStack.push({ node: start, iter: 0 });
    idx.set(start, index);
    low.set(start, index);
    index++;
    stack.push(start);
    onStack.add(start);

    while (callStack.length > 0) {
      const frame = callStack[callStack.length - 1];
      const neighbours = adj.get(frame.node) ?? [];
      if (frame.iter < neighbours.length) {
        const w = neighbours[frame.iter];
        frame.iter++;
        if (!idx.has(w)) {
          idx.set(w, index);
          low.set(w, index);
          index++;
          stack.push(w);
          onStack.add(w);
          callStack.push({ node: w, iter: 0 });
        } else if (onStack.has(w)) {
          low.set(frame.node, Math.min(low.get(frame.node)!, idx.get(w)!));
        }
        continue;
      }
      // Children exhausted — possibly close an SCC.
      if (low.get(frame.node) === idx.get(frame.node)) {
        const scc: string[] = [];
        let popped: string;
        do {
          popped = stack.pop()!;
          onStack.delete(popped);
          scc.push(popped);
        } while (popped !== frame.node);
        sccs.push(scc);
      }
      callStack.pop();
      if (callStack.length > 0) {
        const parent = callStack[callStack.length - 1];
        low.set(parent.node, Math.min(low.get(parent.node)!, low.get(frame.node)!));
      }
    }
  };

  for (const n of seen) {
    if (!idx.has(n)) visit(n);
  }

  const cycleNodes = new Set<string>();
  const cycleEdges = new Set<string>();
  const cyclicSccs: string[][] = [];
  // Self-loop check: an SCC of size 1 is cyclic only if the node has
  // an edge back to itself.
  const selfLoops = new Set<string>();
  for (const e of edges) {
    if (e.from === e.to) selfLoops.add(e.from);
  }
  for (const scc of sccs) {
    const cyclic = scc.length > 1 || selfLoops.has(scc[0]);
    if (!cyclic) continue;
    cyclicSccs.push(scc);
    for (const n of scc) cycleNodes.add(n);
  }
  // Mark intra-SCC edges. A node-to-component map keeps the edge
  // classification O(E) instead of O(E·|SCC|).
  const compOf = new Map<string, number>();
  for (let i = 0; i < sccs.length; i++) {
    for (const n of sccs[i]) compOf.set(n, i);
  }
  const cyclicSet = new Set<number>();
  for (const scc of cyclicSccs) cyclicSet.add(compOf.get(scc[0])!);
  for (const e of edges) {
    if (compOf.get(e.from) === compOf.get(e.to) && cyclicSet.has(compOf.get(e.from)!)) {
      cycleEdges.add(edgeKey(e));
    }
  }
  return { nodeIds: cycleNodes, edgeKeys: cycleEdges, sccs: cyclicSccs };
}

export interface CriticalPath {
  nodeIds: Set<string>;
  edgeKeys: Set<string>;
  // length is the hop count along the longest chain. Surfaced in the
  // legend so an operator can tell whether the highlight reflects a
  // 30-link mountain or a 3-link shortcut.
  length: number;
}

// criticalPath returns the longest hop chain through the
// SCC-condensation of (nodes, edges). On a pure DAG this is simply
// the longest path; on a graph with cycles, each cycle is collapsed
// to a super-node so the highlight stays a single contiguous chain.
//
// We pick "longest by hop count" rather than "longest by some
// estimate" because WorkItem doesn't carry effort estimates today. If
// the manifest grows a `priority_weight` or similar later, swap the
// edge weight without touching the algorithm structure.
export function criticalPath(nodes: string[], edges: DirectedEdge[]): CriticalPath {
  // Step 1: build SCC condensation. We rerun Tarjan because we want
  // the per-node component id; detectCycles throws away that map.
  const adj = new Map<string, string[]>();
  const all = new Set<string>();
  const ensure = (id: string) => {
    if (!adj.has(id)) adj.set(id, []);
    all.add(id);
  };
  for (const n of nodes) ensure(n);
  for (const e of edges) {
    ensure(e.from);
    ensure(e.to);
    adj.get(e.from)!.push(e.to);
  }

  // SCC discovery (iterative, same shape as detectCycles).
  let index = 0;
  const idx = new Map<string, number>();
  const low = new Map<string, number>();
  const onStack = new Set<string>();
  const stack: string[] = [];
  const compOf = new Map<string, number>();
  const components: string[][] = [];

  type Frame = { node: string; iter: number };
  const callStack: Frame[] = [];

  const visit = (start: string) => {
    callStack.push({ node: start, iter: 0 });
    idx.set(start, index);
    low.set(start, index);
    index++;
    stack.push(start);
    onStack.add(start);
    while (callStack.length > 0) {
      const frame = callStack[callStack.length - 1];
      const neighbours = adj.get(frame.node) ?? [];
      if (frame.iter < neighbours.length) {
        const w = neighbours[frame.iter];
        frame.iter++;
        if (!idx.has(w)) {
          idx.set(w, index);
          low.set(w, index);
          index++;
          stack.push(w);
          onStack.add(w);
          callStack.push({ node: w, iter: 0 });
        } else if (onStack.has(w)) {
          low.set(frame.node, Math.min(low.get(frame.node)!, idx.get(w)!));
        }
        continue;
      }
      if (low.get(frame.node) === idx.get(frame.node)) {
        const comp: string[] = [];
        let popped: string;
        do {
          popped = stack.pop()!;
          onStack.delete(popped);
          comp.push(popped);
          compOf.set(popped, components.length);
        } while (popped !== frame.node);
        components.push(comp);
      }
      callStack.pop();
      if (callStack.length > 0) {
        const parent = callStack[callStack.length - 1];
        low.set(parent.node, Math.min(low.get(parent.node)!, low.get(frame.node)!));
      }
    }
  };
  for (const n of all) if (!idx.has(n)) visit(n);

  // Step 2: build condensation edges (edges between distinct SCCs).
  const condAdj = new Map<number, Set<number>>();
  const condIn = new Map<number, Set<number>>();
  for (let i = 0; i < components.length; i++) {
    condAdj.set(i, new Set());
    condIn.set(i, new Set());
  }
  for (const e of edges) {
    const a = compOf.get(e.from);
    const b = compOf.get(e.to);
    if (a == null || b == null || a === b) continue;
    condAdj.get(a)!.add(b);
    condIn.get(b)!.add(a);
  }

  // Step 3: topological sort of the condensation DAG. Tarjan emits
  // SCCs in reverse topological order, so reversing `components` gives
  // a valid topo order. We rely on that rather than a separate Kahn
  // pass — keeps the function tight without sacrificing correctness.
  const topo = components.map((_, i) => i).reverse();

  // Step 4: longest path DP on the condensation.
  const dist = new Map<number, number>();
  const pred = new Map<number, number | null>();
  for (let i = 0; i < components.length; i++) {
    dist.set(i, 0);
    pred.set(i, null);
  }
  let bestEnd = -1;
  let bestDist = -1;
  for (const u of topo) {
    const du = dist.get(u)!;
    if (du > bestDist) {
      bestDist = du;
      bestEnd = u;
    }
    for (const v of condAdj.get(u) ?? []) {
      const candidate = du + 1;
      if (candidate > (dist.get(v) ?? -1)) {
        dist.set(v, candidate);
        pred.set(v, u);
      }
    }
  }

  if (bestEnd < 0) {
    return { nodeIds: new Set(), edgeKeys: new Set(), length: 0 };
  }

  // Walk predecessor chain to recover the SCC sequence; expand each
  // SCC into its node ids. The path length we report is hops between
  // SCCs, so a single-cycle group counts as one stop along the chain.
  const path: number[] = [];
  let cur: number | null = bestEnd;
  while (cur != null) {
    path.push(cur);
    cur = pred.get(cur) ?? null;
  }
  path.reverse();

  const pathSet = new Set(path);
  const pathNodeIds = new Set<string>();
  for (const compIdx of path) {
    for (const n of components[compIdx]) pathNodeIds.add(n);
  }
  // Edges along the path: any edge whose endpoints are in two
  // adjacent SCCs on the chain, OR an intra-SCC edge inside a cyclic
  // group on the path (so the highlight stays connected through cycles).
  const adjacentPairs = new Set<string>();
  for (let i = 0; i < path.length - 1; i++) {
    adjacentPairs.add(`${path[i]}->${path[i + 1]}`);
  }
  const pathEdgeKeys = new Set<string>();
  for (const e of edges) {
    const a = compOf.get(e.from);
    const b = compOf.get(e.to);
    if (a == null || b == null) continue;
    if (a === b && pathSet.has(a) && components[a].length > 1) {
      pathEdgeKeys.add(edgeKey(e));
    } else if (adjacentPairs.has(`${a}->${b}`)) {
      pathEdgeKeys.add(edgeKey(e));
    }
  }

  return { nodeIds: pathNodeIds, edgeKeys: pathEdgeKeys, length: bestDist };
}

export function edgeKey(e: DirectedEdge): string {
  return `${e.from}\u0000${e.to}`;
}
