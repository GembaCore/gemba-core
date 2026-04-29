import { describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BoardColumn } from '../BoardColumn';
import type { WorkItem } from '@/types/core.gen';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';

// gm-t4af: BoardColumn renders WorkItemCard, which now reads
// `<Capability has="has_evidence">`. Wrap in a CapabilitiesProvider
// (which itself needs a QueryClient) so the cards render their
// gated glyphs without crashing.
function caps(): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'fake',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted', closed: 'completed' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: true,
    },
    orchestration_plane: null,
  };
}

function renderColumn(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps()}>{children}</CapabilitiesProvider>
      </QueryClientProvider>
    );
  }
  return render(ui, { wrapper: Wrapper });
}

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
    renderColumn(
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
    const { container } = renderColumn(
      <BoardColumn category="started" label="Started" items={items} />
    );
    const list = container.querySelector('ol') as HTMLElement;
    const cards = within(list).getAllByRole('listitem');
    const ids = cards.map((li) => li.querySelector('[data-work-item-id]')?.getAttribute('data-work-item-id'));
    expect(ids).toEqual(['gm-p0-fresh', 'gm-p0', 'gm-p2', 'gm-none']);
  });

  it('renders zero items without crashing', () => {
    renderColumn(<BoardColumn category="completed" label="Completed" items={[]} />);
    expect(screen.getByText('0')).toBeTruthy();
  });
});
