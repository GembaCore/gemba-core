// /refine surface tests (gm-3ofd). Covers the page-level wiring:
// hardcoded backlog filter, default sort, search box, empty state,
// row click → drawer.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { RefinePage } from '../RefinePage';
import { HotkeysProvider } from '@/hotkeys';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';

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

function wrap(url: string): (p: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return ({ children }) => (
    <QueryClientProvider client={client}>
      <CapabilitiesProvider initial={caps()}>
        <HotkeysProvider>
          <MemoryRouter initialEntries={[url]}>{children}</MemoryRouter>
        </HotkeysProvider>
      </CapabilitiesProvider>
    </QueryClientProvider>
  );
}

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'backlog',
    created_at: '2026-04-24T00:00:00Z',
    updated_at: '2026-04-24T00:00:00Z',
    ...patch,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('RefinePage', () => {
  const fetchSpy = vi.fn();
  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy.mockReset();
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('hardwires the backlog state filter on the API call', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    const url = String(fetchSpy.mock.calls[0]?.[0] ?? '');
    expect(url).toContain('state_category=backlog');
  });

  it('renders the empty state when the backlog is clean', async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ items: [], total: 0 }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() =>
      expect(screen.getByTestId('refine-empty')).toBeTruthy(),
    );
  });

  it('sorts by priority asc then created_at asc within priority', async () => {
    const items = [
      wi('gm-low-old', { priority: 3, created_at: '2026-01-01T00:00:00Z' }),
      wi('gm-hi-new', { priority: 1, created_at: '2026-04-01T00:00:00Z' }),
      wi('gm-hi-old', { priority: 1, created_at: '2026-02-01T00:00:00Z' }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText('title-gm-hi-old')).toBeTruthy());
    const cells = Array.from(document.querySelectorAll('td'))
      .map((c) => c.textContent ?? '')
      .filter((t) => t.startsWith('title-'));
    // First two are priority=1 (older first), last is priority=3.
    expect(cells.slice(0, 3)).toEqual([
      'title-gm-hi-old',
      'title-gm-hi-new',
      'title-gm-low-old',
    ]);
  });

  it('surfaces the four refine columns with derived values (gm-51i2)', async () => {
    const items = [
      wi('gm-a', {
        title: 'old item with epic + blockers + dispatch',
        // ~5 days old relative to current date is fine; we just need a chip rendered.
        created_at: '2026-04-24T00:00:00Z',
        custom: { 'gemba.suggested_epic': 'gm-epic-1' },
        relationships: [
          { kind: 'blocks', from: 'gm-z', to: 'gm-a' },
          { kind: 'blocks', from: 'gm-y', to: 'gm-a' },
        ],
        dispatch_status: 'awaiting-review',
      }),
      wi('gm-b', {
        title: 'plain item',
        created_at: '2026-04-28T00:00:00Z',
      }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText(/plain item/)).toBeTruthy());
    // Suggested-epic chip on row a.
    expect(screen.getByTestId('grid-cell-suggested-epic-gm-a').textContent).toContain('gm-epic-1');
    expect(screen.getByTestId('grid-cell-suggested-epic-gm-b').textContent).toBe('—');
    // Blockers chip on row a (count = 2); row b shows the dim placeholder.
    expect(screen.getByTestId('grid-cell-blockers-gm-a').textContent).toBe('2');
    expect(screen.getByTestId('grid-cell-blockers-gm-b').textContent).toBe('—');
    // Dispatch chip on row a only — ready / empty are suppressed.
    expect(screen.getByTestId('grid-cell-dispatch-gm-a').textContent).toContain('awaiting-review');
    expect(screen.getByTestId('grid-cell-dispatch-gm-b').textContent).toBe('—');
    // Age columns rendered for both rows.
    expect(screen.getByTestId('grid-cell-age-gm-a')).toBeTruthy();
    expect(screen.getByTestId('grid-cell-age-gm-b')).toBeTruthy();
  });

  it('clicking the blockers header sorts numerically (gm-51i2)', async () => {
    const items = [
      wi('gm-zero', { title: 'zero blockers' }),
      wi('gm-three', {
        title: 'three blockers',
        relationships: [
          { kind: 'blocks', from: 'a', to: 'gm-three' },
          { kind: 'blocks', from: 'b', to: 'gm-three' },
          { kind: 'blocks', from: 'c', to: 'gm-three' },
        ],
      }),
      wi('gm-one', {
        title: 'one blocker',
        relationships: [{ kind: 'blocks', from: 'a', to: 'gm-one' }],
      }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByTestId('grid-sort-blockers')).toBeTruthy());
    // First click — tanstack defaults numeric columns to desc-first, so
    // most-blocked rises to the top.
    fireEvent.click(screen.getByTestId('grid-sort-blockers'));
    await waitFor(() => {
      const titles = Array.from(document.querySelectorAll('td'))
        .map((c) => c.textContent ?? '')
        .filter((t) => t.endsWith('blocker') || t.endsWith('blockers'));
      expect(titles.slice(0, 3)).toEqual([
        'three blockers',
        'one blocker',
        'zero blockers',
      ]);
    });
    // Second click flips to ascending.
    fireEvent.click(screen.getByTestId('grid-sort-blockers'));
    await waitFor(() => {
      const titles = Array.from(document.querySelectorAll('td'))
        .map((c) => c.textContent ?? '')
        .filter((t) => t.endsWith('blocker') || t.endsWith('blockers'));
      expect(titles.slice(0, 3)).toEqual([
        'zero blockers',
        'one blocker',
        'three blockers',
      ]);
    });
  });

  it('clicking the age header sorts by underlying timestamp (gm-51i2)', async () => {
    const items = [
      wi('gm-newest', { title: 'newest', created_at: '2026-04-28T00:00:00Z' }),
      wi('gm-oldest', { title: 'oldest', created_at: '2025-01-01T00:00:00Z' }),
      wi('gm-mid', { title: 'mid', created_at: '2026-01-01T00:00:00Z' }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByTestId('grid-sort-age')).toBeTruthy());
    // Click — ascending by timestamp puts oldest first.
    fireEvent.click(screen.getByTestId('grid-sort-age'));
    await waitFor(() => {
      const titles = Array.from(document.querySelectorAll('td'))
        .map((c) => c.textContent ?? '')
        .filter((t) => ['newest', 'oldest', 'mid'].includes(t));
      expect(titles.slice(0, 3)).toEqual(['oldest', 'mid', 'newest']);
    });
  });

  it('search input narrows by title and updates the URL', async () => {
    const items = [
      wi('gm-a', { title: 'fix login regression' }),
      wi('gm-b', { title: 'redesign empty board' }),
    ];
    fetchSpy.mockResolvedValue(jsonResponse({ items, total: items.length }));
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText(/fix login/)).toBeTruthy());
    fireEvent.change(screen.getByTestId('refine-search'), {
      target: { value: 'login' },
    });
    await waitFor(() => expect(screen.queryByText(/redesign/)).toBeNull());
    expect(screen.getByText(/fix login/)).toBeTruthy();
  });
});

