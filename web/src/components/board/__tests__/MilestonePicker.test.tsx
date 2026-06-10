// gm-l7hy MilestonePicker — pill + dropdown + Show button.
// gm-935r: option rows are also drop targets; the picker uses
// useDndMonitor so renders need to be wrapped in a DndContext.

import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen, type RenderResult } from '@testing-library/react';
import { DndContext, type DragEndEvent } from '@dnd-kit/core';
import { MilestonePicker } from '../MilestonePicker';
import { MILESTONE_ALL } from '../milestone';
import { milestoneOptionDroppableId } from '../dragToReparent';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';

const COMMON = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function milestone(id: string, title: string): WorkItem {
  return { ...COMMON, id, title, kind: KIND_MILESTONE, relationships: [] };
}

function renderInDnd(
  ui: React.ReactElement,
  onDragEnd?: (e: DragEndEvent) => void,
): RenderResult {
  return render(<DndContext onDragEnd={onDragEnd}>{ui}</DndContext>);
}

describe('MilestonePicker', () => {
  it('renders nothing when there are no milestones', () => {
    const { container } = renderInDnd(
      <MilestonePicker items={[]} value={MILESTONE_ALL} onChange={() => {}} />,
    );
    expect(container.querySelector('[data-testid="board-milestone-picker"]')).toBeNull();
  });

  it('renders trigger with "All" when value is MILESTONE_ALL', () => {
    const items = [milestone('m-1', 'M1 Beta')];
    renderInDnd(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={() => {}} />);
    expect(screen.getByTestId('board-milestone-trigger').textContent).toContain('All');
  });

  it('lists All + every milestone in M-number order', () => {
    const items = [
      milestone('m-late', 'M5 Late'),
      milestone('m-early', 'M1 Early'),
      milestone('m-mid', 'M3 Mid'),
    ];
    renderInDnd(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={() => {}} />);
    fireEvent.click(screen.getByTestId('board-milestone-trigger'));

    const dropdown = screen.getByTestId('board-milestone-dropdown');
    const ids = Array.from(dropdown.querySelectorAll('[role="option"]')).map(
      (el) => el.getAttribute('data-testid'),
    );
    expect(ids).toEqual([
      'board-milestone-option-all',
      'board-milestone-option-m-early',
      'board-milestone-option-m-mid',
      'board-milestone-option-m-late',
    ]);
  });

  it('clicking an option calls onChange with the milestone id', () => {
    const onChange = vi.fn();
    const items = [milestone('m-1', 'M1 Beta')];
    renderInDnd(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={onChange} />);
    fireEvent.click(screen.getByTestId('board-milestone-trigger'));
    fireEvent.click(screen.getByTestId('board-milestone-option-m-1'));
    expect(onChange).toHaveBeenCalledWith('m-1');
  });

  it('Show button calls onShow with the active milestone id', () => {
    const onShow = vi.fn();
    const items = [milestone('m-1', 'M1 Beta')];
    renderInDnd(
      <MilestonePicker items={items} value="m-1" onChange={() => {}} onShow={onShow} />,
    );
    fireEvent.click(screen.getByTestId('board-milestone-show'));
    expect(onShow).toHaveBeenCalledWith('m-1');
  });

  it('Show button is hidden when value is MILESTONE_ALL', () => {
    const items = [milestone('m-1', 'M1 Beta')];
    renderInDnd(
      <MilestonePicker
        items={items}
        value={MILESTONE_ALL}
        onChange={() => {}}
        onShow={() => {}}
      />,
    );
    expect(screen.queryByTestId('board-milestone-show')).toBeNull();
  });

  it('option rows are registered with milestone-option droppable ids', () => {
    // gm-935r: a sentinel test — the picker exposes useDroppable on
    // each option row so the BoardPage drag handler can recognise the
    // drop target. Verifies the row carries the expected testid and
    // the encoded droppable id matches dragToReparent's encoding.
    const items = [milestone('m-1', 'M1 Beta')];
    renderInDnd(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={() => {}} />);
    fireEvent.click(screen.getByTestId('board-milestone-trigger'));
    expect(screen.getByTestId('board-milestone-option-all')).toBeTruthy();
    expect(screen.getByTestId('board-milestone-option-m-1')).toBeTruthy();
    expect(milestoneOptionDroppableId('m-1')).toBe('milestone-option|m-1');
    expect(milestoneOptionDroppableId(MILESTONE_ALL)).toBe('milestone-option|all');
  });
});
