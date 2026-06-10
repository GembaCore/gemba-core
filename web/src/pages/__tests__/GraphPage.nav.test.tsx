// GraphPage keyboard-navigation tests (gm-e12.20). Verifies the
// next-when-deterministic / always-back traversal contract by driving
// the page through clicks, ArrowUp/ArrowDown, and Enter, then
// asserting that the focused-node marker (data-focused-node on the
// canvas host) tracks the predicted path.
//
// Same ReactFlow stub as GraphPage.test.tsx — jsdom can't run the real
// canvas. The stub only renders one button per node and invokes
// onNodeClick; the focus marker we assert against lives on the host
// div, not inside the React Flow viewport, so it survives stubbing.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { WorkItem } from '@/types/core.gen';

vi.mock('reactflow', async () => {
  type StubNode = { id: string; data?: { id: string; title: string } };
  type Props = {
    nodes: StubNode[];
    edges: { id: string; source: string; target: string }[];
    onNodeClick?: (e: unknown, node: StubNode) => void;
    children?: ReactNode;
  };
  function ReactFlow({ nodes, edges, onNodeClick, children }: Props) {
    return (
      <div data-testid="rf-stub">
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
import { RhpProvider } from '@/components/rhp/RhpContext';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
import type { CapabilitiesResponse } from '@/capabilities';

function caps(): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'fake',
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
      <MemoryRouter>
        <RhpProvider>
          <RhpPinnedContentProvider>
            <QueryClientProvider client={client}>
              <CapabilitiesProvider initial={caps()}>
                <HotkeysProvider>{children}</HotkeysProvider>
              </CapabilitiesProvider>
            </QueryClientProvider>
          </RhpPinnedContentProvider>
        </RhpProvider>
      </MemoryRouter>
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

function focusedId(): string | null {
  const host = screen.getByTestId('graph-canvas-host');
  return host.getAttribute('data-focused-node');
}

function press(key: string) {
  // The HotkeysProvider listens on window keydown; fireEvent on the
  // body doesn't bubble cleanly because the provider attaches at the
  // window level. Dispatch directly on window to mirror reality.
  act(() => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));
  });
}

describe('GraphPage keyboard navigation (gm-e12.20)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // Chain A → B → C: B has one outgoing (C) and one incoming (A);
  // C has one incoming (B); A has one outgoing (B). Every step is
  // unambiguous so ArrowDown / ArrowUp can walk the whole chain.
  function renderChain() {
    fetchSpy.mockResolvedValueOnce(
      jsonResp({
        items: [
          wi('a', { relationships: [{ kind: 'blocks', from: 'a', to: 'b' }] }),
          wi('b', { relationships: [{ kind: 'blocks', from: 'b', to: 'c' }] }),
          wi('c'),
        ],
        total: 3,
      })
    );
    render(<GraphPage />, { wrapper: wrapper() });
  }

  it('ArrowDown steps forward through a single-successor chain', async () => {
    renderChain();
    await waitFor(() => expect(screen.getByTestId('rf-stub-node-a')).toBeTruthy());

    act(() => {
      screen.getByTestId('rf-stub-node-a').click();
    });
    expect(focusedId()).toBe('a');

    press('ArrowDown');
    await waitFor(() => expect(focusedId()).toBe('b'));

    press('ArrowDown');
    await waitFor(() => expect(focusedId()).toBe('c'));

    // C has no outgoing structural edge; ArrowDown is a no-op.
    press('ArrowDown');
    expect(focusedId()).toBe('c');
  });

  it('ArrowUp steps back to the predecessor', async () => {
    renderChain();
    await waitFor(() => expect(screen.getByTestId('rf-stub-node-a')).toBeTruthy());

    act(() => {
      screen.getByTestId('rf-stub-node-c').click();
    });
    expect(focusedId()).toBe('c');

    press('ArrowUp');
    await waitFor(() => expect(focusedId()).toBe('b'));

    press('ArrowUp');
    await waitFor(() => expect(focusedId()).toBe('a'));

    // A has no predecessor; ArrowUp is a no-op.
    press('ArrowUp');
    expect(focusedId()).toBe('a');
  });

  it('Next button is disabled when the focused node has multiple successors', async () => {
    // A blocks B and C — two successors → ambiguous, no next.
    fetchSpy.mockResolvedValueOnce(
      jsonResp({
        items: [
          wi('a', {
            relationships: [
              { kind: 'blocks', from: 'a', to: 'b' },
              { kind: 'blocks', from: 'a', to: 'c' },
            ],
          }),
          wi('b'),
          wi('c'),
        ],
        total: 3,
      })
    );
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('rf-stub-node-a')).toBeTruthy());

    act(() => {
      screen.getByTestId('rf-stub-node-a').click();
    });
    expect(focusedId()).toBe('a');

    const nextBtn = screen.getByTestId('graph-step-next') as HTMLButtonElement;
    expect(nextBtn.disabled).toBe(true);

    // ArrowDown on an ambiguous node is also a no-op — focus stays
    // on A.
    press('ArrowDown');
    expect(focusedId()).toBe('a');
  });

  it('ArrowUp prefers the most-recently-visited predecessor when ambiguous', async () => {
    // Both A and X block C. Visiting A then C makes A the most-
    // recent predecessor of C, so ArrowUp from C lands on A
    // (not the alphabetic min, which would be A here too — so we
    // also assert the X-visit-first variant separately).
    fetchSpy.mockResolvedValue(
      jsonResp({
        items: [
          wi('a', { relationships: [{ kind: 'blocks', from: 'a', to: 'c' }] }),
          wi('x', { relationships: [{ kind: 'blocks', from: 'x', to: 'c' }] }),
          wi('c'),
        ],
        total: 3,
      })
    );
    render(<GraphPage />, { wrapper: wrapper() });
    await waitFor(() => expect(screen.getByTestId('rf-stub-node-c')).toBeTruthy());

    // Visit X, then C — last visit before C is X, so ArrowUp
    // should pick X.
    act(() => {
      screen.getByTestId('rf-stub-node-x').click();
    });
    act(() => {
      screen.getByTestId('rf-stub-node-c').click();
    });
    expect(focusedId()).toBe('c');

    press('ArrowUp');
    await waitFor(() => expect(focusedId()).toBe('x'));
  });

  it('Enter is a no-op when no node has been focused', async () => {
    // Guard rail: the openFocused handler must early-return when
    // focusedId is null so an operator who lands on /graph and hits
    // Enter doesn't trigger an undefined-id drawer fetch.
    renderChain();
    await waitFor(() => expect(screen.getByTestId('rf-stub-node-a')).toBeTruthy());

    expect(focusedId()).toBeNull();
    press('Enter');
    // No drawer should have opened — the click-driven path is
    // covered by the existing GraphPage.test.tsx "clicking a node
    // opens the WorkItem drawer" test, so Enter on a focused node
    // is just a re-bind of the same setOpenId(focusedId) call.
    expect(screen.queryByTestId('work-item-drawer-content')).toBeNull();
  });
});
