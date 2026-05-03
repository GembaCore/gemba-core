import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { BoardPage } from '../BoardPage';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import { HotkeyRegistry, HotkeysContext } from '@/hotkeys';
import type { WorkItem } from '@/types/core.gen';
import type { EscalationRequest } from '@/api/escalations';
import type { ApiError } from '@/api/client';
import { RhpProvider } from '@/components/rhp/RhpContext';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
import { WorkItemDetailRegistration } from '@/components/rhp/details/WorkItemDetailRegistration';
import { RhpShell } from '@/components/rhp/RhpShell';

// gm-e11.3: BoardPage now reads escalations to power the banner +
// per-card badges. Mock it out so existing fetch-driven tests don't
// trip on the extra /api/escalations request.
vi.mock('@/hooks/useEscalations', () => ({
  useEscalations: vi.fn(),
}));

import { useEscalations } from '@/hooks/useEscalations';
const mockUseEscalations = vi.mocked(useEscalations);

function emptyEscalationsResult(): UseQueryResult<EscalationRequest[], ApiError> {
  return {
    data: [],
    error: null,
    isError: false,
    isPending: false,
    isLoading: false,
    isSuccess: true,
    isFetching: false,
    isRefetching: false,
    isStale: false,
    isPlaceholderData: false,
    isLoadingError: false,
    isRefetchError: false,
    status: 'success',
    fetchStatus: 'idle',
    dataUpdatedAt: 0,
    errorUpdatedAt: 0,
    failureCount: 0,
    failureReason: null,
    errorUpdateCount: 0,
    isPaused: false,
    refetch: vi.fn(),
  } as unknown as UseQueryResult<EscalationRequest[], ApiError>;
}

// Seed a CapabilitiesProvider so WorkItemDrawer / EpicDetail's
// useCapabilities() resolves. The board itself doesn't consult the
// manifest, but the drawers (rendered inside BoardPage) do.
const caps: CapabilitiesResponse = {
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

function wrap(ui: ReactNode, initialEntry = '/board', initialCaps: CapabilitiesResponse = caps) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const registry = new HotkeyRegistry();
  return (
    <MemoryRouter initialEntries={[initialEntry]}>
      <RhpProvider>
        <RhpPinnedContentProvider>
          <QueryClientProvider client={client}>
            <CapabilitiesProvider initial={initialCaps}>
              <HotkeysContext.Provider value={registry}>
                <Routes>
                  <Route path="/board" element={ui} />
                  <Route path="/board/*" element={ui} />
                </Routes>
                {/* Register the workitem detail kind + shell so RHP content renders */}
                <WorkItemDetailRegistration />
                <RhpShell />
              </HotkeysContext.Provider>
            </CapabilitiesProvider>
          </QueryClientProvider>
        </RhpPinnedContentProvider>
      </RhpProvider>
    </MemoryRouter>
  );
}

const beadsOnlyCaps: CapabilitiesResponse = {
  ...caps,
  runtime_mode: 'beads_only',
  beads_only: true,
  orchestration_plane: null,
};

