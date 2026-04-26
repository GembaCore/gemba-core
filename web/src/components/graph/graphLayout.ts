// Layered DAG layout for the dependency graph (gm-e12.16, top-down
// re-orient gm-e12.20).
//
// We deliberately avoid a third-party layout dep (dagre, elkjs) — the
// dependency graph is a sparse DAG with a handful of edge types and
// at-most a few thousand nodes, and a hand-rolled layered placer is
// faster to tune to our domain than to drag in a 200KB layout engine.
//
// Layering: each node sits in a row equal to the longest hop chain
// from any root. Roots (no incoming structural edges) anchor the top
// of the canvas; descendants flow downward. Nodes inside cycles take
// the layer of any one cycle member — they cluster together because
// the rendering layer paints them red anyway.
//
// Axes (gm-e12.20): layer → y (vertical), index-within-layer → x
// (horizontal). The previous left-to-right orientation read upside-
// down for a dependency view; top-down matches how operators read
// "what's upstream / downstream" in a vertical scroll surface.
//
// Within a layer we sort by id to keep the layout deterministic
// across renders. Horizontal spacing scales with the densest layer so
// neighbours never overlap.

import type { DirectedEdge } from './graphAnalysis';

export interface LayoutNodeInput {
  id: string;
}

export interface LayoutResult {
  // positions maps each node id to its (x, y) in graph-space pixels.
  positions: Map<string, { x: number; y: number }>;
  // width / height are the layout's bounding box; the GraphPage uses
  // these to fit-view the React Flow canvas on initial mount.
  width: number;
  height: number;
}

const COLUMN_WIDTH = 220;
const ROW_HEIGHT = 80;
const LAYER_PADDING = 40;

// layoutLayered places nodes row-by-row from longest-chain depth.
// Edges classified as "structural" drive layering; the
// `structuralEdges` parameter lets the caller exclude non-ordering
// kinds (relates_to, extension) which would otherwise inflate depth
// estimates without representing a real predecessor / successor
// relationship.
export function layoutLayered(
  nodes: LayoutNodeInput[],
  structuralEdges: DirectedEdge[]
): LayoutResult {
  const ids = nodes.map((n) => n.id);
  if (ids.length === 0) {
    return { positions: new Map(), width: 0, height: 0 };
  }

  // Build adjacency and in-degree only over structural edges.
  const out = new Map<string, string[]>();
  const inDeg = new Map<string, number>();
  for (const id of ids) {
    out.set(id, []);
    inDeg.set(id, 0);
  }
  for (const e of structuralEdges) {
    if (!out.has(e.from) || !out.has(e.to)) continue;
    out.get(e.from)!.push(e.to);
    inDeg.set(e.to, (inDeg.get(e.to) ?? 0) + 1);
  }

  // Layer assignment via longest-path DP. Since the input may contain
  // cycles, we run a relaxation that bails after V iterations — any
  // node whose layer is still being raised after that count is part
  // of a cycle and stays at the maximum it's reached so far.
  const layer = new Map<string, number>();
  for (const id of ids) layer.set(id, 0);
  const v = ids.length;
  for (let iter = 0; iter < v; iter++) {
    let changed = false;
    for (const id of ids) {
      const baseLayer = layer.get(id)!;
      for (const w of out.get(id) ?? []) {
        if ((layer.get(w) ?? 0) <= baseLayer) {
          layer.set(w, baseLayer + 1);
          changed = true;
        }
      }
    }
    if (!changed) break;
  }

  // Bucket nodes by layer, sort each layer by id for determinism.
  const layers: string[][] = [];
  for (const id of ids) {
    const L = layer.get(id) ?? 0;
    while (layers.length <= L) layers.push([]);
    layers[L].push(id);
  }
  for (const bucket of layers) bucket.sort();

  const positions = new Map<string, { x: number; y: number }>();
  let maxCols = 0;
  for (let row = 0; row < layers.length; row++) {
    const bucket = layers[row];
    if (bucket.length > maxCols) maxCols = bucket.length;
    for (let col = 0; col < bucket.length; col++) {
      positions.set(bucket[col], {
        x: col * COLUMN_WIDTH + LAYER_PADDING,
        y: row * ROW_HEIGHT + LAYER_PADDING,
      });
    }
  }

  return {
    positions,
    width: maxCols * COLUMN_WIDTH + LAYER_PADDING * 2,
    height: layers.length * ROW_HEIGHT + LAYER_PADDING * 2,
  };
}
