// Drag-to-reparent (gm-935r). The dnd-kit pointer events are jsdom-
// hostile, so the integration we cover here is the pure resolver:
// (DragEnd, item index) → PATCH parent_id payload, including the
// MILESTONE_ALL → "" sentinel and the no-op branches.

import { describe, expect, it } from 'vitest';
import type { WorkItem } from '@/types/core.gen';
import {
  milestoneOptionDroppableId,
  parseMilestoneOptionId,
  resolveReparent,
} from '../dragToReparent';
import { MILESTONE_ALL } from '../milestone';

function epic(id: string, parentId?: string): WorkItem {
  const it: WorkItem = {
    id,
    kind: 'epic',
    title: `Epic ${id}`,
    status: 'open',
    state_category: 'unstarted',
    created_at: '',
    updated_at: '',
    relationships: [],
  };
  if (parentId) {
    it.relationships = [{ kind: 'parent_child', from: parentId, to: id }];
  }
  return it;
}

describe('milestoneOptionDroppableId / parseMilestoneOptionId', () => {
  it('round-trips a milestone id', () => {
    const id = milestoneOptionDroppableId('demo/m-1');
    expect(id).toBe('milestone-option|demo/m-1');
    expect(parseMilestoneOptionId(id)).toBe('demo/m-1');
  });

  it('round-trips MILESTONE_ALL', () => {
    const id = milestoneOptionDroppableId(MILESTONE_ALL);
    expect(parseMilestoneOptionId(id)).toBe(MILESTONE_ALL);
  });

  it('returns null for non-option ids', () => {
    expect(parseMilestoneOptionId('cell|root|started')).toBeNull();
    expect(parseMilestoneOptionId('something-else')).toBeNull();
  });
});

describe('resolveReparent', () => {
  const items = new Map<string, WorkItem>([
    ['demo/e-a', epic('demo/e-a')],
    ['demo/e-b', epic('demo/e-b', 'demo/m-1')],
    ['demo/t-c', { ...epic('demo/t-c'), kind: 'task' }],
  ]);

  it('produces a parent_id PATCH when an epic is dropped on a milestone option', () => {
    expect(
      resolveReparent({
        activeID: 'demo/e-a',
        overID: milestoneOptionDroppableId('demo/m-1'),
        itemById: items,
      }),
    ).toEqual({ id: 'demo/e-a', patch: { parent_id: 'demo/m-1' } });
  });

  it('emits parent_id="" when an epic is dropped on the All option', () => {
    expect(
      resolveReparent({
        activeID: 'demo/e-b',
        overID: milestoneOptionDroppableId(MILESTONE_ALL),
        itemById: items,
      }),
    ).toEqual({ id: 'demo/e-b', patch: { parent_id: '' } });
  });

  it('is a no-op when the epic is already parented to the target milestone', () => {
    expect(
      resolveReparent({
        activeID: 'demo/e-b',
        overID: milestoneOptionDroppableId('demo/m-1'),
        itemById: items,
      }),
    ).toBeNull();
  });

  it('is a no-op when an unparented epic is dropped on All', () => {
    expect(
      resolveReparent({
        activeID: 'demo/e-a',
        overID: milestoneOptionDroppableId(MILESTONE_ALL),
        itemById: items,
      }),
    ).toBeNull();
  });

  it('is a no-op when over is absent', () => {
    expect(
      resolveReparent({
        activeID: 'demo/e-a',
        overID: null,
        itemById: items,
      }),
    ).toBeNull();
  });

  it('is a no-op when the over id is not a milestone option', () => {
    expect(
      resolveReparent({
        activeID: 'demo/e-a',
        overID: 'cell|root|started',
        itemById: items,
      }),
    ).toBeNull();
  });

  it('is a no-op when the active id is not in the item index', () => {
    expect(
      resolveReparent({
        activeID: 'demo/missing',
        overID: milestoneOptionDroppableId('demo/m-1'),
        itemById: items,
      }),
    ).toBeNull();
  });

  it('is a no-op when the dragged item is not an epic', () => {
    expect(
      resolveReparent({
        activeID: 'demo/t-c',
        overID: milestoneOptionDroppableId('demo/m-1'),
        itemById: items,
      }),
    ).toBeNull();
  });
});
