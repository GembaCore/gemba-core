import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { BoardColumn } from '../BoardColumn';
import type { WorkItem } from '@/types/core.gen';

function item(overrides: Partial<WorkItem> & Pick<WorkItem, 'id'>): WorkItem {
  return {
    kind: 'task',
    title: overrides.id,
    status: 'open',
    state_category: 'started',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...overrides,
  } as WorkItem;
}

describe('BoardColumn', () => {
  it('renders header label and count', () => {
    render(
      <BoardColumn
        category="started"
        label="In progress"
        items={[item({ id: 'gm-a' }), item({ id: 'gm-b' })]}
      />
    );
    expect(screen.getByRole('heading', { name: 'In progress' })).toBeTruthy();
    expect(screen.getByText('2')).toBeTruthy();
  });

  it('sorts by priority ascending (P0 first), nulls last; ties broken by updated_at desc', () => {
    const items: WorkItem[] = [
      item({ id: 'gm-p2', priority: 2, updated_at: '2026-04-22T00:00:00Z' }),
      item({ id: 'gm-none', priority: null, updated_at: '2026-04-22T00:00:00Z' }),
      item({ id: 'gm-p0', priority: 0, updated_at: '2026-04-21T00:00:00Z' }),
      item({ id: 'gm-p0-fresh', priority: 0, updated_at: '2026-04-22T12:00:00Z' }),
    ];
    const { container } = render(
      <BoardColumn category="started" label="Started" items={items} />
    );
    const list = container.querySelector('ol') as HTMLElement;
    const cards = within(list).getAllByRole('listitem');
    const ids = cards.map((li) => li.querySelector('[data-bead-id]')?.getAttribute('data-bead-id'));
    expect(ids).toEqual(['gm-p0-fresh', 'gm-p0', 'gm-p2', 'gm-none']);
  });

  it('renders zero items without crashing', () => {
    render(<BoardColumn category="completed" label="Completed" items={[]} />);
    expect(screen.getByText('0')).toBeTruthy();
  });
});
