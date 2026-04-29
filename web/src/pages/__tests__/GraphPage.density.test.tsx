// GraphPage density tests (gm-vubw). Covers the auto-granularity
// behaviour that flips a 300+ node workspace from items → epics when
// the auto-fitted zoom lands below the readability threshold (0.3).
//
// The stub exposes a getViewport hook the page reads after fitView so
// the auto-flip can pick up the post-fit zoom; tests mutate the stub
// state and re-fire onInit to simulate ReactFlow's behaviour.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { WorkItem } from '@/types/core.gen';

// fittedZoom controls what getViewport() returns post-fitView. Tests
// set it BEFORE rendering so the first re-fit captures the chosen
// zoom and drives the auto-granularity decision.
let fittedZoom = 1;
// onMoveCb captures the page's onMove handler so tests can simulate
// the operator scrolling/zooming after the canvas has settled.
let onMoveCb:
  | ((evt: MouseEvent | null, viewport: { x: number; y: number; zoom: number }) => void)
  | null = null;

vi.mock('reactflow', async () => {
  type StubNode = { id: string; data?: { id: string; title: string } };
  type StubInstance = {
    fitView: () => void;
    setCenter: (x: number, y: number, opts?: unknown) => void;
    getNode: (id: string) => StubNode | undefined;
    getViewport: () => { x: number; y: number; zoom: number };
  };
  type Props = {
    nodes: StubNode[];
    edges: { id: string; source: string; target: string }[];
    onNodeClick?: (e: unknown, node: StubNode) => void;
    onInit?: (instance: StubInstance) => void;
    onMove?: (
      evt: MouseEvent | null,
      viewport: { x: number; y: number; zoom: number }
    ) => void;
    children?: ReactNode;
  };
  function ReactFlow({ nodes, edges, onNodeClick, onInit, onMove, children }: Props) {
    onMoveCb = onMove ?? null;
    const ref = (el: HTMLDivElement | null) => {
      if (el && onInit) {
        const inst: StubInstance = {
          fitView: () => undefined,
          setCenter: () => undefined,
          getNode: (id: string) => nodes.find((n) => n.id === id),
          getViewport: () => ({ x: 0, y: 0, zoom: fittedZoom }),
        };
        onInit(inst);
      }
    };
    return (
      <div data-testid="rf-stub" ref={ref}>
        <div data-testid="rf-stub-node-count">{nodes.length}</div>
        <div data-testid="rf-stub-edge-count">{edges.length}</div>
        {nodes.map((n) => (
          <button
            key={n.id}
            type="button"
            data-testid={`rf-stub-node-${n.id}`}
            onClick={(e) => onNodeClick?.(e, n)}
          >
            {n.data?.title ?? n.id}
          </button>
        ))}
        {children}
      </div>
    );
  }
  return {
    __esModule: true,
    default: ReactFlow,
    Background: () => null,
    BackgroundVariant: { Dots: 'dots' },
    Controls: () => null,
    MiniMap: () => null,
    Panel: ({ children }: { children: ReactNode }) => <>{children}</>,
    MarkerType: { ArrowClosed: 'arrowclosed' },
    Handle: () => null,
    Position: { Left: 'left', Right: 'right' },
  };
});

import { GraphPage } from '../GraphPage';
import { CapabilitiesProvider } from '@/capabilities';
import { HotkeysProvider } from '@/hotkeys';
import type { CapabilitiesResponse } from '@/capabilities';

function caps(): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'beads',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: false,
    },
    orchestration_plane: null,
  };
}

function wrapper(): (p: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }): JSX.Element {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps()}>
          <HotkeysProvider>
            <MemoryRouter>{children}</MemoryRouter>
          </HotkeysProvider>
        </CapabilitiesProvider>
      </QueryClientProvider>
    );
  };
}

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

