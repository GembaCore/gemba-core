import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import { BoardPage } from '../BoardPage';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import { HotkeyRegistry, HotkeysContext } from '@/hotkeys';
import { STATE_CATEGORIES, type WorkItem } from '@/types/core.gen';

// Seed a CapabilitiesProvider so WorkItemDrawer / EpicDrawer's
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

function wrap(ui: ReactNode, initialEntry = '/board') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const registry = new HotkeyRegistry();
  return (
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>
          <HotkeysContext.Provider value={registry}>
            <Routes>
              <Route path="/board" element={ui} />
              <Route path="/board/*" element={ui} />
            </Routes>
          </HotkeysContext.Provider>
        </CapabilitiesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

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
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
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
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
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
    fetchSpy.mockResolvedValueOnce(
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
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: data, total: data.length }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    render(wrap(<BoardPage />, '/board?view=workitem'));
    await waitFor(() => expect(screen.getByTestId('board-workitem')).toBeTruthy());

    for (const cat of STATE_CATEGORIES) {
      expect(screen.getByTestId(`board-column-${cat}`)).toBeTruthy();
    }
    const unstartedCol = screen.getByTestId('board-column-unstarted');
    expect(unstartedCol.querySelectorAll('[data-work-item-id]')).toHaveLength(2);
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

  // /board/:epicId deep-link auto-opens the Epic drawer (ui-spec L116).
  it('/board/:epicId mounts the EpicDrawer for that epic', async () => {
    const data: WorkItem[] = [
      epic('root'),
      epic('e1', 'root', { description: 'Epic e1 detail.' }),
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
    await waitFor(() => expect(screen.getByTestId('epic-drawer-content')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Epic e1 detail.')).toBeTruthy());
  });

  // Regression: bd ids carry slashes ("gemba/gemba/gm-e1"). The route
  // must match the entire suffix, not just the first segment, otherwise
  // the drawer never mounts and the URL bar shows but the panel doesn't
  // appear.
  it('/board/<workspace-prefixed-id> opens the EpicDrawer', async () => {
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
    await waitFor(() => expect(screen.getByTestId('epic-drawer-content')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Prefixed.')).toBeTruthy());
  });

  it('?view=workitem still opens the WorkItem drawer on card click', async () => {
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
    await waitFor(() => expect(screen.getByTestId('work-item-drawer-content')).toBeTruthy());
    await waitFor(() => expect(screen.getByText('Deep dive into gm-a.')).toBeTruthy());
  });

  // The in-page view toggle flips ?view=workitem on/off without a reload.
  it('view toggle switches between Epic and WorkItem boards', async () => {
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

    fireEvent.click(screen.getByTestId('view-toggle-epic'));
    await waitFor(() => expect(screen.getByTestId('board-epic')).toBeTruthy());
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
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => expect(screen.getByTestId('board-empty')).toBeTruthy());
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });
});
