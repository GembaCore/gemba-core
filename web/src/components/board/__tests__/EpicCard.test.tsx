import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { EpicCard, type EpicChildCounts } from '../EpicCard';
import type { WorkItem } from '@/types/core.gen';

function emptyCounts(total = 0): EpicChildCounts {
  return {
    total,
    byState: { backlog: 0, unstarted: 0, staged: 0, started: 0, completed: 0, canceled: 0 },
  };
}

const baseEpic: WorkItem = {
  id: 'gm-e1',
  kind: 'epic',
  title: 'Stage one — define and ship the foundation',
  status: 'open',
  state_category: 'started',
  priority: 1,
  created_at: '2026-04-20T00:00:00Z',
  updated_at: '2026-04-22T00:00:00Z',
};

describe('EpicCard — anatomy (gm-pd2 / ui-spec §4.2)', () => {
  it('renders id, title, kind chip, and state dot', () => {
    render(<EpicCard item={baseEpic} childCounts={emptyCounts()} />);
    expect(screen.getByText('gm-e1')).toBeTruthy();
    expect(screen.getByText(/Stage one/)).toBeTruthy();
    expect(screen.getByText(/^epic$/i)).toBeTruthy();
    expect(screen.getByTitle('started')).toBeTruthy();
  });

  it('paints a priority stripe via data-priority and the corresponding border class', () => {
    const { container } = render(
      <EpicCard item={{ ...baseEpic, priority: 0 }} childCounts={emptyCounts()} />
    );
    const article = container.querySelector('[data-epic-card="true"]');
    expect(article).toBeTruthy();
    expect(article!.getAttribute('data-priority')).toBe('P0');
    expect(article!.className).toContain('border-l-rose-500');
    expect(article!.className).toContain('border-l-[3px]');
    // SR-only label exposes the priority for AT users since the
    // stripe is a pure visual cue.
    expect(screen.getByText('Priority P0')).toBeTruthy();
  });

  it('drops the data-priority attribute when no priority is set', () => {
    const noPri: WorkItem = { ...baseEpic, priority: null };
    const { container } = render(<EpicCard item={noPri} childCounts={emptyCounts()} />);
    const article = container.querySelector('[data-epic-card="true"]');
    expect(article!.hasAttribute('data-priority')).toBe(false);
    // The stripe falls back to the default neutral border (no color
    // class added) but the border-l-[3px] width sticks.
    expect(article!.className).toContain('border-l-[3px]');
  });

  it('applies bold + 3-line clamp on the title', () => {
    render(<EpicCard item={baseEpic} childCounts={emptyCounts()} />);
    const heading = screen.getByRole('heading', { level: 3 });
    expect(heading.className).toContain('font-bold');
    expect(heading.className).toContain('line-clamp-3');
  });

  it('renders the readiness row when childCounts.readiness is provided', () => {
    const counts: EpicChildCounts = {
      total: 7,
      byState: {
        backlog: 0,
        unstarted: 1,
        staged: 0,
        started: 1,
        completed: 1,
        canceled: 0,
      },
      readiness: { ready: 3, blocked: 2 },
    };
    render(<EpicCard item={baseEpic} childCounts={counts} />);
    const row = screen.getByTestId('epic-readiness');
    expect(row.textContent).toContain('3/7 ready');
    expect(row.textContent).toContain('2 blocked');
    expect(row.textContent).toContain('1 in progress');
    expect(row.textContent).toContain('1 done');
  });

  it('hides the readiness row entirely when readiness is undefined', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={{ total: 5, byState: emptyCounts(5).byState }}
      />
    );
    expect(screen.queryByTestId('epic-readiness')).toBeNull();
  });

  it('hides the readiness row when total is zero (avoid 0/0 ready)', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={{ ...emptyCounts(0), readiness: { ready: 0, blocked: 0 } }}
      />
    );
    expect(screen.queryByTestId('epic-readiness')).toBeNull();
  });

  it('hides the token-budget gauge when prop is absent', () => {
    render(<EpicCard item={baseEpic} childCounts={emptyCounts(1)} />);
    expect(screen.queryByTestId('epic-token-budget')).toBeNull();
  });

  it('renders the token-budget gauge with usage label when prop is set', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        tokenBudget={{ used: 1234, budget: 10000 }}
      />
    );
    const gauge = screen.getByTestId('epic-token-budget');
    expect(gauge.textContent).toContain('1,234');
    expect(gauge.textContent).toContain('10,000');
  });

  it('shifts gauge color past 70% and again past 100%', () => {
    const { rerender, container } = render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        tokenBudget={{ used: 5000, budget: 10000 }}
      />
    );
    let bar = container.querySelector('[data-testid="epic-token-budget"] div div') as HTMLElement;
    expect(bar.className).toContain('bg-emerald-500');

    rerender(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        tokenBudget={{ used: 7500, budget: 10000 }}
      />
    );
    bar = container.querySelector('[data-testid="epic-token-budget"] div div') as HTMLElement;
    expect(bar.className).toContain('bg-amber-500');

    rerender(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        tokenBudget={{ used: 12000, budget: 10000 }}
      />
    );
    bar = container.querySelector('[data-testid="epic-token-budget"] div div') as HTMLElement;
    expect(bar.className).toContain('bg-rose-500');
  });

  it('hides the badges row when no badges are set', () => {
    render(<EpicCard item={baseEpic} childCounts={emptyCounts(1)} badges={{}} />);
    expect(screen.queryByTestId('epic-badges')).toBeNull();
  });

  it('hides the badges row when escalations is zero (no chip)', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        badges={{ escalations: 0 }}
      />
    );
    expect(screen.queryByTestId('epic-badges')).toBeNull();
  });

  it('renders parallel-group glyph when set', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        badges={{ parallelGroup: 'A' }}
      />
    );
    expect(screen.getByTestId('epic-badge-parallel-group').textContent).toBe('A');
  });

  it('renders escalation badge with count', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        badges={{ escalations: 3 }}
      />
    );
    const badge = screen.getByTestId('epic-badge-escalations');
    expect(badge.textContent).toContain('3');
    expect(badge.getAttribute('title')).toContain('3 open escalations');
  });

  it('renders purview badge when violated', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        badges={{ purviewViolation: true }}
      />
    );
    expect(screen.getByTestId('epic-badge-purview')).toBeTruthy();
  });

  it('renders up to 3 perspective glyphs', () => {
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts(1)}
        badges={{ perspectives: ['planner', 'reviewer', 'architect', 'qa'] }}
      />
    );
    const wrap = screen.getByTestId('epic-badge-perspectives');
    expect(wrap.children.length).toBe(3);
  });

  it('drops the priority chip from the header (replaced by stripe)', () => {
    // The pre-gm-pd2 layout had a P-label chip in the header. The
    // new anatomy moves the priority signal to the left stripe;
    // the visible chip MUST NOT render. The only "P1" mention is
    // the sr-only "Priority P1" label inside a longer string —
    // queryAllByText('P1') with exact match should find nothing.
    render(<EpicCard item={baseEpic} childCounts={emptyCounts()} />);
    expect(screen.queryAllByText('P1').length).toBe(0);
    // The sr-only label is still present for AT users.
    const sr = screen.getByText('Priority P1');
    expect(sr.className).toContain('sr-only');
  });

  it('opens the drawer on single click in non-draggable mode', () => {
    const onSelect = vi.fn();
    render(<EpicCard item={baseEpic} childCounts={emptyCounts()} onSelect={onSelect} />);
    fireEvent.click(screen.getByRole('button'));
    expect(onSelect).toHaveBeenCalledWith('gm-e1');
  });

  it('does not single-click-open in draggable mode (drag-start owns the click)', () => {
    const onSelect = vi.fn();
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts()}
        onSelect={onSelect}
        draggable
      />
    );
    fireEvent.click(screen.getByRole('button'));
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('opens drawer on "o" keypress (matches WorkItemCard convention)', () => {
    const onSelect = vi.fn();
    render(
      <EpicCard
        item={baseEpic}
        childCounts={emptyCounts()}
        onSelect={onSelect}
        draggable
      />
    );
    fireEvent.keyDown(screen.getByRole('button'), { key: 'o' });
    expect(onSelect).toHaveBeenCalledWith('gm-e1');
  });
});
