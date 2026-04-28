// gm-uekk ScopePicker — pill + dropdown.

import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ScopePicker } from '../ScopePicker';
import { SCOPE_ALL } from '../scope';
import type { WorkItem } from '@/types/core.gen';

const COMMON = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function epic(id: string, title: string, parent?: string): WorkItem {
  return {
    ...COMMON,
    id,
    kind: 'epic',
    title,
    relationships: parent ? [{ kind: 'parent_child', from: parent, to: id }] : [],
  };
}

describe('ScopePicker', () => {
  it('renders trigger pill with "All" by default', () => {
    render(<ScopePicker items={[]} value={SCOPE_ALL} onChange={() => {}} />);
    const trigger = screen.getByTestId('board-scope-trigger');
    expect(trigger.textContent).toContain('All');
  });

  it('opens dropdown on click and lists All + roots + child epics indented', () => {
    const items = [
      epic('root-a', 'Token spending'),
      epic('child-a-1', 'Sprint budget', 'root-a'),
      epic('root-b', 'Personas'),
    ];
    render(<ScopePicker items={items} value={SCOPE_ALL} onChange={() => {}} />);
    fireEvent.click(screen.getByTestId('board-scope-trigger'));

    expect(screen.getByTestId('board-scope-option-all')).toBeTruthy();
    expect(screen.getByTestId('board-scope-option-root-a')).toBeTruthy();
    expect(screen.getByTestId('board-scope-option-child-a-1')).toBeTruthy();
    expect(screen.getByTestId('board-scope-option-root-b')).toBeTruthy();
    // Child option is rendered indented (depth=1 → pl-7 class).
    const childRow = screen.getByTestId('board-scope-option-child-a-1');
    expect(childRow.className).toContain('pl-7');
  });

  it('clicking an option calls onChange with the option id', () => {
    const onChange = vi.fn();
    const items = [epic('root-a', 'Token spending')];
    render(<ScopePicker items={items} value={SCOPE_ALL} onChange={onChange} />);
    fireEvent.click(screen.getByTestId('board-scope-trigger'));
    fireEvent.click(screen.getByTestId('board-scope-option-root-a'));
    expect(onChange).toHaveBeenCalledWith('root-a');
  });

  it('trigger label reflects the active scope when not All', () => {
    const items = [epic('root-a', 'Token spending')];
    render(<ScopePicker items={items} value="root-a" onChange={() => {}} />);
    const trigger = screen.getByTestId('board-scope-trigger');
    expect(trigger.textContent).toContain('Token spending');
  });

  it('shows the empty-state hint when there are no epics', () => {
    render(<ScopePicker items={[]} value={SCOPE_ALL} onChange={() => {}} />);
    fireEvent.click(screen.getByTestId('board-scope-trigger'));
    expect(screen.getByText(/No epics yet/)).toBeTruthy();
  });
});
