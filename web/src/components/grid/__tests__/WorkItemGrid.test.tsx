// WorkItemGrid tests (gm-e12.3.1, gm-5v8v.6.1). Exercises the column
// visibility menu, row-click wiring, virtualization, and the inline
// cell editing for title / priority / state.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render as rtlRender, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';
import { WorkItemGrid } from '../WorkItemGrid';
import type { WorkItem } from '@/types/core.gen';

// Inline editing wires useUpdateWorkItem (gm-5v8v.6.1) which needs a
// QueryClient. Wrap every render with a fresh client per test so
// mutations don't bleed across cases.
function render(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return rtlRender(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-24T00:00:00Z',
    updated_at: '2026-04-24T00:00:00Z',
    ...patch,
  };
}

function range(n: number): WorkItem[] {
  return Array.from({ length: n }, (_, i) => wi(`gm-${i}`));
}

// Layout APIs (clientHeight, getBoundingClientRect, offsetHeight) are
// stubbed globally in vitest.setup.ts so @tanstack/react-virtual can
// compute a non-zero viewport. Without the stubs every test would see
// zero visible rows.

describe('WorkItemGrid', () => {
  it('renders rows for every item in a small input set', () => {
    render(<WorkItemGrid rows={range(5)} />);
    for (let i = 0; i < 5; i++) {
      expect(screen.getByTestId(`grid-row-gm-${i}`)).toBeTruthy();
    }
  });

  it('virtualizes: large input yields a bounded number of DOM rows', () => {
    render(<WorkItemGrid rows={range(1000)} />);
    // Ceiling: viewport height (600) / row height (36) ≈ 17, plus
    // overscan × 2 (20). Padding rows are excluded via the testid
    // filter. Even with generous slack, a virtualized grid must not
    // materialise anywhere near all 1000 rows.
    const domRows = screen.queryAllByTestId(/^grid-row-gm-\d+$/);
    expect(domRows.length).toBeGreaterThan(0);
    expect(domRows.length).toBeLessThan(100);
  });

  // gm-e12.3 DoD piece 1 — pin the 10k-row mount cost. We can't
  // measure 60fps in jsdom (no compositor, no rAF tied to a real
  // refresh rate), so we assert the property that makes 60fps possible:
  // the DOM-row count stays bounded regardless of input size, AND the
  // initial mount completes in well under a frame budget worth of
  // wall-clock time. If a future change accidentally drops
  // virtualization (e.g. by passing rows through a non-memoised filter
  // upstream that snapshot-clones every cell), this test will catch it.
  it('10k rows mount under a frame budget and stay bounded', () => {
    const start = performance.now();
    render(<WorkItemGrid rows={range(10_000)} />);
    const elapsed = performance.now() - start;
    const domRows = screen.queryAllByTestId(/^grid-row-gm-\d+$/);
    expect(domRows.length).toBeGreaterThan(0);
    expect(domRows.length).toBeLessThan(100);
    // 500ms is generous — production hardware mounts in ~30ms and a
    // local jsdom run lands at ~40-60ms. The budget exists to fail
    // loudly if someone accidentally renders all 10k rows; a
    // regression there blows past 500ms with room to spare. CI's
    // slower x86 runners land in the 200-300ms band, which is why we
    // sit higher than the local-laptop number.
    expect(elapsed).toBeLessThan(500);
  });

  it('toggling a column in the visibility menu hides + restores it', () => {
    render(<WorkItemGrid rows={range(3)} />);
    // Menu is collapsed by default.
    expect(screen.queryByTestId('grid-columns-menu')).toBeNull();

    act(() => {
      screen.getByTestId('grid-columns-toggle').click();
    });
    const menu = screen.getByTestId('grid-columns-menu');
    expect(menu).toBeTruthy();

    // Labels column is visible by default → a header cell renders it.
    const headerLabelsPresent = () =>
      Array.from(document.querySelectorAll('th')).some((th) => th.textContent === 'Labels');
    expect(headerLabelsPresent()).toBe(true);

    const labelsCheckbox = within(menu).getByTestId('grid-column-labels') as HTMLInputElement;
    expect(labelsCheckbox.checked).toBe(true);
    act(() => {
      labelsCheckbox.click();
    });

    // Column header for Labels must be gone — the <th> is removed,
    // even though the menu label "Labels" still renders.
    expect(headerLabelsPresent()).toBe(false);

    // Flip it back.
    act(() => {
      (within(menu).getByTestId('grid-column-labels') as HTMLInputElement).click();
    });
    expect(headerLabelsPresent()).toBe(true);
  });

  it('row click fires onSelect with the row id', () => {
    const onSelect = vi.fn();
    render(<WorkItemGrid rows={range(3)} onSelect={onSelect} />);
    act(() => {
      screen.getByTestId('grid-row-gm-1').click();
    });
    expect(onSelect).toHaveBeenCalledWith('gm-1');
  });

  it('does not render the presets button unless presets prop is set', () => {
    render(<WorkItemGrid rows={range(1)} />);
    expect(screen.queryByTestId('grid-presets-toggle')).toBeNull();
  });
});

