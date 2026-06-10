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

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  Filter,
  Layers,
  Network,
  RotateCcw,
  Route as RouteIcon,
  Wand2,
} from 'lucide-react';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
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
import { useRhp } from '@/components/rhp/RhpContext';
import {
  MILESTONE_ALL,
  buildMilestoneOptions,
  filterByMilestone,
  type MilestoneID,
} from '@/components/board/milestone';
import {
  SCOPE_ALL,
  buildScopeOptions,
  filterByScope,
  type ScopeID,
} from '@/components/board/scope';
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

// gm-vubw: density-UX threshold. Below this zoom the items view is
// unreadable (a 320-node real workspace lands at ~0.02), so when the
// operator hasn't taken manual control of the granularity toggle we
// auto-flip to 'epics' aggregation. Picked at 0.3 because below it
// nodes render under ~60px wide on a 200px layout — too small to
// read titles. Above it items stay individually legible.
const AUTO_GRANULARITY_ZOOM_THRESHOLD = 0.3;
const NARROW_CANVAS_WIDTH = 420;
const NARROW_CANVAS_OVERVIEW_ZOOM = 0.9;
const GRAPH_SEARCH_PARAM = 'q';

function statesFromQuery(p: URLSearchParams): StateCategory[] {
  const all = p.getAll('state_category');
  return all.filter((s): s is StateCategory => (STATE_CATEGORIES as readonly string[]).includes(s));
}

function kindsFromQuery(p: URLSearchParams): string[] {
  return p.getAll('kind').filter((s) => s.length > 0);
}

