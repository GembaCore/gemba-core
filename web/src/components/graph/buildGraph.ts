// buildGraph assembles the React Flow inputs (nodes, edges) from the
// raw WorkItem list plus the WorkPlane manifest's edge_extensions
// declaration. Pure — same shape returned for the same input — so
// memoisation in GraphPage is meaningful.
//
// Edge kind handling:
//   * Three core kinds (blocks, parent_child, relates_to) are always
//     drawn from `WorkItem.relationships`.
//   * Adaptor-native edges live in `WorkItem.custom["beads:dependencies"]`
//     (the bd adaptor's extension surface). We only emit them when
//     the bound manifest declares them in `edge_extensions` — that's
//     the gm-root DD-15 contract: extension data without a manifest
//     entry is silently dropped, so a hostile or stale adaptor can't
//     pollute the graph.
//   * Unknown edge kinds (relates_to from a foreign adaptor we haven't
//     declared) are dropped, not coerced. Coercing would let a kind
//     name collision render under the wrong style.

import type { CapabilityManifest, RelationshipKind, WorkItem } from '@/types/core.gen';
import type { DirectedEdge } from './graphAnalysis';

export const CORE_EDGE_KINDS: ReadonlyArray<RelationshipKind> = [
  'blocks',
  'parent_child',
  'relates_to',
];

// STRUCTURAL_KINDS drive the layered layout — these are the edges
// where direction implies sequencing. relates_to is horizontal and
// extension edges default to horizontal too unless an extension widget
// provides a custom layout hint (gm-e12.16 doesn't ship one).
const STRUCTURAL_KINDS: ReadonlySet<string> = new Set(['blocks', 'parent_child']);

export interface GraphEdge extends DirectedEdge {
  kind: string;
  isExtension: boolean;
}

export interface BuildGraphResult {
  nodeIds: string[];
  edges: GraphEdge[];
  // structuralEdges is the subset that drives layering. Same shape as
  // edges but pre-filtered so the layout module doesn't need to know
  // which kinds count as structural.
  structuralEdges: DirectedEdge[];
  // declaredExtensionEdgeKinds reports which extension edge names
  // the manifest declared. Used by the legend so the operator sees
  // which adaptor-native edges are even possible to encounter.
  declaredExtensionEdgeKinds: string[];
  // droppedUndeclared is the count of extension-edge candidates we
  // saw on items but rejected because the manifest didn't list them.
  // Surfaced as a small "N edges hidden" warning in the legend so a
  // misconfigured adaptor is debuggable rather than silently mute.
  droppedUndeclared: number;
}

interface BeadsDependency {
  type?: string;
  to_bead_id?: string;
  from_bead_id?: string;
}

// extensionEdgeFields maps an adaptor name to the WorkItem.custom key
// where its native edge rows live. Today only the bd adaptor surfaces
// extension edges; future adaptors slot in here without the page or
// the layout caring.
const EXTENSION_EDGE_KEYS: Record<string, string> = {
  beads: 'beads:dependencies',
};

export function buildGraph(
  items: WorkItem[],
  manifest: CapabilityManifest | null
): BuildGraphResult {
  const declared = new Set(
    (manifest?.edge_extensions ?? []).map((ext) => ext.name)
  );
  const nodeIds = items.map((it) => it.id);
  const knownIds = new Set(nodeIds);
  const edges: GraphEdge[] = [];
  const structuralEdges: DirectedEdge[] = [];
  let droppedUndeclared = 0;

  for (const it of items) {
    // Core relationships — always drawn. Source server omits unknown
    // kinds by contract, but we double-check anyway so a future
    // schema relaxation doesn't sneak through unstyled.
    for (const r of it.relationships ?? []) {
      const from = r.from || it.id;
      const to = r.to;
      if (!from || !to) continue;
      // Drop edges to nodes we don't have — keeps the canvas honest
      // when the API returns a paginated subset (today the list endpoint
      // returns everything so this is a no-op, but the property is
      // worth pinning).
      if (!knownIds.has(from) || !knownIds.has(to)) continue;
      if (!CORE_EDGE_KINDS.includes(r.kind)) continue;
      const ge: GraphEdge = { from, to, kind: r.kind, isExtension: false };
      edges.push(ge);
      if (STRUCTURAL_KINDS.has(r.kind)) {
        structuralEdges.push({ from, to });
      }
    }

    // Extension edges. Only emit when the manifest declared them.
    const adaptorName = manifest?.adaptor_name;
    const key = adaptorName ? EXTENSION_EDGE_KEYS[adaptorName] : undefined;
    if (!key) continue;
    const raw = it.custom?.[key];
    if (!Array.isArray(raw)) continue;
    for (const dep of raw as BeadsDependency[]) {
      const kind = dep.type;
      if (!kind) continue;
      // Some bd kinds are already mapped to a core kind (gm-e6.2);
      // the gm-peg list handler emits the core projection, so the
      // raw native row is informational. Only the kinds the manifest
      // explicitly DECLARES as extensions get drawn — anything else
      // is a duplicate of a core edge or a typo.
      if (!declared.has(kind)) {
        droppedUndeclared++;
        continue;
      }
      const from = dep.from_bead_id ?? it.id;
      const to = dep.to_bead_id;
      if (!from || !to) continue;
      if (!knownIds.has(from) || !knownIds.has(to)) continue;
      edges.push({ from, to, kind, isExtension: true });
    }
  }

  return {
    nodeIds,
    edges,
    structuralEdges,
    declaredExtensionEdgeKinds: Array.from(declared),
    droppedUndeclared,
  };
}