function jsonResp(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

// makeLargeDataset builds a 300-item dataset where every item is a
// child of one of three Epics. Cross-Epic blocks edges keep the
// graph non-trivial. The shape mirrors what beads emits for a real
// gemba workspace.
function makeLargeDataset(): WorkItem[] {
  const items: WorkItem[] = [];
  const epicIds = ['e1', 'e2', 'e3'];
  for (const eid of epicIds) {
    items.push(wi(eid, { kind: 'epic' }));
  }
  for (let i = 0; i < 300; i++) {
    const epicId = epicIds[i % epicIds.length];
    items.push(
      wi(`t${i}`, {
        kind: 'task',
        relationships: [{ kind: 'parent_child', from: epicId, to: `t${i}` }],
      })
    );
  }
  return items;
}

describe('GraphPage density UX (gm-vubw)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    fittedZoom = 1;
    onMoveCb = null;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('auto-flips to epics when a 300-node workspace fits at low zoom', async () => {
    // Simulate the real workspace: 303 nodes auto-fit at zoom 0.02.
    fittedZoom = 0.02;
    fetchSpy.mockResolvedValueOnce(
      jsonResp({ items: makeLargeDataset(), total: 303 })
    );
    render(<GraphPage />, { wrapper: wrapper() });

    // Wait for items to load and render initially as items (303 nodes
    // before the auto-flip resolves on the next paint).
    await waitFor(() => {
      const count = parseInt(
        screen.getByTestId('rf-stub-node-count').textContent ?? '0',
        10
      );
      expect(count).toBeGreaterThan(0);
    });

    // After the post-fit zoom is captured, the page should re-render
    // with epic-aggregated nodes (3 epics → 3 nodes, no edges since
    // the dataset has no cross-Epic links).
    await waitFor(() => {
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('3');
    });
    expect(screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')).toBe(
      'epics'
    );
  });

  it('keeps items granularity when the auto-fit zoom is above threshold', async () => {
    // Small workspace fits comfortably: 5 items at zoom 1.
    fittedZoom = 1;
    fetchSpy.mockResolvedValueOnce(
      jsonResp({
        items: [wi('a'), wi('b'), wi('c'), wi('d'), wi('e')],
        total: 5,
      })
    );
    render(<GraphPage />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('5')
    );
    // The auto-fit zoom is 1.0 — well above 0.3 — so granularity
    // stays on items and the canvas shows all five nodes.
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')).toBe(
      'items'
    );
  });

  it('respects manual override — auto-low zoom does not flip to epics after operator pins items', async () => {
    fittedZoom = 0.02;
    fetchSpy.mockResolvedValueOnce(
      jsonResp({ items: makeLargeDataset(), total: 303 })
    );
    render(<GraphPage />, { wrapper: wrapper() });

    // Wait for the auto-flip to land on epics.
    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('3')
    );

    // Operator clicks the granularity toggle, pinning items.
    const toggle = screen.getByTestId('graph-toggle-granularity');
    act(() => {
      toggle.click();
    });
    expect(
      screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')
    ).toBe('items');
    expect(
      screen.getByTestId('graph-canvas-host').getAttribute('data-granularity-auto')
    ).toBeNull();

    // Simulate the operator panning at the same low zoom — auto
    // mode is off, so the view stays on items even though the
    // captured zoom (0.02) would otherwise force epics.
    act(() => {
      onMoveCb?.(new MouseEvent('mousemove'), { x: 0, y: 0, zoom: 0.02 });
    });
    expect(
      screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')
    ).toBe('items');
  });

  it('Auto button re-enables auto-granularity after a manual pin', async () => {
    fittedZoom = 0.02;
    fetchSpy.mockResolvedValueOnce(
      jsonResp({ items: makeLargeDataset(), total: 303 })
    );
    render(<GraphPage />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('3')
    );

    // Pin items via toggle.
    act(() => {
      screen.getByTestId('graph-toggle-granularity').click();
    });
    expect(
      screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')
    ).toBe('items');

    // Click Auto to resume auto-driven granularity.
    act(() => {
      screen.getByTestId('graph-toggle-granularity-auto').click();
    });
    // Captured zoom is still 0.02 → auto returns to epics.
    await waitFor(() =>
      expect(
        screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')
      ).toBe('epics')
    );
  });

  it('flips back to items when the operator zooms in past the threshold', async () => {
    fittedZoom = 0.02;
    fetchSpy.mockResolvedValueOnce(
      jsonResp({ items: makeLargeDataset(), total: 303 })
    );
    render(<GraphPage />, { wrapper: wrapper() });

    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('3')
    );

    // Operator scroll-zooms in past the 0.3 threshold while still
    // in auto mode. The onMove callback updates currentZoom and the
    // auto-decision flips back to items.
    act(() => {
      onMoveCb?.(new MouseEvent('wheel'), { x: 0, y: 0, zoom: 0.5 });
    });
    await waitFor(() =>
      expect(
        screen.getByTestId('graph-canvas-host').getAttribute('data-granularity')
      ).toBe('items')
    );
  });
});