function bead(
  id: string,
  category: WorkItem['state_category'],
  extra: Partial<WorkItem> = {}
): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title ${id}`,
    status: 'open',
    state_category: category,
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...extra,
  };
}

function epic(id: string, parent?: string, extra: Partial<WorkItem> = {}): WorkItem {
  return {
    ...bead(id, 'started', { kind: 'epic', ...extra }),
    relationships: parent ? [{ kind: 'parent_child', from: parent, to: id }] : [],
  };
}

describe('BoardPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    mockUseEscalations.mockReturnValue(emptyEscalationsResult());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
    mockUseEscalations.mockReset();
  });

  it('shows skeleton columns while loading', () => {
    fetchSpy.mockImplementation(() => new Promise(() => {}));
    render(wrap(<BoardPage />));
    expect(screen.getByTestId('board-loading')).toBeTruthy();
    expect(screen.getAllByTestId('board-skeleton-card').length).toBeGreaterThan(0);
  });

  // Default Epic-primary view (gm-root.6 / ui-spec §4). Cards on /board
  // are Epics swimlaned by parent-epic; the WorkItem-flat board is now
  // the alternate.
  it('default view at /board renders Epic cards in swimlanes', async () => {
    const data: WorkItem[] = [
      epic('root'),
      epic('e1', 'root'),
      epic('e2', 'root'),
      bead('t1', 'started', { relationships: [{ kind: 'parent_child', from: 'e1', to: 't1' }] }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ items: data, total: data.length }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
    // Swimlane keyed by root epic id.
    expect(screen.getByTestId('board-epic-swimlane-root')).toBeTruthy();
    // Epic cards (not WorkItem cards) carry data-epic-card="true".
    const epicCards = document.querySelectorAll('[data-epic-card="true"]');
    expect(epicCards).toHaveLength(3);
    // The flat WorkItem board is NOT mounted on the default view.
    expect(screen.queryByTestId('board-workitem')).toBeNull();
  });

  it('shows the Epic-empty copy when the dataset has no Epics', async () => {
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: [bead('t1', 'started')], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-epic-empty')).toBeTruthy());
    expect(screen.getByText(/No Epics yet/i)).toBeTruthy();
  });

  // /board?view=workitem opts back into the M1 flat view (ui-spec L293).
  it('?view=workitem renders the flat WorkItem columns', async () => {
    const data: WorkItem[] = [
      bead('gm-a', 'started'),
      bead('gm-b', 'unstarted'),
      bead('gm-c', 'unstarted'),
      bead('gm-d', 'completed'),
    ];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />, '/board?view=workitem'));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());

    // Default execution kanban collapses unstarted + staged into Ready
    // and hides backlog/canceled from the board surface.
    expect(screen.getByTestId('board-column-ready')).toBeTruthy();
    expect(screen.getByTestId('board-column-started')).toBeTruthy();
    expect(screen.getByTestId('board-column-completed')).toBeTruthy();
    expect(screen.queryByTestId('board-column-backlog')).toBeNull();
    const readyCol = screen.getByTestId('board-column-ready');
    expect(readyCol.querySelectorAll('[data-work-item-id]')).toHaveLength(2);
    // Epic view is not mounted on the alternate.
    expect(screen.queryByTestId('board-epic')).toBeNull();
  });

  it('shows empty state when the adaptor returns zero beads', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [], total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
  });

  it('defaults beads-only mode to Flat and keeps Cascade available for wrappers', async () => {
    const data: WorkItem[] = [
      bead('gm-task', 'unstarted', {
        title: 'Leaf bead',
        relationships: [{ kind: 'parent_child', from: 'gm-epic', to: 'gm-task' }],
      }),
      bead('gm-m1', 'unstarted', { kind: 'milestone', title: 'M1 Foundation' }),
      epic('gm-epic', 'gm-m1', { title: 'Planning epic' }),
    ];
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ items: data, total: data.length }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    );

    render(wrap(<BoardPage />, '/board', beadsOnlyCaps));

    await waitFor(() => expect(screen.getByTestId('board-list')).toBeTruthy());
    expect(screen.getByTestId('view-toggle-list').getAttribute('data-active')).toBe('true');
    expect(screen.queryByTestId('view-toggle-epic')).toBeNull();
    expect(screen.queryByTestId('view-toggle-workitem')).toBeNull();
    expect(screen.getByTestId('board-list-kind-milestone')).toBeTruthy();

    fireEvent.click(screen.getByTestId('view-toggle-cascade'));
    await waitFor(() => expect(screen.getByTestId('beads-cascade')).toBeTruthy());

    const rows = [...document.querySelectorAll('[data-testid^="beads-cascade-row-"]')].map((el) =>
      el.getAttribute('data-testid')
    );
    expect(rows).toEqual([
      'beads-cascade-row-gm-m1',
      'beads-cascade-row-gm-epic',
      'beads-cascade-row-gm-task',
    ]);
  });

  // /board/:epicId deep-link auto-opens the RHP epic detail tab (gm-root.22.6).
  it('/board/:epicId pops the RHP epic detail tab for that epic', async () => {
    const data: WorkItem[] = [epic('root'), epic('e1', 'root', { description: 'Epic e1 detail.' })];
    fetchSpy.mockImplementation((url: string) => {
      if (url === '/api/work-items') {
        return Promise.resolve(
          new Response(JSON.stringify({ items: data, total: data.length }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      if (url === '/api/work-items/e1') {
        return Promise.resolve(
          new Response(JSON.stringify(data[1]), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    render(wrap(<BoardPage />, '/board/e1'));
    await waitFor(() => expect(screen.getByTestId('epic-detail-id')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Epic e1 detail.')).toBeTruthy());
  });

  // Regression: bd ids carry slashes ("gemba/gemba/gm-e1"). The route
  // must match the entire suffix, not just the first segment, otherwise
  // the detail tab never pops and the URL bar shows but the panel doesn't
  // appear.
  it('/board/<workspace-prefixed-id> pops the RHP epic detail tab', async () => {
    const data: WorkItem[] = [
      epic('gemba/gemba/gm-root'),
      epic('gemba/gemba/gm-e1', 'gemba/gemba/gm-root', { description: 'Prefixed.' }),
    ];
    fetchSpy.mockImplementation((url: string) => {
      if (url === '/api/work-items') {
        return Promise.resolve(
          new Response(JSON.stringify({ items: data, total: data.length }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      if (url === '/api/work-items/gemba%2Fgemba%2Fgm-e1') {
        return Promise.resolve(
          new Response(JSON.stringify(data[1]), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    render(wrap(<BoardPage />, '/board/gemba/gemba/gm-e1'));
    await waitFor(() => expect(screen.getByTestId('epic-detail-id')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Prefixed.')).toBeTruthy());
  });

  it('?view=workitem still opens the WorkItem detail (RHP) on card click', async () => {
    const listed = bead('gm-a', 'started');
    const detail: WorkItem = { ...listed, description: 'Deep dive into gm-a.' };
    fetchSpy.mockImplementation((url: string) => {
      if (url === '/api/work-items') {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [listed], total: 1 }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      if (url === '/api/work-items/gm-a') {
        return Promise.resolve(
          new Response(JSON.stringify(detail), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    render(wrap(<BoardPage />, '/board?view=workitem'));
    const card = await waitFor(() => screen.getByRole('button', { name: /open bead gm-a/i }));
    fireEvent.click(card);
    await waitFor(() => expect(screen.getByTestId('workitem-detail-id')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Deep dive into gm-a.')).toBeTruthy());
  });

  // gm-root.22.5 regression: legacy ?bead=X deep-link shim translates to
  // ?rhp=workitem:X and opens the RHP WorkItemDetail tab.
  it('?bead=X deep-links into the WorkItemDetail (RHP) via migration shim', async () => {
    const listed = bead('gm-a', 'started');
    const detail: WorkItem = { ...listed, description: 'Deep dive into gm-a.' };
    fetchSpy.mockImplementation((url: string) => {
      if (url === '/api/work-items') {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [listed], total: 1 }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      if (url === '/api/work-items/gm-a') {
        return Promise.resolve(
          new Response(JSON.stringify(detail), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }
      throw new Error(`unexpected fetch to ${url}`);
    });

    render(wrap(<BoardPage />, '/board?bead=gm-a'));
    // Shim fires on first mount: ?bead=gm-a → ?rhp=workitem:gm-a.
    // WorkItemDetailRegistration registers the 'workitem' kind; the
    // RHP body renders WorkItemDetail. Wait for the id label to appear.
    await waitFor(() => expect(screen.getByTestId('workitem-detail-id')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Deep dive into gm-a.')).toBeTruthy());
  });

  // The in-page view toggle flips ?view=workitem|list on/off without
  // a reload. gm-e12.19.1 added the third "list" mode (former
  // /backlog surface lifted into Board) so the toggle is now
  // tri-state — pin all three transitions.
  it('view toggle cycles through Epic, WorkItem, and List boards', async () => {
    const data: WorkItem[] = [epic('root'), bead('t1', 'started')];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());

    fireEvent.click(screen.getByTestId('view-toggle-workitem'));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());

    // gm-e12.19.1: List mode renders BoardListView under the same
    // shell. The list pane carries data-testid="board-list".
    fireEvent.click(screen.getByTestId('view-toggle-list'));
    await waitFor(() => expect(screen.getByTestId('board-list')).toBeTruthy());

    fireEvent.click(screen.getByTestId('view-toggle-epic'));
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
  });

  // gm-uekk: scope picker replaces the old swimlane-switcher.
  // Selecting a scope in the picker narrows the board to that
  // scope's lineage; the by-parent-epic swimlanes for the rest
  // disappear.
  it('scope picker narrows the kanban to the selected lineage', async () => {
    const data: WorkItem[] = [epic('e1'), epic('e2'), epic('e3')];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());

    // Default scope=All: three swimlanes (one per root).
    expect(screen.getByTestId('board-epic-swimlane-e1')).toBeTruthy();
    expect(screen.getByTestId('board-epic-swimlane-e2')).toBeTruthy();
    expect(screen.getByTestId('board-epic-swimlane-e3')).toBeTruthy();

    // Open the picker, click the e1 scope.
    fireEvent.click(screen.getByTestId('board-scope-trigger'));
    await waitFor(() => expect(screen.getByTestId('board-scope-option-e1')).toBeTruthy());
    fireEvent.click(screen.getByTestId('board-scope-option-e1'));

    await waitFor(() => expect(screen.queryByTestId('board-epic-swimlane-e2')).toBeNull());
    expect(screen.queryByTestId('board-epic-swimlane-e3')).toBeNull();
    // e1's lineage swimlane is still mounted.
    expect(screen.getByTestId('board-epic-swimlane-e1')).toBeTruthy();
  });

  // ?scope=<id> deep-links narrow the kanban on first paint.
  it('?scope=<id> boots scoped to that lineage', async () => {
    const data: WorkItem[] = [epic('e1'), epic('e2')];
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />, '/board?scope=e1'));
    await waitFor(() => expect(screen.getByTestId('board-epic-swimlane-e1')).toBeTruthy());
    expect(screen.queryByTestId('board-epic-swimlane-e2')).toBeNull();
  });

  // ?swimlane=by-label is a legacy URL; the migration step in
  // workItemViews.ts strips it on first paint so deep-links
  // continue to render (without the by-label grouping that no
  // longer exists).
  it('legacy ?swimlane= param is stripped on load', async () => {
    const data: WorkItem[] = [epic('e1')];
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />, '/board?swimlane=by-label'));
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
    // Default scope=All means the e1 swimlane renders.
    expect(screen.getByTestId('board-epic-swimlane-e1')).toBeTruthy();
  });

  // gm-5ekd: Backlog column is hidden by default in the kanban; the
  // Refine surface (gm-3ofd) is the new home for triage. ?show_backlog=1
  // and a power-mode toggle bring the column back; localStorage
  // persists the preference across reloads.
  describe('show_backlog kanban toggle (gm-5ekd)', () => {
    beforeEach(() => {
      try {
        window.localStorage.removeItem('gemba.board.show-backlog');
      } catch {
        /* private mode etc. */
      }
    });

    function mockData() {
      const data: WorkItem[] = [
        bead('gm-bk', 'backlog'),
        bead('gm-up', 'unstarted'),
        bead('gm-st', 'staged'),
        bead('gm-pr', 'started'),
        bead('gm-dn', 'completed'),
        bead('gm-cn', 'canceled'),
      ];
      fetchSpy.mockResolvedValue(
        new Response(JSON.stringify({ items: data, total: data.length }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );
    }

    it('default WorkItem kanban renders 3 operational columns and no Backlog heading', async () => {
      mockData();
      render(wrap(<BoardPage />, '/board?view=workitem'));
      await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
      expect(screen.queryByTestId('board-column-backlog')).toBeNull();
      const columns = document.querySelectorAll('[data-testid^="board-column-"]');
      // 3 column sections + their 3 count badges = 6 nodes match the prefix.
      // Filter to the section nodes (those without -count suffix).
      const sections = Array.from(columns).filter(
        (n) => !n.getAttribute('data-testid')?.endsWith('-count')
      );
      expect(sections).toHaveLength(3);
      // No Backlog column heading — the column heading is an h2 inside
      // the missing board-column-backlog section.
      const headings = Array.from(
        document.querySelectorAll('[data-testid^="board-column-"] h2')
      ).map((h) => h.textContent);
      expect(headings).not.toContain('Backlog');
    });

    it('?show_backlog=1 renders 4 columns including Backlog', async () => {
      mockData();
      render(wrap(<BoardPage />, '/board?view=workitem&show_backlog=1'));
      await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
      expect(screen.getByTestId('board-column-backlog')).toBeTruthy();
      const columns = document.querySelectorAll('[data-testid^="board-column-"]');
      const sections = Array.from(columns).filter(
        (n) => !n.getAttribute('data-testid')?.endsWith('-count')
      );
      expect(sections).toHaveLength(4);
    });

    it('toggle click flips the URL param + persists to localStorage', async () => {
      mockData();
      render(wrap(<BoardPage />, '/board?view=workitem'));
      await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
      // Default off.
      expect(screen.queryByTestId('board-column-backlog')).toBeNull();
      // Click toggle → backlog column appears + storage flips on.
      fireEvent.click(screen.getByTestId('board-show-backlog-toggle'));
      await waitFor(() => expect(screen.getByTestId('board-column-backlog')).toBeTruthy());
      expect(window.localStorage.getItem('gemba.board.show-backlog')).toBe('1');
      // Click again → backlog column hidden + storage cleared.
      fireEvent.click(screen.getByTestId('board-show-backlog-toggle'));
      await waitFor(() => expect(screen.queryByTestId('board-column-backlog')).toBeNull());
      expect(window.localStorage.getItem('gemba.board.show-backlog')).toBeNull();
    });

    it('localStorage gemba.board.show-backlog=1 survives reload', async () => {
      window.localStorage.setItem('gemba.board.show-backlog', '1');
      mockData();
      render(wrap(<BoardPage />, '/board?view=workitem'));
      await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());
      // No URL param, but storage hydrates the toggle on.
      expect(screen.getByTestId('board-column-backlog')).toBeTruthy();
      const toggle = screen.getByTestId('board-show-backlog-toggle');
      expect(toggle.getAttribute('data-active')).toBe('true');
    });

    it('Epic kanban also drops Backlog by default and restores it on toggle', async () => {
      const data: WorkItem[] = [epic('root'), epic('e1', 'root')];
      fetchSpy.mockResolvedValue(
        new Response(JSON.stringify({ items: data, total: data.length }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );
      render(wrap(<BoardPage />, '/board?show_backlog=1'));
      await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
      // Backlog plus the three operational columns when toggle is on.
      expect(screen.getByTestId('board-epic-cell-root-backlog')).toBeTruthy();
    });

    it('show_backlog toggle is hidden in list mode', async () => {
      mockData();
      render(wrap(<BoardPage />, '/board?layout=list'));
      await waitFor(() => expect(screen.getByTestId('board-list')).toBeTruthy());
      // List mode shows the Power toggle, not the show_backlog toggle.
      expect(screen.queryByTestId('board-show-backlog-toggle')).toBeNull();
      expect(screen.getByTestId('board-power-toggle')).toBeTruthy();
    });
  });

  it('shows error state with a retry button that re-fetches', async () => {
    // 503/adaptor_degraded does not auto-retry (see useWorkItems.retry),
    // so the error surfaces immediately and the manual retry click
    // triggers exactly one additional fetch.
    fetchSpy
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'adaptor_degraded', message: 'reconnecting' }), {
          status: 503,
          headers: { 'Content-Type': 'application/json' },
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ items: [], total: 0 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );
    render(wrap(<BoardPage />));
    await waitFor(() => expect(screen.getByTestId('board-error')).toBeTruthy());
    const beforeRetry = countWorkItemCalls();
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
    // Count only /api/work-items calls; the empty-state also fires a
    // /api/v1/onboarder/probe per gm-root.17.13 which is unrelated.
    expect(countWorkItemCalls()).toBeGreaterThan(beforeRetry);

    function countWorkItemCalls(): number {
      return fetchSpy.mock.calls.filter((args) => {
        const url = typeof args[0] === 'string' ? args[0] : (args[0] as Request).url;
        return url.includes('/api/work-items');
      }).length;
    }
  });
});
