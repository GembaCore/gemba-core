import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { BeadCard } from '../BeadCard';
import { relativeTime } from '../relativeTime';
import type { WorkItem } from '@/types/core.gen';

const base: WorkItem = {
  id: 'gm-x1',
  kind: 'task',
  title: 'Render the board pane',
  status: 'open',
  state_category: 'started',
  priority: 0,
  created_at: '2026-04-20T00:00:00Z',
  updated_at: '2026-04-22T00:00:00Z',
};

describe('BeadCard', () => {
  it('renders id, title, priority chip, and state dot', () => {
    render(<BeadCard item={base} />);
    expect(screen.getByText('gm-x1')).toBeTruthy();
    expect(screen.getByText('Render the board pane')).toBeTruthy();
    expect(screen.getByText('P0')).toBeTruthy();
    // State dot has title="started" for accessibility.
    expect(screen.getByTitle('started')).toBeTruthy();
  });

  it('omits priority chip when priority is null', () => {
    render(<BeadCard item={{ ...base, priority: null }} />);
    expect(screen.queryByText('P0')).toBeNull();
    expect(screen.queryByText(/^P[0-4]$/)).toBeNull();
  });

  it('shows assignee name when set, falling back to owner', () => {
    const withAssignee: WorkItem = {
      ...base,
      assignee: { id: 'a1', name: 'quartz', agent_kind: 'agent' },
      owner: { id: 'o1', name: 'mike', agent_kind: 'human' },
    };
    render(<BeadCard item={withAssignee} />);
    expect(screen.getByText('quartz')).toBeTruthy();

    const ownerOnly: WorkItem = {
      ...base,
      owner: { id: 'o1', name: 'mike', agent_kind: 'human' },
    };
    render(<BeadCard item={ownerOnly} />);
    expect(screen.getByText('mike')).toBeTruthy();
  });

  it('renders up to 3 labels with +N overflow', () => {
    const many: WorkItem = {
      ...base,
      labels: ['layer:ui', 'milestone:m1', 'risk:medium', 'surface:frontend', 'fed:safe'],
    };
    const { container } = render(<BeadCard item={many} />);
    const list = container.querySelector('ul');
    expect(list).toBeTruthy();
    const chips = within(list as HTMLElement).getAllByRole('listitem');
    // 3 visible + 1 overflow chip.
    expect(chips).toHaveLength(4);
    expect(screen.getByText('+2')).toBeTruthy();
  });

  it('shows notes glyph when description is non-empty', () => {
    render(<BeadCard item={{ ...base, description: 'hello' }} />);
    expect(screen.getByTestId('glyph-notes')).toBeTruthy();
    expect(screen.queryByTestId('glyph-evidence')).toBeNull();
  });

  it('shows evidence glyph when evidence array is non-empty', () => {
    render(
      <BeadCard
        item={{
          ...base,
          evidence: [{ id: 'e1', kind: 'commit', source: 'git', captured_at: '2026-04-22T00:00:00Z' }],
        }}
      />
    );
    expect(screen.getByTestId('glyph-evidence')).toBeTruthy();
  });

  it('shows parent glyph when a parent_child relationship originates from this bead', () => {
    const child: WorkItem = {
      ...base,
      relationships: [{ kind: 'parent_child', from: 'gm-x1', to: 'gm-parent' }],
    };
    render(<BeadCard item={child} />);
    expect(screen.getByTestId('glyph-parent')).toBeTruthy();
  });
});

describe('relativeTime', () => {
  const now = new Date('2026-04-23T00:00:00Z');

  it.each([
    ['2026-04-22T23:59:30Z', '30s'],
    ['2026-04-22T23:55:00Z', '5m'],
    ['2026-04-22T20:00:00Z', '4h'],
    ['2026-04-20T00:00:00Z', '3d'],
    ['2026-04-02T00:00:00Z', '3w'],
    ['2026-01-23T00:00:00Z', '3mo'],
    ['2024-04-23T00:00:00Z', '2y'],
  ])('formats %s as %s', (iso, expected) => {
    expect(relativeTime(iso, now)).toBe(expected);
  });

  it('falls back to the raw string when parsing fails', () => {
    expect(relativeTime('not-a-date', now)).toBe('not-a-date');
  });
});
