import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WorkItemCard } from '../WorkItemCard';
import { relativeTime } from '../relativeTime';
import type { WorkItem } from '@/types/core.gen';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';

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

// gm-t4af: WorkItemCard now reads `<Capability has="has_evidence">` to
// gate the paperclip glyph + the closed-without-evidence warning.
// Default the capability ON in tests so existing assertions keep
// working; opt-out with renderCard(ui, { evidence: false }).
function caps(opts: { evidence?: boolean } = {}): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'fake',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted', closed: 'completed' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: opts.evidence ?? true,
    },
    orchestration_plane: null,
  };
}

function renderCard(ui: ReactElement, opts?: { evidence?: boolean }) {
  // CapabilitiesProvider runs its own useQuery internally even when
  // `initial` is passed, so it needs a QueryClient in the tree.
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps(opts)}>{children}</CapabilitiesProvider>
      </QueryClientProvider>
    );
  }
  return render(ui, { wrapper: Wrapper });
}

describe('WorkItemCard', () => {
  it('renders id, title, priority chip, and state dot', () => {
    renderCard(<WorkItemCard item={base} />);
    expect(screen.getByText('gm-x1')).toBeTruthy();
    expect(screen.getByText('Render the board pane')).toBeTruthy();
    expect(screen.getByText('P0')).toBeTruthy();
    // State dot has title="started" for accessibility.
    expect(screen.getByTitle('started')).toBeTruthy();
  });

  it('omits priority chip when priority is null', () => {
    renderCard(<WorkItemCard item={{ ...base, priority: null }} />);
    expect(screen.queryByText('P0')).toBeNull();
    expect(screen.queryByText(/^P[0-4]$/)).toBeNull();
  });

  it('shows assignee name when set, falling back to owner', () => {
    const withAssignee: WorkItem = {
      ...base,
      assignee: { id: 'a1', name: 'quartz', agent_kind: 'agent' },
      owner: { id: 'o1', name: 'mike', agent_kind: 'human' },
    };
    renderCard(<WorkItemCard item={withAssignee} />);
    expect(screen.getByText('quartz')).toBeTruthy();

    const ownerOnly: WorkItem = {
      ...base,
      owner: { id: 'o1', name: 'mike', agent_kind: 'human' },
    };
    renderCard(<WorkItemCard item={ownerOnly} />);
    expect(screen.getByText('mike')).toBeTruthy();
  });

  it('renders up to 3 labels with +N overflow', () => {
    const many: WorkItem = {
      ...base,
      labels: ['layer:ui', 'milestone:m1', 'risk:medium', 'surface:frontend', 'fed:safe'],
    };
    renderCard(<WorkItemCard item={many} />);
    const list = screen.getByTestId('workitem-card-gm-x1-labels');
    const chips = within(list).getAllByRole('listitem');
    // 3 visible + 1 overflow chip.
    expect(chips).toHaveLength(4);
    expect(screen.getByText('+2')).toBeTruthy();
  });

  it('shows notes glyph when description is non-empty', () => {
    renderCard(<WorkItemCard item={{ ...base, description: 'hello' }} />);
    expect(screen.getByTestId('glyph-notes')).toBeTruthy();
    expect(screen.queryByTestId('glyph-evidence')).toBeNull();
  });

  it('shows evidence glyph when evidence array is non-empty', () => {
    renderCard(
      <WorkItemCard
        item={{
          ...base,
          evidence: [{ id: 'e1', kind: 'commit', source: 'git', captured_at: '2026-04-22T00:00:00Z' }],
        }}
      />
    );
    expect(screen.getByTestId('glyph-evidence')).toBeTruthy();
  });

  // gm-t4af: evidence glyph is hidden on adaptors that don't support
  // evidence synthesis. Otherwise the operator sees a paperclip on
  // every closed item — meaningless when the manifest doesn't promise
  // synthesized evidence.
  it('hides evidence glyph when has_evidence capability is off', () => {
    renderCard(
      <WorkItemCard
        item={{
          ...base,
          evidence: [{ id: 'e1', kind: 'commit', source: 'git', captured_at: '2026-04-22T00:00:00Z' }],
        }}
      />,
      { evidence: false }
    );
    expect(screen.queryByTestId('glyph-evidence')).toBeNull();
  });

  // gm-t4af: a closed item with empty evidence on a manifest that
  // requires evidence is a workflow gap — surface the missing-required
  // glyph so the operator notices.
  it('shows missing-required glyph when closed item has no evidence and capability requires it', () => {
    renderCard(
      <WorkItemCard
        item={{
          ...base,
          state_category: 'completed',
        }}
      />
    );
    expect(screen.getByTestId('glyph-evidence-missing')).toBeTruthy();
  });

  it('hides missing-required glyph when capability is off', () => {
    renderCard(
      <WorkItemCard
        item={{
          ...base,
          state_category: 'completed',
        }}
      />,
      { evidence: false }
    );
    expect(screen.queryByTestId('glyph-evidence-missing')).toBeNull();
  });

  it('hides missing-required glyph for non-completed states', () => {
    renderCard(<WorkItemCard item={{ ...base, state_category: 'started' }} />);
    expect(screen.queryByTestId('glyph-evidence-missing')).toBeNull();
  });

  it('shows parent glyph when a parent_child relationship originates from this bead', () => {
    const child: WorkItem = {
      ...base,
      relationships: [{ kind: 'parent_child', from: 'gm-x1', to: 'gm-parent' }],
    };
    renderCard(<WorkItemCard item={child} />);
    expect(screen.getByTestId('glyph-parent')).toBeTruthy();
  });

  it('renders the workflow chip when bead is a step of a poured molecule', () => {
    const stepBead: WorkItem = {
      ...base,
      custom: { 'beads:parent': 'mol-shiny-feature-1' },
    };
    renderCard(<WorkItemCard item={stepBead} />);
    const chip = screen.getByTestId('workitem-card-gm-x1-workflow-chip');
    expect(chip).toBeTruthy();
    expect(chip.getAttribute('title')).toContain('mol-shiny-feature-1');
  });

  it('renders the workflow chip for a wisp parent', () => {
    const stepBead: WorkItem = {
      ...base,
      custom: { 'beads:parent': 'wisp-deacon-patrol-3' },
    };
    renderCard(<WorkItemCard item={stepBead} />);
    expect(screen.getByTestId('workitem-card-gm-x1-workflow-chip')).toBeTruthy();
  });

  it('omits the workflow chip when parent is not a workflow run', () => {
    const stepBead: WorkItem = {
      ...base,
      custom: { 'beads:parent': 'gm-other-epic' },
    };
    renderCard(<WorkItemCard item={stepBead} />);
    expect(screen.queryByTestId('workitem-card-gm-x1-workflow-chip')).toBeNull();
  });

  it('omits the workflow chip when no parent is set', () => {
    renderCard(<WorkItemCard item={base} />);
    expect(screen.queryByTestId('workitem-card-gm-x1-workflow-chip')).toBeNull();
  });

  it('is not interactive when onSelect is omitted', () => {
    renderCard(<WorkItemCard item={base} />);
    const article = screen.getByText('gm-x1').closest('article') as HTMLElement;
    expect(article.getAttribute('role')).toBeNull();
    expect(article.getAttribute('tabIndex')).toBeNull();
  });

  it('becomes an ARIA button when onSelect is provided and fires on click', () => {
    const onSelect = vi.fn();
    renderCard(<WorkItemCard item={base} onSelect={onSelect} />);
    const btn = screen.getByRole('button', { name: /open bead gm-x1/i });
    fireEvent.click(btn);
    expect(onSelect).toHaveBeenCalledWith('gm-x1');
  });

  it('renders escalation badge when escalationCount > 0 (gm-e11.3)', () => {
    renderCard(<WorkItemCard item={base} escalationCount={2} />);
    const badge = screen.getByTestId('workitem-card-gm-x1-escalation-badge');
    expect(badge).toBeTruthy();
    expect(badge.textContent).toContain('2');
    expect(badge.textContent).toContain('Triage');
    expect(badge.getAttribute('title')).toContain('2 open escalations');
  });

  it('uses singular "escalation" in the title for count of 1', () => {
    renderCard(<WorkItemCard item={base} escalationCount={1} />);
    const badge = screen.getByTestId('workitem-card-gm-x1-escalation-badge');
    expect(badge.getAttribute('title')).toBe('1 open escalation');
  });

  it('renders canonical state and derived-signal pills', () => {
    renderCard(
      <WorkItemCard
        item={{
          ...base,
          state_category: 'staged',
          derived: {
            agent_claimable: true,
            human_action_required: false,
            review_pending: true,
          },
        }}
      />
    );
    expect(screen.getByTestId('workitem-card-gm-x1-state-pill').textContent).toBe('Staged');
    expect(screen.getByTestId('workitem-card-gm-x1-ready-pill')).toBeTruthy();
    expect(screen.getByTestId('workitem-card-gm-x1-review-pill')).toBeTruthy();
  });

  it('renders human-action-required as Needs input when no open escalation is attached', () => {
    renderCard(
      <WorkItemCard
        item={{
          ...base,
          derived: {
            agent_claimable: false,
            human_action_required: true,
            review_pending: false,
          },
        }}
      />
    );
    expect(screen.getByTestId('workitem-card-gm-x1-blocked-pill').textContent).toBe('Needs input');
  });

  it('omits escalation badge when count is 0 or unset', () => {
    renderCard(<WorkItemCard item={base} />);
    expect(screen.queryByTestId('workitem-card-gm-x1-escalation-badge')).toBeNull();
    renderCard(<WorkItemCard item={base} escalationCount={0} />);
    expect(screen.queryByTestId('workitem-card-gm-x1-escalation-badge')).toBeNull();
  });

  it('activates on Enter and Space from the keyboard', () => {
    const onSelect = vi.fn();
    renderCard(<WorkItemCard item={base} onSelect={onSelect} />);
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
