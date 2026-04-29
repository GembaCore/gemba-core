// gm-l7hy MilestonePicker — pill + dropdown + Show button.

import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MilestonePicker } from '../MilestonePicker';
import { MILESTONE_ALL } from '../milestone';
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

describe('MilestonePicker', () => {
  it('renders nothing when there are no milestones', () => {
    const { container } = render(
      <MilestonePicker items={[]} value={MILESTONE_ALL} onChange={() => {}} />,
    );
    expect(container.querySelector('[data-testid="board-milestone-picker"]')).toBeNull();
  });

  it('renders trigger with "All" when value is MILESTONE_ALL', () => {
    const items = [milestone('m-1', 'M1 Beta')];
    render(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={() => {}} />);
    expect(screen.getByTestId('board-milestone-trigger').textContent).toContain('All');
  });

  it('lists All + every milestone in M-number order', () => {
    const items = [
      milestone('m-late', 'M5 Late'),
      milestone('m-early', 'M1 Early'),
      milestone('m-mid', 'M3 Mid'),
    ];
    render(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={() => {}} />);
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
    render(<MilestonePicker items={items} value={MILESTONE_ALL} onChange={onChange} />);
    fireEvent.click(screen.getByTestId('board-milestone-trigger'));
    fireEvent.click(screen.getByTestId('board-milestone-option-m-1'));
    expect(onChange).toHaveBeenCalledWith('m-1');
  });

  it('Show button calls onShow with the active milestone id', () => {
    const onShow = vi.fn();
    const items = [milestone('m-1', 'M1 Beta')];
    render(
      <MilestonePicker items={items} value="m-1" onChange={() => {}} onShow={onShow} />,
    );
    fireEvent.click(screen.getByTestId('board-milestone-show'));
    expect(onShow).toHaveBeenCalledWith('m-1');
  });

  it('Show button is hidden when value is MILESTONE_ALL', () => {
    const items = [milestone('m-1', 'M1 Beta')];
    render(
      <MilestonePicker
        items={items}
        value={MILESTONE_ALL}
        onChange={() => {}}
        onShow={() => {}}
      />,
    );
    expect(screen.queryByTestId('board-milestone-show')).toBeNull();
  });
});
