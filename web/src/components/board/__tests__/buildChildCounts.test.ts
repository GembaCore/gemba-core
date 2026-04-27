import { describe, expect, it } from 'vitest';
import { buildChildCounts } from '../EpicView';
import type { WorkItem } from '@/types/core.gen';

// Minimal WorkItem factory — keeps the test fixtures small while
// covering the gm-pd2 readiness-aggregation rules buildChildCounts
// owns.
function child(
  id: string,
  parent: string,
  state: WorkItem['state_category'],
  derived?: WorkItem['derived']
): WorkItem {
  return {
    id,
    kind: 'task',
    title: id,
    status: 'open',
    state_category: state,
    created_at: '',
    updated_at: '',
    relationships: [{ kind: 'parent_child', from: parent, to: id }],
    derived,
  };
}

function epic(id: string): WorkItem {
  return {
    id,
    kind: 'epic',
    title: id,
    status: 'open',
    state_category: 'unstarted',
    created_at: '',
    updated_at: '',
    relationships: [],
  };
}

describe('buildChildCounts (gm-pd2)', () => {
  it('aggregates state buckets per parent', () => {
    const items: WorkItem[] = [
      epic('e1'),
      child('c1', 'e1', 'started'),
      child('c2', 'e1', 'started'),
      child('c3', 'e1', 'completed'),
    ];
    const got = buildChildCounts(items);
    const e1 = got.get('e1')!;
    expect(e1.total).toBe(3);
    expect(e1.byState.started).toBe(2);
    expect(e1.byState.completed).toBe(1);
  });

  it('omits readiness when no child carries derived signals', () => {
    const items: WorkItem[] = [
      epic('e1'),
      child('c1', 'e1', 'started'),
      child('c2', 'e1', 'unstarted'),
    ];
    const e1 = buildChildCounts(items).get('e1')!;
    expect(e1.readiness).toBeUndefined();
  });

  it('populates readiness when at least one child has derived signals', () => {
    const items: WorkItem[] = [
      epic('e1'),
      child('c1', 'e1', 'unstarted', {
        agent_claimable: true,
        human_action_required: false,
        review_pending: false,
      }),
      child('c2', 'e1', 'unstarted', {
        agent_claimable: false,
        human_action_required: true,
        review_pending: false,
      }),
      child('c3', 'e1', 'started'), // no derived — counts in state but not readiness
    ];
    const e1 = buildChildCounts(items).get('e1')!;
    expect(e1.readiness).toEqual({ ready: 1, blocked: 1 });
    expect(e1.total).toBe(3);
  });

  it('counts a child that is both agent_claimable and human_action_required as both', () => {
    // Defensible behavior: the two flags answer different questions
    // (could an agent pick this up vs. is a human stuck on it). The
    // server is allowed to set both true on a single bead; the row
    // reflects that without de-duping.
    const items: WorkItem[] = [
      epic('e1'),
      child('c1', 'e1', 'unstarted', {
        agent_claimable: true,
        human_action_required: true,
        review_pending: false,
      }),
    ];
    const e1 = buildChildCounts(items).get('e1')!;
    expect(e1.readiness).toEqual({ ready: 1, blocked: 1 });
  });

  it('handles multiple parents independently', () => {
    const items: WorkItem[] = [
      epic('e1'),
      epic('e2'),
      child('c1', 'e1', 'started', {
        agent_claimable: true,
        human_action_required: false,
        review_pending: false,
      }),
      child('c2', 'e2', 'unstarted', {
        agent_claimable: false,
        human_action_required: true,
        review_pending: false,
      }),
    ];
    const map = buildChildCounts(items);
    expect(map.get('e1')!.readiness).toEqual({ ready: 1, blocked: 0 });
    expect(map.get('e2')!.readiness).toEqual({ ready: 0, blocked: 1 });
  });
});