export function GraphPage() {
  const { data: items = [], isLoading, error } = useWorkItems();
  const [params, setParams] = useSearchParams();
  const { workPlane } = useCapabilities();
  const { popDetail, tabs } = useRhp();
  const [highlightCycles, setHighlightCycles] = useState(true);
  const [criticalMode, setCriticalMode] = useState(false);
  // gm-sfbh: selection-aware viewport. Track the ReactFlow instance
  // via onInit so click-to-focus and clear-to-fit can drive the
  // camera. The id of the currently focused node is also surfaced
  // on the canvas host as data-focused-node so specs can assert
  // the selection without inspecting the React Flow viewport state.
  const instanceRef = useRef<ReactFlowInstance | null>(null);
  const canvasHostRef = useRef<HTMLDivElement | null>(null);
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const [canvasWidth, setCanvasWidth] = useState(Number.POSITIVE_INFINITY);
  // gm-sfbh (post-RHP): the legacy WorkItemDrawer's onClose used to
  // clear focusedId + re-fit the camera in one step (Escape, ×, click-
  // outside all funneled through it). The drawer is gone — its content
  // lives on an RHP detail tab now — so we mirror the behavior by
  // watching the RHP's open tabs: when no `workitem` detail tab is
  // open anymore we drop focus and re-fit. Keeps Escape (Radix-
  // independent: the RHP closes the focused detail tab on the
  // drawer-close hotkey) and the × button consistent with the prior
  // contract.
  const hasWorkItemDetailTab = tabs.some((t) => t.kind === 'workitem');
  // gm-qdqu: hovered node + its one-hop neighbours light up via
  // data-hover-related on each affected node and edge. Hover state
  // is ephemeral — leaves the DOM as soon as the pointer moves off.
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  // gm-ndb6: granularity toggle. 'items' (default) renders one node
  // per WorkItem; 'epics' rolls every WorkItem up to its enclosing
  // Epic. Edges between cross-Epic items aggregate to Epic→Epic
  // edges via deriveAggregatedItems below.
  const [granularity, setGranularity] = useState<'items' | 'epics'>('items');
  // gm-vubw: when granularityOverride is false the effective view is
  // driven by zoom — under AUTO_GRANULARITY_ZOOM_THRESHOLD we render
  // epics so a 300-node workspace shows readable units instead of
  // microscopic dots. Once the operator clicks the toggle we set
  // override=true and stop auto-flipping; the operator can re-enable
  // auto via the Auto button.
  const [granularityOverride, setGranularityOverride] = useState(false);
  const [currentZoom, setCurrentZoom] = useState(1);
  const milestone: MilestoneID = params.get('milestone') ?? MILESTONE_ALL;
  const scope: ScopeID = params.get('scope') ?? SCOPE_ALL;
  const stateFilters = useMemo(() => statesFromQuery(params), [params]);
  const kindFilters = useMemo(() => kindsFromQuery(params), [params]);
  const search = params.get(GRAPH_SEARCH_PARAM) ?? '';

  const updateParams = useCallback(
    (mutate: (next: URLSearchParams) => void) => {
      const next = new URLSearchParams(params);
      mutate(next);
      setParams(next, { replace: true });
    },
    [params, setParams]
  );
  const setMilestone = useCallback(
    (nextMilestone: MilestoneID) => {
      updateParams((next) => {
        if (nextMilestone === MILESTONE_ALL) next.delete('milestone');
        else next.set('milestone', nextMilestone);
      });
    },
    [updateParams]
  );
  const setScope = useCallback(
    (nextScope: ScopeID) => {
      updateParams((next) => {
        if (nextScope === SCOPE_ALL) next.delete('scope');
        else next.set('scope', nextScope);
      });
    },
    [updateParams]
  );
  const setStateFilters = useCallback(
    (states: StateCategory[]) => {
      updateParams((next) => {
        next.delete('state_category');
        for (const state of states) next.append('state_category', state);
      });
    },
    [updateParams]
  );
  const setKindFilters = useCallback(
    (kinds: string[]) => {
      updateParams((next) => {
        next.delete('kind');
        for (const kind of kinds) next.append('kind', kind);
      });
    },
    [updateParams]
  );
  const setSearch = useCallback(
    (nextSearch: string) => {
      updateParams((next) => {
        if (nextSearch.trim()) next.set(GRAPH_SEARCH_PARAM, nextSearch);
        else next.delete(GRAPH_SEARCH_PARAM);
      });
    },
    [updateParams]
  );
  const clearFilters = useCallback(() => {
    updateParams((next) => {
      next.delete('milestone');
      next.delete('scope');
      next.delete('state_category');
      next.delete('kind');
      next.delete(GRAPH_SEARCH_PARAM);
    });
  }, [updateParams]);

  const filteredItems = useMemo(() => {
    let out = filterByScope(filterByMilestone(items, milestone), scope);
    if (stateFilters.length > 0) {
      const allowed = new Set(stateFilters);
      out = out.filter((it) => allowed.has(it.state_category));
    }
    if (kindFilters.length > 0) {
      const allowed = new Set(kindFilters);
      out = out.filter((it) => allowed.has(it.kind));
    }
    const needle = search.trim().toLowerCase();
    if (needle) {
      out = out.filter(
        (it) => it.id.toLowerCase().includes(needle) || it.title.toLowerCase().includes(needle)
      );
    }
    return out;
  }, [items, milestone, scope, stateFilters, kindFilters, search]);
  const filtersActive =
    milestone !== MILESTONE_ALL ||
    scope !== SCOPE_ALL ||
    stateFilters.length > 0 ||
    kindFilters.length > 0 ||
    search.trim().length > 0;
  const narrowCanvas = canvasWidth < NARROW_CANVAS_WIDTH;
  const effectiveGranularity = useMemo<'items' | 'epics'>(() => {
    if (granularityOverride) return granularity;
    return narrowCanvas || currentZoom < AUTO_GRANULARITY_ZOOM_THRESHOLD
      ? 'epics'
      : 'items';
  }, [granularity, granularityOverride, currentZoom, narrowCanvas]);
  const renderItems = useMemo(
    () => (effectiveGranularity === 'epics' ? deriveAggregatedItems(filteredItems) : filteredItems),
    [effectiveGranularity, filteredItems]
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

  useEffect(() => {
    const el = canvasHostRef.current;
    if (!el) return;
    const readWidth = () => {
      const next = el.clientWidth || el.getBoundingClientRect().width;
      setCanvasWidth(next || Number.POSITIVE_INFINITY);
    };
    readWidth();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', readWidth);
      return () => window.removeEventListener('resize', readWidth);
    }
    const observer = new ResizeObserver(readWidth);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // ReactFlow's `fitView` prop only fits once on mount. When the
  // initial render happens during the items-loading phase, ReactFlow
  // mounts with nodes=[] and parks its camera at the empty origin —
  // when items finally arrive the new nodes can land outside the
  // viewport and the canvas looks blank. Re-fit when the *raw* item
  // count first becomes non-zero (or changes between loads). gm-vubw:
  // we deliberately key off raw items, not graph.nodeIds — flipping
  // granularity changes the node count too, but it should preserve
  // the operator's zoom intent rather than re-fit.
  const lastFitCount = useRef(0);
  // gm-vubw: programmatic fitView fires onMove with no sourceEvent.
  // The flag lets onMove ignore the auto-fit's emitted zoom event so
  // currentZoom only tracks operator-initiated zoom/pan.
  const suppressMoveZoom = useRef(false);
  const fitOverview = useCallback(() => {
    const inst = instanceRef.current;
    if (!inst) return;
    if (narrowCanvas && graph.nodeIds.length > 0) {
      const centerFirstNode = () => {
        const node = inst.getNode(graph.nodeIds[0]);
        if (!node) return false;
        const w = node.width ?? 200;
        const h = node.height ?? 60;
        const zoom = Math.min(
          NARROW_CANVAS_OVERVIEW_ZOOM,
          Math.max(0.6, (canvasWidth - 16) / w)
        );
        suppressMoveZoom.current = true;
        void inst.setCenter(node.position.x + w / 2, node.position.y + h / 2, {
          zoom,
        });
        suppressMoveZoom.current = false;
        return true;
      };
      if (!centerFirstNode()) {
        requestAnimationFrame(() => {
          centerFirstNode();
        });
      }
      return;
    }
    suppressMoveZoom.current = true;
    void inst.fitView({ padding: 0.1 });
    suppressMoveZoom.current = false;
  }, [canvasWidth, graph.nodeIds, narrowCanvas]);

  useEffect(() => {
    const count = items.length;
    if (count > 0 && count !== lastFitCount.current) {
      const isFirstFit = lastFitCount.current === 0;
      lastFitCount.current = count;
      // Defer to the next paint so React Flow has translated the new
      // nodes into its internal store before we ask for a fit.
      const id = requestAnimationFrame(() => {
        const inst = instanceRef.current;
        if (!inst) return;
        fitOverview();
        // gm-vubw: capture the post-fit zoom on first fit so the
        // auto-granularity decision sees the real fitted zoom (the
        // 320-node workspace lands at ~0.02). On later fits we
        // already know the camera is bracketing the operator's view.
        if (isFirstFit) {
          const vp = inst.getViewport?.();
          if (vp && typeof vp.zoom === 'number') setCurrentZoom(vp.zoom);
        }
      });
      return () => cancelAnimationFrame(id);
    }
  }, [fitOverview, items.length]);

  useEffect(() => {
    if (items.length === 0 || granularityOverride) return;
    const id = requestAnimationFrame(() => fitOverview());
    return () => cancelAnimationFrame(id);
  }, [effectiveGranularity, fitOverview, granularityOverride, items.length]);

  const graphNodeSignature = graph.nodeIds.join('\u0000');
  useEffect(() => {
    if (items.length === 0) return;
    if (focusedId && !graph.nodeIds.includes(focusedId)) {
      setFocusedId(null);
    }
    if (graph.nodeIds.length === 0) return;
    const id = requestAnimationFrame(() => fitOverview());
    return () => cancelAnimationFrame(id);
  }, [fitOverview, focusedId, graph.nodeIds, graph.nodeIds.length, graphNodeSignature, items.length]);

  useEffect(() => {
    if (!narrowCanvas || items.length === 0 || graph.nodeIds.length === 0) return;
    const id = window.setTimeout(() => fitOverview(), 50);
    return () => window.clearTimeout(id);
  }, [effectiveGranularity, fitOverview, graph.nodeIds.length, items.length, narrowCanvas]);

  // gm-sfbh (post-RHP migration): mirror the legacy onClose behavior —
  // when the workitem detail tab transitions from open to closed
  // (via Escape, ×, or any other path) clear the focused-node marker
  // and re-fit the camera so the operator returns to the at-a-glance
  // view in one keystroke. Tracking the previous state (rather than
  // "absence == close") avoids racing with the registry: if the
  // 'workitem' kind hasn't been registered yet, `tabs` may briefly
  // omit the workitem entry on first paint even though `popDetail`
  // queued it; we only clear focus on the open → closed edge.
  const prevHasWorkItemDetailTabRef = useRef(false);
  useEffect(() => {
    const prev = prevHasWorkItemDetailTabRef.current;
    prevHasWorkItemDetailTabRef.current = hasWorkItemDetailTab;
    if (!prev) return;
    if (hasWorkItemDetailTab) return;
    if (!focusedId) return;
    setFocusedId(null);
    fitAll();
  }, [focusedId, hasWorkItemDetailTab, fitAll]);

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
    if (focusedId) popDetail({ kind: 'workitem', id: focusedId });
  }, [focusedId, popDetail]);

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
            Dependency graph across visible work. Click a node to drill in.
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <button
            type="button"
            onClick={stepBack}
            disabled={!canStepBack}
            data-testid="graph-step-back"
            title="Step up to predecessor (ArrowUp)"
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 transition-colors',
              'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
              'disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-white dark:disabled:hover:bg-neutral-900'
            )}
          >
            <ArrowUp className="h-3.5 w-3.5" />
            <span>Up</span>
          </button>
          <button
            type="button"
            onClick={stepNext}
            disabled={!canStepNext}
            data-testid="graph-step-next"
            title={
              canStepNext
                ? 'Step down to next node (ArrowDown)'
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
            <span>Down</span>
            <ArrowDown className="h-3.5 w-3.5" />
          </button>
          <div className="mx-1 h-4 w-px bg-neutral-200 dark:bg-neutral-800" />
          <GraphFilterMenu
            items={items}
            filteredCount={filteredItems.length}
            milestone={milestone}
            onChangeMilestone={setMilestone}
            scope={scope}
            onChangeScope={setScope}
            stateFilters={stateFilters}
            onChangeStateFilters={setStateFilters}
            kindFilters={kindFilters}
            onChangeKindFilters={setKindFilters}
            search={search}
            onChangeSearch={setSearch}
            filtersActive={filtersActive}
            onClear={clearFilters}
          />
          <ToggleButton
            active={effectiveGranularity === 'epics'}
            onClick={() => {
              // gm-vubw: any explicit click pins granularity — auto
              // mode resumes only via the Auto button below. The new
              // value flips off whatever is currently effective so the
              // operator's click always changes the view.
              setGranularity(effectiveGranularity === 'epics' ? 'items' : 'epics');
              setGranularityOverride(true);
            }}
            testid="graph-toggle-granularity"
            icon={<Layers className="h-3.5 w-3.5" />}
          >
            {effectiveGranularity === 'epics' ? 'Epics' : 'Items'}
          </ToggleButton>
          <ToggleButton
            active={!granularityOverride}
            onClick={() => setGranularityOverride(false)}
            testid="graph-toggle-granularity-auto"
            icon={<Wand2 className="h-3.5 w-3.5" />}
          >
            Auto
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
        ref={canvasHostRef}
        className="relative min-h-0 flex-1"
        data-testid="graph-canvas-host"
        data-focused-node={focusedId ?? undefined}
        data-granularity={effectiveGranularity}
        data-granularity-auto={granularityOverride ? undefined : 'true'}
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
        ) : filteredItems.length === 0 ? (
          <div
            className="m-8 rounded-md border border-dashed border-neutral-300 p-8 text-center text-sm text-neutral-500 dark:border-neutral-700"
            data-testid="graph-filtered-empty"
          >
            <p>No graph nodes match the current filters.</p>
            <button
              type="button"
              data-testid="graph-filtered-empty-clear"
              onClick={clearFilters}
              className="mt-3 rounded border border-neutral-300 bg-white px-3 py-1 text-xs text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800"
            >
              Clear filters
            </button>
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={NODE_TYPES}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            proOptions={{ hideAttribution: true }}
            onInit={(instance) => {
              instanceRef.current = instance;
            }}
            onNodeClick={(_evt, node) => {
              // gm-sfbh: focus camera on the clicked node, then pop
              // the RHP workitem detail tab. The two surfaces are
              // independent — the tab can be closed via the rail ×;
              // the focus persists until the operator clicks the canvas
              // background to clear it.
              // gm-e12.20: route through moveFocus so the click also
              // appends to the back-history that ArrowLeft consults.
              moveFocus(node.id);
              popDetail({ kind: 'workitem', id: node.id });
            }}
            onPaneClick={() => {
              // gm-sfbh: clearing the selection re-fits the camera
              // to the full graph and drops the focused-node marker.
              setFocusedId(null);
              fitAll();
            }}
            onNodeMouseEnter={(_evt, node) => setHoveredId(node.id)}
            onNodeMouseLeave={() => setHoveredId(null)}
            onMove={(_evt, viewport) => {
              if (suppressMoveZoom.current) return;
              setCurrentZoom(viewport.zoom);
            }}
            // 1000-node DoD: panOnScroll keeps the canvas responsive
            // when the graph is bigger than the viewport, and the
            // minimap gives the operator something to navigate by
            // without paying for a full layout pass per render.
            panOnScroll
            // A real workspace's layered layout (200–500 nodes) sprawls
            // far wider than 1.0×: without dropping minZoom below the
            // React Flow default (0.5) — and below the prior 0.1 that
            // still wasn't enough at 320 nodes — fitView clamps and
            // ends up framing a slice that doesn't include most nodes.
            // The operator-perceived symptom is "graph is blank"; the
            // actual symptom is "graph is way off-camera."
            minZoom={0.02}
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

    </div>
  );
}

