import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { WorkItemCard } from '../WorkItemCard';
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

describe('WorkItemCard', () => {
  it('renders id, title, priority chip, and state dot', () => {
    render(<WorkItemCard item={base} />);
    expect(screen.getByText('gm-x1')).toBeTruthy();
    expect(screen.getByText('Render the board pane')).toBeTruthy();
    expect(screen.getByText('P0')).toBeTruthy();
    // State dot has title="started" for accessibility.
    expect(screen.getByTitle('started')).toBeTruthy();
  });

  it('omits priority chip when priority is null', () => {
    render(<WorkItemCard item={{ ...base, priority: null }} />);
    expect(screen.queryByText('P0')).toBeNull();
    expect(screen.queryByText(/^P[0-4]$/)).toBeNull();
  });

  it('shows assignee name when set, falling back to owner', () => {
    const withAssignee: WorkItem = {
      ...base,
      assignee: { id: 'a1', name: 'quartz', agent_kind: 'agent' },
      owner: { id: 'o1', name: 'mike', agent_kind: 'human' },
    };
    render(<WorkItemCard item={withAssignee} />);
    expect(screen.getByText('quartz')).toBeTruthy();

    const ownerOnly: WorkItem = {
      ...base,
      owner: { id: 'o1', name: 'mike', agent_kind: 'human' },
    };
    render(<WorkItemCard item={ownerOnly} />);
    expect(screen.getByText('mike')).toBeTruthy();
  });

  it('renders up to 3 labels with +N overflow', () => {
    const many: WorkItem = {
      ...base,
      labels: ['layer:ui', 'milestone:m1', 'risk:medium', 'surface:frontend', 'fed:safe'],
    };
    const { container } = render(<WorkItemCard item={many} />);
    const list = container.querySelector('ul');
    expect(list).toBeTruthy();
    const chips = within(list as HTMLElement).getAllByRole('listitem');
    // 3 visible + 1 overflow chip.
    expect(chips).toHaveLength(4);
    expect(screen.getByText('+2')).toBeTruthy();
  });

  it('shows notes glyph when description is non-empty', () => {
    render(<WorkItemCard item={{ ...base, description: 'hello' }} />);
    expect(screen.getByTestId('glyph-notes')).toBeTruthy();
    expect(screen.queryByTestId('glyph-evidence')).toBeNull();
  });

  it('shows evidence glyph when evidence array is non-empty', () => {
    render(
      <WorkItemCard
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
    render(<WorkItemCard item={child} />);
    expect(screen.getByTestId('glyph-parent')).toBeTruthy();
  });

  it('renders the workflow chip when bead is a step of a poured molecule', () => {
    const stepBead: WorkItem = {
      ...base,
      custom: { 'beads:parent': 'mol-shiny-feature-1' },
    };
    render(<WorkItemCard item={stepBead} />);
    const chip = screen.getByTestId('workitem-card-gm-x1-workflow-chip');
    expect(chip).toBeTruthy();
    expect(chip.getAttribute('title')).toContain('mol-shiny-feature-1');
  });

  it('renders the workflow chip for a wisp parent', () => {
    const stepBead: WorkItem = {
      ...base,
      custom: { 'beads:parent': 'wisp-deacon-patrol-3' },
    };
    render(<WorkItemCard item={stepBead} />);
    expect(screen.getByTestId('workitem-card-gm-x1-workflow-chip')).toBeTruthy();
  });

  it('omits the workflow chip when parent is not a workflow run', () => {
    const stepBead: WorkItem = {
      ...base,
      custom: { 'beads:parent': 'gm-other-epic' },
    };
    render(<WorkItemCard item={stepBead} />);
    expect(screen.queryByTestId('workitem-card-gm-x1-workflow-chip')).toBeNull();
  });

  it('omits the workflow chip when no parent is set', () => {
    render(<WorkItemCard item={base} />);
    expect(screen.queryByTestId('workitem-card-gm-x1-workflow-chip')).toBeNull();
  });

  it('is not interactive when onSelect is omitted', () => {
    render(<WorkItemCard item={base} />);
    const article = screen.getByText('gm-x1').closest('article') as HTMLElement;
    expect(article.getAttribute('role')).toBeNull();
    expect(article.getAttribute('tabIndex')).toBeNull();
  });

  it('becomes an ARIA button when onSelect is provided and fires on click', () => {
    const onSelect = vi.fn();
    render(<WorkItemCard item={base} onSelect={onSelect} />);
    const btn = screen.getByRole('button', { name: /open bead gm-x1/i });
    fireEvent.click(btn);
    expect(onSelect).toHaveBeenCalledWith('gm-x1');
  });

  it('activates on Enter and Space from the keyboard', () => {
    const onSelect = vi.fn();
    render(<WorkItemCard item={base} onSelect={onSelect} />);
    const btn = screen.getByRole('button', { name: /open bead/i });
    fireEvent.keyDown(btn, { key: 'Enter' });
    fireEvent.keyDown(btn, { key: ' ' });
    expect(onSelect).toHaveBeenCalledTimes(2);
    expect(onSelect).toHaveBeenNthCalledWith(1, 'gm-x1');
    expect(onSelect).toHaveBeenNthCalledWith(2, 'gm-x1');
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
