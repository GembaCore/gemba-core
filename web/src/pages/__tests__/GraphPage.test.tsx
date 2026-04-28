// GraphPage smoke tests (gm-e12.16). Heavy lifting is in the
// graphAnalysis / buildGraph unit tests; here we mount the page with
// a mocked ReactFlow (jsdom can't run the real renderer — no
// ResizeObserver, no compositor) and verify the wire-up: data flows
// from useWorkItems → buildGraph → the legend & node list, the
// toggles flip the highlight state, and clicking a node opens the
// drawer.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { WorkItem } from '@/types/core.gen';

// Mock reactflow before importing GraphPage so the page sees the
// stub. The stub renders one DOM marker per node + invokes the
// onNodeClick prop when our test code calls it; everything else
// (Background, Controls, MiniMap, Panel) becomes a passthrough.
// fitViewSpy is mutated by the stub's onInit callback so tests can
// assert the late-arrival fitView path (the bug behind "graph view
// appears blank" — ReactFlow's `fitView` prop only fits once on mount,
// so when items arrive after first render the camera was parked on
// nodes=[]. GraphPage now calls fitView() on the captured instance
// when the node count first becomes non-zero).
const fitViewSpy = vi.fn();

vi.mock('reactflow', async () => {
  type StubNode = { id: string; data?: { id: string; title: string } };
  type StubInstance = {
    fitView: () => void;
    setCenter: (x: number, y: number, opts?: unknown) => void;
    getNode: (id: string) => StubNode | undefined;
  };
  type Props = {
    nodes: StubNode[];
    edges: { id: string; source: string; target: string }[];
    onNodeClick?: (e: unknown, node: StubNode) => void;
    onInit?: (instance: StubInstance) => void;
    children?: ReactNode;
  };
  function ReactFlow({ nodes, edges, onNodeClick, onInit, children }: Props) {
    const ref = (el: HTMLDivElement | null) => {
      if (el && onInit) {
        const inst: StubInstance = {
          fitView: () => fitViewSpy(),
          setCenter: () => undefined,
          getNode: (id: string) => nodes.find((n) => n.id === id),
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

function caps(extDeclared = false): CapabilitiesResponse {
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
      edge_extensions: extDeclared
        ? [{ name: 'discovered_from', directed: true }]
        : undefined,
    },
    orchestration_plane: null,
  };
}

function wrapper(extDeclared = false): (p: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }): JSX.Element {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps(extDeclared)}>
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

function jsonResp(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('GraphPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
    fitViewSpy.mockReset();
  });

  // Regression: with `fitView` declared as a ReactFlow prop only, the
  // camera fits once on mount. When items load AFTER mount, ReactFlow
  // mounted with nodes=[] and the camera parked on the empty origin —
  // when 320 nodes finally arrived they rendered offscreen and the
  // canvas looked blank. GraphPage now imperatively calls fitView()
  // when the node count first becomes non-zero.
  it('re-fits the camera when work items load after mount', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResp({ items: [wi('a'), wi('b'), wi('c')], total: 3 })
    );
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('3')
    );
    // requestAnimationFrame is jsdom-stubbed via setTimeout; flush.
    await waitFor(() => expect(fitViewSpy).toHaveBeenCalled());
  });

  it('renders an empty-state when the WorkPlane has nothing to draw', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResp({ items: [], total: 0 }));
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() =>
      expect(screen.getByText(/No work items/i)).toBeTruthy()
    );
  });

  it('mounts ReactFlow with one node per WorkItem and core edges only by default', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResp({
        items: [
          wi('a', { relationships: [{ kind: 'blocks', from: 'a', to: 'b' }] }),
          wi('b'),
        ],
        total: 2,
      })
    );
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-node-count').textContent).toBe('2')
    );
    expect(screen.getByTestId('rf-stub-edge-count').textContent).toBe('1');
  });

  it('renders extension edges when the manifest declares them', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResp({
        items: [
          wi('a', {
            custom: {
              'beads:dependencies': [
                { type: 'discovered_from', from_bead_id: 'a', to_bead_id: 'b' },
              ],
            },
          }),
          wi('b'),
        ],
        total: 2,
      })
    );
    render(<GraphPage />, { wrapper: wrapper(/* extDeclared */ true) });
    await waitFor(() =>
      expect(screen.getByTestId('rf-stub-edge-count').textContent).toBe('1')
    );
  });

  it('flips the cycle highlight off and on via the toggle', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResp({ items: [wi('a'), wi('b')], total: 2 })
    );
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('rf-stub')).toBeTruthy());
    const toggle = screen.getByTestId('graph-toggle-cycles');
    expect(toggle.getAttribute('data-active')).toBe('true');
    act(() => {
      toggle.click();
    });
    expect(toggle.getAttribute('data-active')).toBeNull();
    act(() => {
      toggle.click();
    });
    expect(toggle.getAttribute('data-active')).toBe('true');
  });

  it('flags cycles in the legend when a back-edge is present', async () => {
    fetchSpy.mockResolvedValueOnce(
      jsonResp({
        items: [
          wi('a', { relationships: [{ kind: 'blocks', from: 'a', to: 'b' }] }),
          wi('b', { relationships: [{ kind: 'blocks', from: 'b', to: 'a' }] }),
        ],
        total: 2,
      })
    );
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() =>
      expect(screen.getByTestId('graph-legend-cycles')).toBeTruthy()
    );
    expect(screen.getByTestId('graph-legend-cycles').textContent).toMatch(/1 cycle/);
  });

  it('clicking a node opens the WorkItem drawer', async () => {
    fetchSpy.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/work-items/')) {
        return jsonResp(wi('a', { description: 'detail' }));
      }
      return jsonResp({ items: [wi('a')], total: 1 });
    });
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('rf-stub-node-a')).toBeTruthy());
    act(() => {
      screen.getByTestId('rf-stub-node-a').click();
    });
    // The drawer's container test id pattern lives in WorkItemDrawer;
    // settle on any rendered drawer marker so this test stays
    // resilient to drawer layout tweaks.
    await waitFor(() => {
      const drawer = document.querySelector('[role="dialog"]');
      expect(drawer).toBeTruthy();
    });
  });
});
