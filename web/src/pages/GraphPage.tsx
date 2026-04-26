// GraphPage (gm-e12.16). Replaces the placeholder Graph route with a
// real React Flow surface that renders the WorkItem dependency graph.
//
// Three core edge kinds (blocks, parent_child, relates_to) always
// draw. Extension edges only appear when the bound WorkPlane manifest
// declares them — the `buildGraph` helper enforces this so a stale
// manifest can't leak adaptor-internal edges.
//
// Liveness: SSE invalidates ['beads'] on workitem.* events (gm-e12.2),
// so the graph rebuilds within a frame of any mutation. We don't
// stream incremental graph patches — at 1000 nodes the recompute is
// well under a frame budget thanks to the O(V+E) analysis passes.

import { useCallback, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle,
  ArrowLeft,
  ArrowRight,
  Layers,
  Network,
  Route as RouteIcon,
} from 'lucide-react';
import type { WorkItem } from '@/types/core.gen';
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  MiniMap,
  Panel,
  type Edge,
  type Node,
  type ReactFlowInstance,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useCapabilities } from '@/capabilities/context-internal';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { WorkItemNode, type WorkItemNodeData } from '@/components/graph/WorkItemNode';
import { buildGraph } from '@/components/graph/buildGraph';
import { criticalPath, detectCycles, edgeKey } from '@/components/graph/graphAnalysis';
import { layoutLayered } from '@/components/graph/graphLayout';
import { useHotkey, useHotkeyScope } from '@/hotkeys';
import { cn } from '@/lib/utils';

// HISTORY_CAP bounds the focused-node back-stack so a long-running
// session can't accumulate unbounded predecessor breadcrumbs. 32 is
// well past any reasonable Gemba-walk traversal length.
const HISTORY_CAP = 32;

const NODE_TYPES = { workItem: WorkItemNode };

// EDGE_STYLE encodes the core-edge visual identity. relates_to renders
// horizontal + dashed because it doesn't imply ordering; blocks /
// parent_child use solid arrows. Extension edges fall through to a
// dotted style so they're visually distinct from anything core
// without making the canvas noisy.
const EDGE_STYLE: Record<string, { stroke: string; strokeDasharray?: string }> = {
  blocks: { stroke: '#dc2626' },
  parent_child: { stroke: '#0284c7' },
  relates_to: { stroke: '#737373', strokeDasharray: '4 3' },
};

const EXTENSION_EDGE_STYLE = { stroke: '#8b5cf6', strokeDasharray: '2 4' };

const HIGHLIGHT_CYCLE = '#dc2626';
const HIGHLIGHT_CRITICAL = '#f59e0b';

