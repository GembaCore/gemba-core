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

import { useMemo, useState } from 'react';
import { AlertTriangle, Network, Route as RouteIcon } from 'lucide-react';
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  MiniMap,
  Panel,
  type Edge,
  type Node,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useCapabilities } from '@/capabilities/context-internal';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { WorkItemNode, type WorkItemNodeData } from '@/components/graph/WorkItemNode';
import { buildGraph } from '@/components/graph/buildGraph';
import { criticalPath, detectCycles, edgeKey } from '@/components/graph/graphAnalysis';
import { layoutLayered } from '@/components/graph/graphLayout';
import { cn } from '@/lib/utils';

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

  const graph = useMemo(() => buildGraph(items, manifest), [items, manifest]);

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

  const nodes = useMemo<Node<WorkItemNodeData>[]>(() => {
    return items.map((it) => {
      const pos = layout.positions.get(it.id) ?? { x: 0, y: 0 };
      const inCycle = highlightCycles && cycles.nodeIds.has(it.id);
      const onCriticalPath = criticalMode && critical.nodeIds.has(it.id);
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
        },
      };
    });
  }, [items, layout.positions, cycles.nodeIds, critical.nodeIds, highlightCycles, criticalMode]);

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

      <div className="relative min-h-0 flex-1" data-testid="graph-canvas-host">
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
            onNodeClick={(_evt, node) => setOpenId(node.id)}
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

      <WorkItemDrawer openId={openId} onClose={() => setOpenId(null)} />
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

