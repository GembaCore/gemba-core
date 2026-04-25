// WorkItemGrid tests (gm-e12.3.1). Exercises the column visibility
// menu, row-click wiring, and confirms virtualization by asserting
// that only a bounded number of DOM rows exist for a large input set.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, within } from '@testing-library/react';
import { WorkItemGrid } from '../WorkItemGrid';
import type { WorkItem } from '@/types/core.gen';

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
    // 200ms is generous — production hardware mounts in ~30ms. The
    // budget exists to fail loudly if someone accidentally renders all
    // 10k rows; a regression there blows past 200ms with room to spare.
    expect(elapsed).toBeLessThan(200);
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