export function GraphPage() {
  const { data: items = [], isLoading, error } = useWorkItems();
  const { workPlane } = useCapabilities();
  const [openId, setOpenId] = useState<string | null>(null);
  const [highlightCycles, setHighlightCycles] = useState(true);
  const [criticalMode, setCriticalMode] = useState(false);
  // gm-sfbh: selection-aware viewport. Track the ReactFlow instance
  // via onInit so click-to-focus and clear-to-fit can drive the
  // camera. The id of the currently focused node is also surfaced
  // on the canvas host as data-focused-node so specs can assert
  // the selection without inspecting the React Flow viewport state.
  const instanceRef = useRef<ReactFlowInstance | null>(null);
  const [focusedId, setFocusedId] = useState<string | null>(null);
  // gm-qdqu: hovered node + its one-hop neighbours light up via
  // data-hover-related on each affected node and edge. Hover state
  // is ephemeral — leaves the DOM as soon as the pointer moves off.
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  // gm-ndb6: granularity toggle. 'items' (default) renders one node
  // per WorkItem; 'epics' rolls every WorkItem up to its enclosing
  // Epic. Edges between cross-Epic items aggregate to Epic→Epic
  // edges via deriveAggregatedItems below.
  const [granularity, setGranularity] = useState<'items' | 'epics'>('items');
  const renderItems = useMemo(
    () => (granularity === 'epics' ? deriveAggregatedItems(items) : items),
    [granularity, items]
  );

  const focusOnNode = useCallback((id: string) => {
    const inst = instanceRef.current;
    if (!inst) return;
    const node = inst.getNode(id);
    if (!node) return;
    // setCenter takes the node's position centre; node.position is
    // the top-left, so add half the rendered size when known.
    // Synchronous (no `duration`) — animations would keep React Flow
    // running into Playwright's teardown window and time out the
    // worker. Visual smoothness is a separate polish bead.
    const w = node.width ?? 200;
    const h = node.height ?? 60;
    const cx = node.position.x + w / 2;
    const cy = node.position.y + h / 2;
    void inst.setCenter(cx, cy, { zoom: 1.25 });
  }, []);

  const fitAll = useCallback(() => {
    const inst = instanceRef.current;
    if (!inst) return;
    void inst.fitView();
  }, []);

  // Manifest projection — the in-app type drops a couple of optional
  // fields the codegen carries, so we pass it through `as` instead of
  // exporting yet another shape. buildGraph only reads adaptor_name +
  // edge_extensions, both of which are present on both shapes.
  const manifest = useMemo(
    () =>
      workPlane
        ? // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (workPlane as any)
        : null,
    [workPlane]
  );

  const graph = useMemo(() => buildGraph(renderItems, manifest), [renderItems, manifest]);

  const cycles = useMemo(
    () => detectCycles(graph.nodeIds, graph.structuralEdges),
    [graph.nodeIds, graph.structuralEdges]
  );

  const critical = useMemo(
    () => criticalPath(graph.nodeIds, graph.structuralEdges),
    [graph.nodeIds, graph.structuralEdges]
  );

  const layout = useMemo(
    () => layoutLayered(graph.nodeIds.map((id) => ({ id })), graph.structuralEdges),
    [graph.nodeIds, graph.structuralEdges]
  );

  // gm-e12.20: traversal indices. successors/predecessors are derived
  // once per render from the structural-edge slice (the same set the
  // layered layout uses) so non-ordering kinds like relates_to and
  // extension edges never participate in keyboard stepping. A
  // single-outgoing successor enables ArrowRight; multi-outgoing
  // disables it (the operator falls back to clicking).
  const successors = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const e of graph.structuralEdges) {
      const list = m.get(e.from) ?? [];
      list.push(e.to);
      m.set(e.from, list);
    }
    for (const list of m.values()) list.sort();
    return m;
  }, [graph.structuralEdges]);
  const predecessors = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const e of graph.structuralEdges) {
      const list = m.get(e.to) ?? [];
      list.push(e.from);
      m.set(e.to, list);
    }
    for (const list of m.values()) list.sort();
    return m;
  }, [graph.structuralEdges]);

  // History stack of focused-node ids. Used by the back hotkey to
  // prefer the most-recently-visited predecessor when the current
  // node has more than one. Bounded to HISTORY_CAP entries so a
  // long Gemba walk doesn't accumulate without bound.
  const historyRef = useRef<string[]>([]);
  const recordVisit = useCallback((id: string) => {
    const h = historyRef.current;
    if (h[h.length - 1] === id) return;
    h.push(id);
    if (h.length > HISTORY_CAP) h.shift();
  }, []);

  const moveFocus = useCallback(
    (id: string) => {
      recordVisit(id);
      setFocusedId(id);
      focusOnNode(id);
    },
    [focusOnNode, recordVisit]
  );

  // canStepNext mirrors the next hotkey's enable rule for the
  // toolbar button — the two surfaces share a single source of
  // truth so a button click can never fire when the keystroke is
  // a no-op (or vice versa).
  const nextTarget = useMemo(() => {
    if (!focusedId) return null;
    const out = successors.get(focusedId) ?? [];
    return out.length === 1 ? out[0] : null;
  }, [focusedId, successors]);
  const canStepNext = nextTarget !== null;

  const backTarget = useMemo(() => {
    if (!focusedId) return null;
    const preds = predecessors.get(focusedId) ?? [];
    if (preds.length === 0) return null;
    if (preds.length === 1) return preds[0];
    // Ambiguous: prefer the most recently visited predecessor. The
    // history walks newest-first; the first match wins. Fall back
    // to the smallest-id predecessor when no history match exists.
    const h = historyRef.current;
    for (let i = h.length - 1; i >= 0; i--) {
      if (preds.includes(h[i])) return h[i];
    }
    return preds[0];
  }, [focusedId, predecessors]);
  const canStepBack = backTarget !== null;

  const stepNext = useCallback(() => {
    if (nextTarget) moveFocus(nextTarget);
  }, [moveFocus, nextTarget]);
  const stepBack = useCallback(() => {
    if (backTarget) moveFocus(backTarget);
  }, [moveFocus, backTarget]);
  const openFocused = useCallback(() => {
    if (focusedId) setOpenId(focusedId);
  }, [focusedId]);

  // Push the graph scope while this page is mounted so the
  // ArrowLeft / ArrowRight / Enter bindings don't leak into Board /
  // Grid / other pages where those keys carry different semantics.
  useHotkeyScope('graph');
  useHotkey('graph-next', stepNext);
  useHotkey('graph-back', stepBack);
  useHotkey('graph-open', openFocused);

  // gm-qdqu: precompute the hover-related set so node + edge passes
  // share the same answer. The set is the hovered node plus every
  // direct neighbour through any structural or extension edge.
  const hoverRelated = useMemo(() => {
    if (!hoveredId) return null;
    const set = new Set<string>([hoveredId]);
    for (const e of graph.edges) {
      if (e.from === hoveredId) set.add(e.to);
      if (e.to === hoveredId) set.add(e.from);
    }
    return set;
  }, [hoveredId, graph.edges]);

  const nodes = useMemo<Node<WorkItemNodeData>[]>(() => {
    return renderItems.map((it) => {
      const pos = layout.positions.get(it.id) ?? { x: 0, y: 0 };
      const inCycle = highlightCycles && cycles.nodeIds.has(it.id);
      const onCriticalPath = criticalMode && critical.nodeIds.has(it.id);
      const hoverRelatedNode = hoverRelated?.has(it.id) ?? false;
      return {
        id: it.id,
        type: 'workItem',
        position: pos,
        data: {
          id: it.id,
          title: it.title,
          stateCategory: it.state_category,
          inCycle,
          onCriticalPath,
          hoverRelated: hoverRelatedNode,
        },
      };
    });
  }, [renderItems, layout.positions, cycles.nodeIds, critical.nodeIds, highlightCycles, criticalMode, hoverRelated]);

  const edges = useMemo<Edge[]>(() => {
    return graph.edges.map((e) => {
      const baseStyle = e.isExtension
        ? EXTENSION_EDGE_STYLE
        : EDGE_STYLE[e.kind] ?? EDGE_STYLE.relates_to;
      const k = edgeKey(e);
      const inCycle = highlightCycles && cycles.edgeKeys.has(k);
      const onCriticalPath = criticalMode && critical.edgeKeys.has(k);
      const stroke = inCycle
        ? HIGHLIGHT_CYCLE
        : onCriticalPath
          ? HIGHLIGHT_CRITICAL
          : baseStyle.stroke;
      return {
        id: `${e.from}->${e.to}:${e.kind}`,
        source: e.from,
        target: e.to,
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: {
          stroke,
          strokeWidth: inCycle || onCriticalPath ? 2.5 : 1.25,
          strokeDasharray: baseStyle.strokeDasharray,
        },
        data: {
          kind: e.kind,
          isExtension: e.isExtension,
          inCycle,
          onCriticalPath,
        },
      };
    });
  }, [graph.edges, cycles.edgeKeys, critical.edgeKeys, highlightCycles, criticalMode]);

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="graph-page">
      <header className="flex items-start justify-between border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
            <Network className="h-5 w-5" aria-hidden />
            Graph
          </h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Dependency graph across every work item. Click a node to drill in.
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <button
            type="button"
            onClick={stepBack}
            disabled={!canStepBack}
            data-testid="graph-step-back"
            title="Step back to predecessor (ArrowLeft)"
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 transition-colors',
              'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
              'disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-white dark:disabled:hover:bg-neutral-900'
            )}
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            <span>Back</span>
          </button>
          <button
            type="button"
            onClick={stepNext}
            disabled={!canStepNext}
            data-testid="graph-step-next"
            title={
              canStepNext
                ? 'Step to next node (ArrowRight)'
                : focusedId
                  ? 'Multiple successors — click one to choose'
                  : 'Click a node to start traversal'
            }
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 transition-colors',
              'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
              'disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-white dark:disabled:hover:bg-neutral-900'
            )}
          >
            <span>Next</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </button>
          <div className="mx-1 h-4 w-px bg-neutral-200 dark:bg-neutral-800" />
          <ToggleButton
            active={granularity === 'epics'}
            onClick={() =>
              setGranularity((g) => (g === 'epics' ? 'items' : 'epics'))
            }
            testid="graph-toggle-granularity"
            icon={<Layers className="h-3.5 w-3.5" />}
          >
            {granularity === 'epics' ? 'Epics' : 'Items'}
          </ToggleButton>
          <ToggleButton
            active={highlightCycles}
            onClick={() => setHighlightCycles((v) => !v)}
            testid="graph-toggle-cycles"
            icon={<AlertTriangle className="h-3.5 w-3.5" />}
            badge={cycles.sccs.length > 0 ? cycles.sccs.length : undefined}
          >
            Cycles
          </ToggleButton>
          <ToggleButton
            active={criticalMode}
            onClick={() => setCriticalMode((v) => !v)}
            testid="graph-toggle-critical"
            icon={<RouteIcon className="h-3.5 w-3.5" />}
            badge={critical.length > 0 ? critical.length : undefined}
          >
            Critical path
          </ToggleButton>
        </div>
      </header>

      <div
        className="relative min-h-0 flex-1"
        data-testid="graph-canvas-host"
        data-focused-node={focusedId ?? undefined}
        data-granularity={granularity}
      >
        {error ? (
          <div className="m-8 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
            {error.message}
          </div>
        ) : isLoading ? (
          <div className="p-8 text-sm text-neutral-500">Loading…</div>
        ) : items.length === 0 ? (
          <div className="m-8 rounded-md border border-dashed border-neutral-300 p-8 text-center text-sm text-neutral-500 dark:border-neutral-700">
            No work items. The graph populates once the bound WorkPlane has
            something to draw.
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={NODE_TYPES}
            fitView
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            proOptions={{ hideAttribution: true }}
            onInit={(instance) => {
              instanceRef.current = instance;
            }}
            onNodeClick={(_evt, node) => {
              // gm-sfbh: focus camera on the clicked node, then open
              // the drawer. The two surfaces are independent — drawer
              // closes via Escape; the focus persists until the
              // operator clicks the canvas background to clear it.
              // gm-e12.20: route through moveFocus so the click also
              // appends to the back-history that ArrowLeft consults.
              moveFocus(node.id);
              setOpenId(node.id);
            }}
            onPaneClick={() => {
              // gm-sfbh: clearing the selection re-fits the camera
              // to the full graph and drops the focused-node marker.
              setFocusedId(null);
              fitAll();
            }}
            onNodeMouseEnter={(_evt, node) => setHoveredId(node.id)}
            onNodeMouseLeave={() => setHoveredId(null)}
            // 1000-node DoD: panOnScroll keeps the canvas responsive
            // when the graph is bigger than the viewport, and the
            // minimap gives the operator something to navigate by
            // without paying for a full layout pass per render.
            panOnScroll
            minZoom={0.1}
            maxZoom={2}
          >
            <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
            <Controls showInteractive={false} />
            <MiniMap
              pannable
              zoomable
              ariaLabel="Graph minimap"
              nodeStrokeWidth={2}
              nodeColor={(node) => {
                const data = node.data as WorkItemNodeData | undefined;
                if (data?.inCycle) return HIGHLIGHT_CYCLE;
                if (data?.onCriticalPath) return HIGHLIGHT_CRITICAL;
                return '#d4d4d4';
              }}
            />
            <Panel position="bottom-left">
              <Legend
                cycles={cycles.sccs.length}
                criticalLength={critical.length}
                extensionEdgeKinds={graph.declaredExtensionEdgeKinds}
                droppedUndeclared={graph.droppedUndeclared}
              />
            </Panel>
          </ReactFlow>
        )}
      </div>

      <WorkItemDrawer
        openId={openId}
        onClose={() => {
          // gm-sfbh: closing the drawer also clears the focused node
          // and re-fits the camera. Single Escape press → operator
          // returns to the at-a-glance view of the graph.
          setOpenId(null);
          setFocusedId(null);
          fitAll();
        }}
      />
    </div>
  );
}