// Drop-into-epic action (gm-ju5o). Single-row + bulk paths share the
// same EpicPickerDialog. The fetch spy routes by URL: backlog GET,
// epics GET (kind=epic), PATCH writes.
describe('RefinePage drop-into-epic (gm-ju5o)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy.mockReset();
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => vi.unstubAllGlobals());

  // routeFetch returns { backlog, epics } payloads + records PATCHes.
  // Tests pass a backlog set and an epic set; PATCH ids are inferred
  // from the URL.
  function routeFetch(opts: { backlog: WorkItem[]; epics: WorkItem[] }) {
    fetchSpy.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url);
      if (init?.method === 'PATCH') {
        return Promise.resolve(jsonResponse({}));
      }
      if (u.includes('kind=epic')) {
        return Promise.resolve(
          jsonResponse({ items: opts.epics, total: opts.epics.length }),
        );
      }
      // default = backlog list
      return Promise.resolve(
        jsonResponse({ items: opts.backlog, total: opts.backlog.length }),
      );
    });
  }

  function patchCalls(): Array<{ url: string; body: Record<string, unknown> }> {
    return fetchSpy.mock.calls
      .filter(([, init]) => (init as RequestInit | undefined)?.method === 'PATCH')
      .map(([url, init]) => ({
        url: String(url),
        body: JSON.parse(String((init as RequestInit).body)),
      }));
  }

  it('opens the epic picker from the bulk-action button and re-parents every selected row', async () => {
    const backlog = [
      wi('gm-a', { title: 'first task' }),
      wi('gm-b', { title: 'second task' }),
      wi('gm-c', { title: 'third task' }),
    ];
    const epics = [
      wi('gm-epic-x', { kind: 'epic', title: 'Epic X', state_category: 'unstarted' }),
      wi('gm-epic-y', { kind: 'epic', title: 'Epic Y', state_category: 'unstarted' }),
    ];
    routeFetch({ backlog, epics });

    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByTestId('grid-row-gm-a')).toBeTruthy());

    // Multi-select two rows via the leading checkbox column.
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-a'));
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-b'));
    await waitFor(() =>
      expect(screen.getByTestId('grid-selection-count').textContent).toContain('2'),
    );

    fireEvent.click(screen.getByTestId('grid-bulk-drop-into-epic'));
    await waitFor(() => expect(screen.getByTestId('epic-picker-dialog')).toBeTruthy());

    // Picker lists both epics, none of the backlog tasks.
    await waitFor(() =>
      expect(screen.getByTestId('epic-picker-option-gm-epic-x')).toBeTruthy(),
    );
    expect(screen.getByTestId('epic-picker-option-gm-epic-y')).toBeTruthy();
    expect(screen.queryByTestId('epic-picker-option-gm-a')).toBeNull();
    expect(screen.queryByTestId('epic-picker-option-gm-b')).toBeNull();

    fireEvent.click(screen.getByTestId('epic-picker-option-gm-epic-x'));

    // Two PATCHes — one per selected row — both setting parent_id to the picked epic.
    await waitFor(() => expect(patchCalls().length).toBe(2));
    const patches = patchCalls();
    const ids = patches.map((p) => p.url.split('/').pop()).sort();
    expect(ids).toEqual(['gm-a', 'gm-b']);
    for (const p of patches) {
      expect(p.body).toEqual({ parent_id: 'gm-epic-x' });
    }
    // Dialog closes after pick.
    await waitFor(() =>
      expect(screen.queryByTestId('epic-picker-dialog')).toBeNull(),
    );
  });

  it('right-click on a single row opens the picker and re-parents that one id', async () => {
    const backlog = [wi('gm-only', { title: 'only task' })];
    const epics = [wi('gm-epic-x', { kind: 'epic', title: 'Epic X', state_category: 'unstarted' })];
    routeFetch({ backlog, epics });

    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByTestId('grid-row-gm-only')).toBeTruthy());

    // Right-click the row → grid auto-selects-only and opens its menu.
    fireEvent.contextMenu(screen.getByTestId('grid-row-gm-only'));
    await waitFor(() => expect(screen.getByTestId('grid-context-menu')).toBeTruthy());
    fireEvent.click(screen.getByTestId('grid-context-drop-into-epic'));
    await waitFor(() => expect(screen.getByTestId('epic-picker-dialog')).toBeTruthy());

    await waitFor(() =>
      expect(screen.getByTestId('epic-picker-option-gm-epic-x')).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId('epic-picker-option-gm-epic-x'));
    await waitFor(() => expect(patchCalls().length).toBe(1));
    const [only] = patchCalls();
    expect(only.url).toContain('/work-items/gm-only');
    expect(only.body).toEqual({ parent_id: 'gm-epic-x' });
  });

  it('picker lists every epic in the dataset and excludes non-epics', async () => {
    const backlog = [wi('gm-a')];
    const epics = [
      wi('gm-epic-1', { kind: 'epic', title: 'Alpha', state_category: 'unstarted' }),
      wi('gm-epic-2', { kind: 'epic', title: 'Beta', state_category: 'unstarted' }),
      wi('gm-epic-3', { kind: 'epic', title: 'Gamma', state_category: 'unstarted' }),
    ];
    routeFetch({ backlog, epics });

    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByTestId('grid-row-gm-a')).toBeTruthy());

    // Open the picker via right-click → context menu (the simplest path
    // that doesn't require multi-select setup).
    fireEvent.contextMenu(screen.getByTestId('grid-row-gm-a'));
    fireEvent.click(screen.getByTestId('grid-context-drop-into-epic'));
    await waitFor(() => expect(screen.getByTestId('epic-picker-dialog')).toBeTruthy());

    await waitFor(() =>
      expect(screen.getByTestId('epic-picker-option-gm-epic-1')).toBeTruthy(),
    );
    const opts = Array.from(
      screen.getByTestId('epic-picker-options').querySelectorAll('[role="option"]'),
    ).map((el) => el.getAttribute('data-testid'));
    expect(opts).toEqual([
      'epic-picker-option-gm-epic-1',
      'epic-picker-option-gm-epic-2',
      'epic-picker-option-gm-epic-3',
    ]);
    // Backlog (non-epic) row is NOT offered as a target.
    expect(screen.queryByTestId('epic-picker-option-gm-a')).toBeNull();
  });
});
describe('RefinePage defer / dismiss actions', () => {
  const fetchSpy = vi.fn();
  beforeEach(() => {
    window.localStorage.clear();
    fetchSpy.mockReset();
    vi.stubGlobal('fetch', fetchSpy);
  });
  afterEach(() => vi.unstubAllGlobals());

  // installRoutes wires fetchSpy to return the supplied list payload on
  // GET /work-items and a stub WorkItem on PATCH; PATCH calls are
  // recorded for assertions on payload shape.
  function installRoutes(items: WorkItem[]) {
    fetchSpy.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (init?.method === 'PATCH') {
        return Promise.resolve(jsonResponse(items[0] ?? wi('gm-x')));
      }
      if (url.includes('/work-items')) {
        return Promise.resolve(jsonResponse({ items, total: items.length }));
      }
      return Promise.resolve(jsonResponse({}));
    });
  }

  function patchCalls(): Array<{ id: string; body: Record<string, unknown> }> {
    return fetchSpy.mock.calls
      .filter(([, init]) => (init as RequestInit | undefined)?.method === 'PATCH')
      .map(([url, init]) => {
        const u = String(url);
        const id = u.slice(u.lastIndexOf('/') + 1);
        const body = JSON.parse((init as RequestInit).body as string);
        return { id, body };
      });
  }

  it('defer popover writes defer-until:<iso> label and strips prior defer-until labels', async () => {
    const items = [
      wi('gm-1', {
        labels: ['area:web', 'defer-until:2026-01-01', 'priority-band:next'],
      }),
    ];
    installRoutes(items);
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText('title-gm-1')).toBeTruthy());
    // Check the row to drive selection through the grid.
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-1'));
    // Bulk Defer button → opens popover.
    fireEvent.click(screen.getByTestId('grid-bulk-defer'));
    fireEvent.change(screen.getByTestId('refine-defer-date'), {
      target: { value: '2026-05-15' },
    });
    fireEvent.click(screen.getByTestId('refine-defer-confirm'));
    await waitFor(() => expect(patchCalls().length).toBe(1));
    const [{ id, body }] = patchCalls();
    expect(id).toBe('gm-1');
    // Existing defer-until:* stripped, every other label preserved,
    // new defer-until:<iso> appended.
    expect(body).toEqual({
      labels: ['area:web', 'priority-band:next', 'defer-until:2026-05-15'],
    });
  });

  it('defer with empty date is a no-op (no PATCH fires)', async () => {
    const items = [wi('gm-1', { labels: [] })];
    installRoutes(items);
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText('title-gm-1')).toBeTruthy());
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-1'));
    fireEvent.click(screen.getByTestId('grid-bulk-defer'));
    // Confirm with empty date.
    fireEvent.click(screen.getByTestId('refine-defer-confirm'));
    // Settle event loop, then assert no PATCH fired.
    await new Promise((r) => setTimeout(r, 10));
    expect(patchCalls().length).toBe(0);
  });

  it('dismiss popover patches state_category=canceled (reason TODO)', async () => {
    const items = [wi('gm-1', { state_category: 'backlog' })];
    installRoutes(items);
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText('title-gm-1')).toBeTruthy());
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-1'));
    fireEvent.click(screen.getByTestId('grid-bulk-dismiss'));
    fireEvent.change(screen.getByTestId('refine-dismiss-reason'), {
      target: { value: 'duplicate of gm-2' },
    });
    fireEvent.click(screen.getByTestId('refine-dismiss-confirm'));
    await waitFor(() => expect(patchCalls().length).toBe(1));
    const [{ id, body }] = patchCalls();
    expect(id).toBe('gm-1');
    // WorkItemPatch has no notes-append field today; we ship the
    // state-change without the reason. Adjust this assertion when an
    // append-notes path lands on the patch surface.
    expect(body).toEqual({ state_category: 'canceled' });
  });

  it('bulk defer applies the same defer-until label across every selected id', async () => {
    const items = [
      wi('gm-1', { labels: ['x'] }),
      wi('gm-2', { labels: ['defer-until:2026-01-01', 'y'] }),
    ];
    installRoutes(items);
    const W = wrap('/refine');
    render(
      <W>
        <RefinePage />
      </W>,
    );
    await waitFor(() => expect(screen.getByText('title-gm-1')).toBeTruthy());
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-1'));
    fireEvent.click(screen.getByTestId('grid-row-checkbox-gm-2'));
    fireEvent.click(screen.getByTestId('grid-bulk-defer'));
    fireEvent.change(screen.getByTestId('refine-defer-date'), {
      target: { value: '2026-06-01' },
    });
    fireEvent.click(screen.getByTestId('refine-defer-confirm'));
    await waitFor(() => expect(patchCalls().length).toBe(2));
    const calls = patchCalls();
    const byId = Object.fromEntries(calls.map((c) => [c.id, c.body]));
    expect(byId['gm-1']).toEqual({ labels: ['x', 'defer-until:2026-06-01'] });
    expect(byId['gm-2']).toEqual({ labels: ['y', 'defer-until:2026-06-01'] });
  });
});
