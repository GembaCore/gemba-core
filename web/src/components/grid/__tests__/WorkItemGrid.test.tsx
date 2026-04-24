// WorkItemGrid tests (gm-e12.3.1). Exercises the column visibility
// menu, row-click wiring, and confirms virtualization by asserting
// that only a bounded number of DOM rows exist for a large input set.

import { describe, expect, it, vi } from 'vitest';
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
});