interface ToggleButtonProps {
  active: boolean;
  onClick: () => void;
  testid: string;
  icon: React.ReactNode;
  badge?: number;
  children: React.ReactNode;
}

function ToggleButton({
  active,
  onClick,
  testid,
  icon,
  badge,
  children,
}: ToggleButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={testid}
      data-active={active || undefined}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 transition-colors',
        active
          ? 'border-sky-700 bg-sky-700 text-white'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
      )}
    >
      {icon}
      <span>{children}</span>
      {badge != null ? (
        <span
          className={cn(
            'rounded-full px-1.5 py-px font-mono text-[10px]',
            active ? 'bg-white/20' : 'bg-neutral-200 dark:bg-neutral-800'
          )}
        >
          {badge}
        </span>
      ) : null}
    </button>
  );
}

interface LegendProps {
  cycles: number;
  criticalLength: number;
  extensionEdgeKinds: string[];
  droppedUndeclared: number;
}

function Legend({ cycles, criticalLength, extensionEdgeKinds, droppedUndeclared }: LegendProps) {
  return (
    <div
      className="rounded-md border border-neutral-200 bg-white/95 px-3 py-2 text-[11px] shadow-sm backdrop-blur dark:border-neutral-700 dark:bg-neutral-900/95"
      data-testid="graph-legend"
    >
      <div className="mb-1 font-semibold uppercase tracking-wide text-neutral-500">
        Edges
      </div>
      <LegendRow color="#dc2626">blocks</LegendRow>
      <LegendRow color="#0284c7">parent_child</LegendRow>
      <LegendRow color="#737373" dashed>
        relates_to
      </LegendRow>
      {extensionEdgeKinds.length > 0 ? (
        <>
          <div className="mt-1 font-semibold uppercase tracking-wide text-neutral-500">
            Extension
          </div>
          {extensionEdgeKinds.map((k) => (
            <LegendRow key={k} color="#8b5cf6" dashed>
              {k}
            </LegendRow>
          ))}
        </>
      ) : null}
      {(cycles > 0 || criticalLength > 0 || droppedUndeclared > 0) && (
        <div className="mt-2 border-t border-neutral-200 pt-1 dark:border-neutral-700">
          {cycles > 0 ? (
            <div className="text-rose-700 dark:text-rose-400" data-testid="graph-legend-cycles">
              {cycles} cycle{cycles === 1 ? '' : 's'}
            </div>
          ) : null}
          {criticalLength > 0 ? (
            <div
              className="text-amber-700 dark:text-amber-400"
              data-testid="graph-legend-critical"
            >
              critical chain: {criticalLength} hops
            </div>
          ) : null}
          {droppedUndeclared > 0 ? (
            <div
              className="text-neutral-500"
              title="Adaptor surfaced edges its manifest does not declare; not drawn."
              data-testid="graph-legend-dropped"
            >
              {droppedUndeclared} edge{droppedUndeclared === 1 ? '' : 's'} hidden
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}

function LegendRow({
  color,
  dashed,
  children,
}: {
  color: string;
  dashed?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2">
      <span
        className="inline-block h-0.5 w-6 shrink-0"
        style={{
          backgroundColor: dashed ? 'transparent' : color,
          borderTop: dashed ? `1.5px dashed ${color}` : undefined,
        }}
      />
      <span>{children}</span>
    </div>
  );
}

// deriveAggregatedItems (gm-ndb6) projects every WorkItem onto its
// enclosing Epic and rewrites the relationship set so the resulting
// node list contains one synthetic WorkItem per Epic. Cross-Epic
// edges (blocks / parent_child / relates_to between items in
// different Epics) become Epic→Epic edges of the same kind. Items
// without an Epic ancestor are dropped — there's no node to render.
//
// Pure / O(V+E). Called once when the granularity toggle flips to
// 'epics' and never again until items or the toggle change.
function deriveAggregatedItems(items: WorkItem[]): WorkItem[] {
  const byId = new Map<string, WorkItem>();
  for (const it of items) byId.set(it.id, it);

  // 1. parent[id] → immediate parent_child source for `id`. Built
  //    from the child's relationships (parent_child: from=parent,
  //    to=child).
  const parent = new Map<string, string>();
  for (const it of items) {
    for (const r of it.relationships ?? []) {
      if (r.kind !== 'parent_child') continue;
      if (r.to === it.id) parent.set(it.id, r.from);
    }
  }

  // 2. Walk each item to its closest Epic ancestor (or itself, if
  //    the item is already an Epic). Memoize so chains aren't
  //    re-traversed.
  const epicOf = new Map<string, string | null>();
  function findEpic(id: string): string | null {
    if (epicOf.has(id)) return epicOf.get(id) ?? null;
    const item = byId.get(id);
    if (!item) {
      epicOf.set(id, null);
      return null;
    }
    if (item.kind === 'epic') {
      epicOf.set(id, id);
      return id;
    }
    const p = parent.get(id);
    if (!p) {
      epicOf.set(id, null);
      return null;
    }
    const ancestor = findEpic(p);
    epicOf.set(id, ancestor);
    return ancestor;
  }
  for (const it of items) findEpic(it.id);

  // 3. Build aggregated relationships per Epic. Skip parent_child
  //    edges between Epics — those define the rollup, they aren't
  //    drawn as graph dependencies. De-dup so a 100-item Epic with
  //    50 internal blocks edges to another Epic emits one Epic→Epic
  //    edge.
  const aggRels = new Map<string, Set<string>>();
  for (const it of items) {
    for (const r of it.relationships ?? []) {
      if (r.kind === 'parent_child') continue;
      const fromEpic = findEpic(r.from);
      const toEpic = findEpic(r.to);
      if (!fromEpic || !toEpic || fromEpic === toEpic) continue;
      const key = `${r.kind}:${fromEpic}->${toEpic}`;
      const set = aggRels.get(fromEpic) ?? new Set<string>();
      set.add(key);
      aggRels.set(fromEpic, set);
    }
  }

  // 4. Materialise a synthetic WorkItem per Epic with its aggregated
  //    relationships attached. Preserve every original Epic field so
  //    state pip / title still render.
  return items
    .filter((it) => it.kind === 'epic')
    .map((epic) => {
      const rels = Array.from(aggRels.get(epic.id) ?? []).map((key) => {
        const [kindRaw, edge] = key.split(':');
        const [from, to] = edge.split('->');
        return {
          kind: kindRaw as WorkItem['relationships'] extends
            | (infer R)[]
            | undefined
            ? R extends { kind: infer K }
              ? K
              : never
            : never,
          from,
          to,
        };
      });
      return { ...epic, relationships: rels } as WorkItem;
    });
}