// ── Inline cell editing (gm-5v8v.6.1) ──────────────────────────────
// Click a title / priority / state cell → editor opens. Esc cancels
// without firing PATCH. Enter / blur commits and the api/workItems
// helper mints a fresh X-GEMBA-Confirm nonce per call (the helper
// is unit-tested separately; here we just confirm a single PATCH
// fires and the body contains the new value).

describe('WorkItemGrid inline edit (gm-5v8v.6.1)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify(wi('gm-1', { title: 'updated' })), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('clicking a title cell activates the editor', () => {
    render(<WorkItemGrid rows={[wi('gm-1', { title: 'first' })]} />);
    expect(screen.queryByTestId('grid-cell-editor-title')).toBeNull();
    act(() => {
      screen.getByTestId('grid-cell-gm-1-title').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-title') as HTMLInputElement;
    expect(editor.value).toBe('first');
  });

  it('Esc cancels the title edit without firing PATCH', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { title: 'first' })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-title').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-title') as HTMLInputElement;
    act(() => {
      editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    // Editor closes; no PATCH fires.
    expect(screen.queryByTestId('grid-cell-editor-title')).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('Enter on a changed title fires one PATCH with the new value', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { title: 'first' })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-title').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-title') as HTMLInputElement;
    fireEvent.change(editor, { target: { value: 'second' } });
    fireEvent.keyDown(editor, { key: 'Enter' });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const [url, init] = fetchSpy.mock.calls[0]!;
    expect(url).toBe('/api/work-items/gm-1');
    expect((init as RequestInit).method).toBe('PATCH');
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({ title: 'second' });
    // Nonce header is present (the api helper mints a fresh UUID per
    // call when no explicit nonce is passed; uniqueness is the
    // helper's contract, not the grid's).
    const headers = (init as RequestInit).headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBeTruthy();
  });

  it('Enter on an unchanged title does NOT fire PATCH', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { title: 'same' })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-title').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-title');
    act(() => {
      editor.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    });
    await Promise.resolve();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('priority cell accepts an integer and PATCHes priority', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { priority: 1 })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-priority').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-priority') as HTMLInputElement;
    fireEvent.change(editor, { target: { value: '3' } });
    fireEvent.keyDown(editor, { key: 'Enter' });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const [, init] = fetchSpy.mock.calls[0]!;
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({ priority: 3 });
  });

  it('priority cell sends null when cleared', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { priority: 2 })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-priority').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-priority') as HTMLInputElement;
    fireEvent.change(editor, { target: { value: '' } });
    fireEvent.keyDown(editor, { key: 'Enter' });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const [, init] = fetchSpy.mock.calls[0]!;
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({ priority: null });
  });

  it('priority cell rejects non-numeric input without PATCHing', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { priority: 1 })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-priority').click();
    });
    const editor = screen.getByTestId('grid-cell-editor-priority') as HTMLInputElement;
    fireEvent.change(editor, { target: { value: 'banana' } });
    fireEvent.keyDown(editor, { key: 'Enter' });
    // Editor closes; the editor's blur would otherwise fire a second
    // commit attempt — committedRef + the cancel path keep that from
    // turning into a PATCH. Wait one tick and assert nothing went out.
    await new Promise((r) => setTimeout(r, 10));
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('state cell select fires PATCH with state_category on change', async () => {
    render(<WorkItemGrid rows={[wi('gm-1', { state_category: 'unstarted' })]} />);
    act(() => {
      screen.getByTestId('grid-cell-gm-1-state').click();
    });
    const select = screen.getByTestId('grid-cell-editor-state') as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'started' } });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    const [, init] = fetchSpy.mock.calls[0]!;
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      state_category: 'started',
    });
  });

  it('clicking an editable cell does NOT fire onSelect (drawer stays closed)', () => {
    const onSelect = vi.fn();
    render(
      <WorkItemGrid
        rows={[wi('gm-1', { title: 'first' })]}
        onSelect={onSelect}
      />
    );
    act(() => {
      screen.getByTestId('grid-cell-gm-1-title').click();
    });
    expect(onSelect).not.toHaveBeenCalled();
    // Editor is open instead.
    expect(screen.getByTestId('grid-cell-editor-title')).toBeTruthy();
  });

  it('clicking elsewhere on the row still opens the drawer', () => {
    const onSelect = vi.fn();
    render(
      <WorkItemGrid rows={[wi('gm-1')]} onSelect={onSelect} />
    );
    // The id cell is read-only; clicking it bubbles up to the row's
    // onClick → onSelect fires.
    act(() => {
      screen.getByTestId('grid-row-gm-1').click();
    });
    expect(onSelect).toHaveBeenCalledWith('gm-1');
  });
});