interface GraphFilterMenuProps {
  items: WorkItem[];
  filteredCount: number;
  milestone: MilestoneID;
  onChangeMilestone: (next: MilestoneID) => void;
  scope: ScopeID;
  onChangeScope: (next: ScopeID) => void;
  stateFilters: StateCategory[];
  onChangeStateFilters: (next: StateCategory[]) => void;
  kindFilters: string[];
  onChangeKindFilters: (next: string[]) => void;
  search: string;
  onChangeSearch: (next: string) => void;
  filtersActive: boolean;
  onClear: () => void;
}

function GraphFilterMenu({
  items,
  filteredCount,
  milestone,
  onChangeMilestone,
  scope,
  onChangeScope,
  stateFilters,
  onChangeStateFilters,
  kindFilters,
  onChangeKindFilters,
  search,
  onChangeSearch,
  filtersActive,
  onClear,
}: GraphFilterMenuProps) {
  const [open, setOpen] = useState(false);
  const milestones = buildMilestoneOptions(items);
  const scopes = buildScopeOptions(items);
  const kinds = useMemo(() => {
    const set = new Set<string>();
    for (const item of items) set.add(item.kind);
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [items]);
  const activeCount =
    (milestone !== MILESTONE_ALL ? 1 : 0) +
    (scope !== SCOPE_ALL ? 1 : 0) +
    stateFilters.length +
    kindFilters.length +
    (search.trim() ? 1 : 0);
  const toggleState = (state: StateCategory) => {
    const next = stateFilters.includes(state)
      ? stateFilters.filter((s) => s !== state)
      : [...stateFilters, state];
    onChangeStateFilters(next);
  };
  const toggleKind = (kind: string) => {
    const next = kindFilters.includes(kind)
      ? kindFilters.filter((k) => k !== kind)
      : [...kindFilters, kind];
    onChangeKindFilters(next);
  };

  return (
    <div className="relative">
      <button
        type="button"
        data-testid="graph-filter-menu-button"
        aria-haspopup="menu"
        aria-expanded={open}
        title="Filters"
        onClick={() => setOpen((cur) => !cur)}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 transition-colors',
          open || filtersActive
            ? 'border-sky-700 bg-sky-50 text-sky-800 dark:border-sky-600 dark:bg-sky-950/50 dark:text-sky-100'
            : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
        )}
      >
        <Filter className="h-3.5 w-3.5" aria-hidden />
        <span>Filters</span>
        {activeCount > 0 ? (
          <span
            data-testid="graph-filter-menu-count"
            className="rounded-full bg-sky-700 px-1.5 text-[10px] font-medium leading-4 text-white dark:bg-sky-500 dark:text-sky-950"
          >
            {activeCount}
          </span>
        ) : null}
        <ChevronDown className="h-3 w-3" aria-hidden />
      </button>
      {open ? (
        <div
          data-testid="graph-filter-menu"
          role="menu"
          className={cn(
            'absolute right-0 z-30 mt-1 w-80 rounded-md border p-2 shadow-lg',
            'border-neutral-200 bg-white text-xs text-neutral-700',
            'dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-200'
          )}
        >
          <GraphMenuSection title="Scope">
            <GraphSelect
              label="Milestone"
              testid="graph-filter-milestone"
              value={milestone}
              onChange={onChangeMilestone}
              options={milestones.map((m) => ({ value: m.id, label: m.label }))}
            />
            <GraphSelect
              label="Epic"
              testid="graph-filter-scope"
              value={scope}
              onChange={onChangeScope}
              options={scopes.map((s) => ({
                value: s.id,
                label: `${s.depth === 1 ? '  ' : ''}${s.label}`,
              }))}
            />
          </GraphMenuSection>

          <GraphMenuSection title="Search">
            <input
              data-testid="graph-filter-search"
              value={search}
              onChange={(e) => onChangeSearch(e.target.value)}
              placeholder="Title or id"
              className="h-7 w-full rounded border border-neutral-300 bg-white px-2 text-xs text-neutral-700 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200"
            />
          </GraphMenuSection>

          <GraphMenuSection title="State">
            <div className="grid grid-cols-2 gap-1">
              {STATE_CATEGORIES.map((state) => (
                <GraphCheckOption
                  key={state}
                  active={stateFilters.includes(state)}
                  onClick={() => toggleState(state)}
                  label={state}
                  testid={`graph-filter-state-${state}`}
                />
              ))}
            </div>
          </GraphMenuSection>

          <GraphMenuSection title="Kind">
            {kinds.length > 0 ? (
              <div className="grid grid-cols-2 gap-1">
                {kinds.map((kind) => (
                  <GraphCheckOption
                    key={kind}
                    active={kindFilters.includes(kind)}
                    onClick={() => toggleKind(kind)}
                    label={kind}
                    testid={`graph-filter-kind-${kind}`}
                  />
                ))}
              </div>
            ) : (
              <p className="px-2 py-1 text-[11px] text-neutral-500">No kinds available.</p>
            )}
          </GraphMenuSection>

          <div className="flex items-center justify-between px-2 pt-2 text-[11px] text-neutral-500">
            <span data-testid="graph-filter-visible-count">{filteredCount} visible</span>
            <button
              type="button"
              data-testid="graph-filter-clear"
              disabled={!filtersActive}
              onClick={onClear}
              className="inline-flex items-center gap-1 rounded px-1.5 py-1 text-neutral-600 hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-40 dark:text-neutral-300 dark:hover:bg-neutral-900"
            >
              <RotateCcw className="h-3 w-3" aria-hidden />
              Clear
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function GraphMenuSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b border-neutral-100 py-2 last:border-0 dark:border-neutral-800">
      <h3 className="px-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-neutral-500">
        {title}
      </h3>
      <div className="space-y-1">{children}</div>
    </section>
  );
}

function GraphSelect({
  label,
  testid,
  value,
  onChange,
  options,
}: {
  label: string;
  testid: string;
  value: string;
  onChange: (next: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <label className="flex items-center gap-2 px-2 text-neutral-500">
      <span className="w-16 shrink-0">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-7 min-w-0 flex-1 rounded border border-neutral-300 bg-white px-1.5 text-xs text-neutral-700 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200"
        data-testid={testid}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function GraphCheckOption({
  active,
  onClick,
  label,
  testid,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  testid: string;
}) {
  return (
    <button
      type="button"
      role="menuitemcheckbox"
      aria-checked={active}
      data-testid={testid}
      data-active={active || undefined}
      onClick={onClick}
      className={cn(
        'flex items-center gap-1 rounded border px-2 py-1 text-left text-xs',
        active
          ? 'border-sky-700 bg-sky-700 text-white'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
      )}
    >
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {active ? <Check className="h-3 w-3 shrink-0" aria-hidden /> : null}
    </button>
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
