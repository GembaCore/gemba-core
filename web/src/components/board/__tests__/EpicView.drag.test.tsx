// Drag-to-restage (gm-75u / ui-spec §4.5). The drag→PATCH pipeline
// is split into:
//
//   1. resolveRestage() — pure function: (DragEnd, itemIndex) → PATCH
//   2. onDragEnd wiring — EpicView calls useUpdateWorkItem.mutate()
//      with whatever resolveRestage returns.
//
// dnd-kit pointer events are jsdom-hostile (they depend on the DOM
// reporting layout rects that jsdom fakes), so the integration test
// covers the branch that matters — "a drop that changes columns fires
// the right PATCH body" — by driving the pure resolver through the
// same inputs a real DragEndEvent would produce.

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { WorkItem } from '@/types/core.gen';
import { workItemsKeys } from '@/hooks/useWorkItems';
import { CapabilitiesProvider, type CapabilitiesResponse } from '@/capabilities';
import { EpicView } from '../EpicView';
import { cellId, parseCellId, resolveRestage } from '../dragToRestage';

const caps: CapabilitiesResponse = {
  work_plane: {
    adaptor_name: 'fake',
    adaptor_version: '0.1.0',
    protocol_version: '0.1.0',
    transport: 'api',
    state_map: { open: 'unstarted' },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
  },
  orchestration_plane: null,
};

function epic(id: string, state: WorkItem['state_category']): WorkItem {
  return {
    id,
    kind: 'epic',
    title: `Epic ${id}`,
    status: 'open',
    state_category: state,
    created_at: '2026-04-10T00:00:00Z',
    updated_at: '2026-04-20T00:00:00Z',
  };
}

describe('cellId / parseCellId', () => {
  it('round-trips a plain rootID', () => {
    const id = cellId('demo/pc-root', 'staged');
    expect(id).toBe('cell|demo/pc-root|staged');
    expect(parseCellId(id)).toEqual({ rootID: 'demo/pc-root', cat: 'staged' });
  });

  it('handles a rootID that itself contains a slash (bd prefixed id)', () => {
    const id = cellId('gemba/gemba/gm-root', 'started');
    expect(parseCellId(id)).toEqual({ rootID: 'gemba/gemba/gm-root', cat: 'started' });
  });

  it('returns null for non-cell ids', () => {
    expect(parseCellId('something-else')).toBeNull();
    expect(parseCellId('cell|no-category')).toBeNull();
    expect(parseCellId('cell|root|bogus')).toBeNull();
  });
});

describe('resolveRestage', () => {
  const items = new Map<string, WorkItem>([
    ['demo/pc-a', epic('demo/pc-a', 'unstarted')],
    ['demo/pc-b', epic('demo/pc-b', 'staged')],
  ]);

  it('produces a PATCH when the drop target is a different column', () => {
    expect(
      resolveRestage({
        activeID: 'demo/pc-a',
        overID: cellId('demo/pc-root', 'started'),
        itemById: items,
      })
    ).toEqual({ id: 'demo/pc-a', patch: { state_category: 'started' } });
  });

  it('is a no-op when the drop target is the same column', () => {
    expect(
      resolveRestage({
        activeID: 'demo/pc-a',
        overID: cellId('demo/pc-root', 'unstarted'),
        itemById: items,
      })
    ).toBeNull();
  });

  it('is a no-op when over is absent (drop outside any cell)', () => {
    expect(
      resolveRestage({
        activeID: 'demo/pc-a',
        overID: null,
        itemById: items,
      })
    ).toBeNull();
  });

  it('is a no-op when the over target is not a cell id', () => {
    expect(
      resolveRestage({
        activeID: 'demo/pc-a',
        overID: 'something-else',
        itemById: items,
      })
    ).toBeNull();
  });

  it('is a no-op when the active id is not in the item index', () => {
    expect(
      resolveRestage({
        activeID: 'demo/pc-missing',
        overID: cellId('demo/pc-root', 'started'),
        itemById: items,
      })
    ).toBeNull();
  });
});

describe('EpicView — droppable cells + draggable cards smoke', () => {
  it('renders every column as a droppable target and every epic card as draggable', () => {
    const items: WorkItem[] = [
      epic('demo/pc-root', 'backlog'),
      epic('demo/pc-a', 'unstarted'),
      epic('demo/pc-b', 'staged'),
    ];
    // Wire all three under pc-root so they share a swimlane.
    items[1].relationships = [
      { kind: 'parent_child', from: 'demo/pc-root', to: 'demo/pc-a' },
    ];
    items[2].relationships = [
      { kind: 'parent_child', from: 'demo/pc-root', to: 'demo/pc-b' },
    ];
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(workItemsKeys.list(), items);
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <CapabilitiesProvider initial={caps}>
            <EpicView items={items} onSelectEpic={() => {}} />
          </CapabilitiesProvider>
        </QueryClientProvider>
      </MemoryRouter>
    );
    // Every state column for the pc-root swimlane is a droppable cell.
    for (const cat of ['backlog', 'unstarted', 'staged', 'started', 'completed', 'canceled']) {
      expect(
        screen.getByTestId(`board-epic-cell-demo/pc-root-${cat}`),
        `cell for ${cat} missing`
      ).toBeTruthy();
    }
    // Cards carry the data-epic-card attr regardless of drag state.
    const cards = document.querySelectorAll('[data-epic-card="true"]');
    expect(cards.length).toBeGreaterThanOrEqual(3);
  });
});