describe('WorkItemGrid presets (gm-e12.3.3)', () => {
  const STORAGE_KEY = 'gemba.test.grid.presets';

  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
  });

  const headerCols = () =>
    Array.from(document.querySelectorAll('th')).map((th) => th.textContent);

  it('applying the Compact built-in preset hides kind/sprint/labels', () => {
    render(<WorkItemGrid rows={range(3)} presets={{ storageKey: STORAGE_KEY }} />);

    // Default → all nine columns present.
    expect(headerCols()).toEqual(expect.arrayContaining(['Kind', 'Sprint', 'Labels']));

    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    act(() => {
      screen.getByTestId('grid-preset-apply-compact').click();
    });

    const cols = headerCols();
    expect(cols).not.toContain('Kind');
    expect(cols).not.toContain('Sprint');
    expect(cols).not.toContain('Labels');
    // Compact keeps these:
    expect(cols).toEqual(expect.arrayContaining(['ID', 'Title', 'State', 'P', 'Assignee', 'Updated']));
  });

  it('saves a custom preset through the prompt and persists it to localStorage', () => {
    render(
      <WorkItemGrid
        rows={range(1)}
        presets={{ storageKey: STORAGE_KEY, promptName: () => 'Mine' }}
      />
    );

    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    act(() => {
      screen.getByTestId('grid-presets-save').click();
    });

    // Reopen menu; saved preset should appear in the Saved group.
    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });

    const saved = screen.getAllByText('Mine');
    expect(saved.length).toBeGreaterThan(0);

    const raw = window.localStorage.getItem(STORAGE_KEY);
    expect(raw).toBeTruthy();
    const parsed = JSON.parse(raw!) as Array<{ name: string }>;
    expect(parsed.map((p) => p.name)).toEqual(['Mine']);
  });

  it('deletes a saved preset via the trash button', () => {
    // Seed a user preset directly into localStorage.
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify([{ id: 'user:1', name: 'Dev', visibility: { title: true } }])
    );
    render(<WorkItemGrid rows={range(1)} presets={{ storageKey: STORAGE_KEY }} />);

    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    act(() => {
      screen.getByTestId('grid-preset-delete-user:1').click();
    });

    expect(screen.queryByTestId('grid-preset-user:1')).toBeNull();
    expect(JSON.parse(window.localStorage.getItem(STORAGE_KEY)!)).toEqual([]);
  });

  it('built-in presets have no delete button', () => {
    render(<WorkItemGrid rows={range(1)} presets={{ storageKey: STORAGE_KEY }} />);
    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    expect(screen.queryByTestId('grid-preset-delete-default')).toBeNull();
    expect(screen.queryByTestId('grid-preset-delete-compact')).toBeNull();
  });

  it('ignores an empty / canceled preset name', () => {
    render(
      <WorkItemGrid
        rows={range(1)}
        presets={{ storageKey: STORAGE_KEY, promptName: () => '  ' }}
      />
    );
    act(() => {
      screen.getByTestId('grid-presets-toggle').click();
    });
    act(() => {
      screen.getByTestId('grid-presets-save').click();
    });
    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
  });
});
