// TabBar tests (gm-e12.19.7).
//
// Coverage:
//   - tabs render with active highlighted
//   - clicking a tab calls onChange(id)
//   - clicking a disabled tab is a no-op
//   - count chip renders when present, omitted when 0/undefined
//   - rightSlot renders at the right
//   - testid lands on the container

import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LayoutGrid, ListTodo, CalendarRange, Network } from 'lucide-react';
import { TabBar } from '../TabBar';

describe('TabBar', () => {
  const baseTabs = [
    { id: 'board', label: 'Board', icon: LayoutGrid },
    { id: 'list', label: 'List', icon: ListTodo },
    { id: 'sprints', label: 'Sprints', icon: CalendarRange, count: 3 },
    { id: 'graph', label: 'Graph', icon: Network, disabled: true },
  ];

  it('renders the tab list with the active tab marked aria-selected', () => {
    const onChange = vi.fn();
    render(
      <TabBar value="list" onChange={onChange} tabs={baseTabs} testid="plan-tabs" />
    );
    expect(screen.getByTestId('plan-tabs')).toBeTruthy();
    const board = screen.getByTestId('plan-tabs-board');
    const list = screen.getByTestId('plan-tabs-list');
    expect(board.getAttribute('aria-selected')).toBe('false');
    expect(list.getAttribute('aria-selected')).toBe('true');
  });

  it('calls onChange(id) when a tab is clicked', () => {
    const onChange = vi.fn();
    render(
      <TabBar value="board" onChange={onChange} tabs={baseTabs} testid="plan-tabs" />
    );
    fireEvent.click(screen.getByTestId('plan-tabs-list'));
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('list');
  });

  it('does not call onChange when a disabled tab is clicked', () => {
    const onChange = vi.fn();
    render(
      <TabBar value="board" onChange={onChange} tabs={baseTabs} testid="plan-tabs" />
    );
    const graph = screen.getByTestId('plan-tabs-graph');
    expect(graph.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(graph);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('renders a count chip when count > 0 and omits it when 0/undefined', () => {
    const onChange = vi.fn();
    render(
      <TabBar
        value="board"
        onChange={onChange}
        tabs={[
          { id: 'board', label: 'Board' },
          { id: 'list', label: 'List', count: 0 },
          { id: 'sprints', label: 'Sprints', count: 3 },
        ]}
        testid="plan-tabs"
      />
    );
    // Sprints has a count chip; Board (undefined) and List (0) do not.
    expect(screen.getByTestId('plan-tabs-sprints-count').textContent).toBe('3');
    expect(screen.queryByTestId('plan-tabs-board-count')).toBeNull();
    expect(screen.queryByTestId('plan-tabs-list-count')).toBeNull();
  });

  it('renders the rightSlot at the far right', () => {
    const onChange = vi.fn();
    render(
      <TabBar
        value="board"
        onChange={onChange}
        tabs={baseTabs}
        rightSlot={<button data-testid="recommend-order">Recommend order</button>}
        testid="plan-tabs"
      />
    );
    const slot = screen.getByTestId('plan-tabs-right');
    expect(slot).toBeTruthy();
    // The recommend-order button is inside the right slot wrapper.
    expect(slot.querySelector('[data-testid="recommend-order"]')).toBeTruthy();
    // Tailwind ml-auto pushes it to the right; assert the class is present.
    expect(slot.className).toContain('ml-auto');
  });

  it('places the testid attribute on the container', () => {
    const onChange = vi.fn();
    render(
      <TabBar value="board" onChange={onChange} tabs={baseTabs} testid="plan-tabs" />
    );
    const container = screen.getByTestId('plan-tabs');
    expect(container.getAttribute('role')).toBe('tablist');
  });
});
