// Tests for the milestone selector model (gm-l7hy).

import { describe, it, expect } from 'vitest';
import {
  MILESTONE_ALL,
  buildMilestoneOptions,
  filterByMilestone,
  listMilestones,
  milestoneNumber,
} from '../milestone';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';

const DEFAULTS = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function milestone(id: string, title: string): WorkItem {
  return { ...DEFAULTS, id, title, kind: KIND_MILESTONE, relationships: [] };
}
function epic(id: string, parentID?: string, title = id): WorkItem {
  return {
    ...DEFAULTS,
    id,
    title,
    kind: 'epic',
    relationships: parentID ? [{ kind: 'parent_child', from: parentID, to: id }] : [],
  };
}
function task(id: string, parentID: string): WorkItem {
  return {
    ...DEFAULTS,
    id,
    title: id,
    kind: 'task',
    relationships: [{ kind: 'parent_child', from: parentID, to: id }],
  };
}

describe('milestoneNumber', () => {
  it('extracts unpadded ascending digits', () => {
    expect(milestoneNumber('M1 Beta')).toBe(1);
    expect(milestoneNumber('M10 Q3 push')).toBe(10);
    expect(milestoneNumber('M999')).toBe(999);
  });
  it('returns 0 when no prefix', () => {
    expect(milestoneNumber('Beta')).toBe(0);
    expect(milestoneNumber('m1 lowercase')).toBe(0);
    expect(milestoneNumber('')).toBe(0);
  });
});

describe('listMilestones', () => {
  it('sorts by M-number ascending; unnumbered last', () => {
    const items = [
      milestone('m-a', 'M3 Late'),
      milestone('m-b', 'M1 Early'),
      milestone('m-c', 'Legacy unnumbered'),
      milestone('m-d', 'M2 Mid'),
      epic('e-1'),
    ];
    expect(listMilestones(items).map((m) => m.id)).toEqual(['m-b', 'm-d', 'm-a', 'm-c']);
  });
});

describe('filterByMilestone', () => {
  // Tree:
  //   M1 ──┬── epic-a ── task-1
  //        └── epic-b
  //   M2 ──── epic-c
  //   epic-orphan (no parent)
  const items = [
    milestone('m-1', 'M1 Beta'),
    milestone('m-2', 'M2 Q3'),
    epic('epic-a', 'm-1'),
    epic('epic-b', 'm-1'),
    epic('epic-c', 'm-2'),
    task('task-1', 'epic-a'),
    epic('epic-orphan'),
  ];

  it('MILESTONE_ALL is a pass-through', () => {
    expect(filterByMilestone(items, MILESTONE_ALL)).toEqual(items);
  });

  it('narrows to descendants and excludes the milestone bead itself', () => {
    const got = filterByMilestone(items, 'm-1').map((i) => i.id).sort();
    expect(got).toEqual(['epic-a', 'epic-b', 'task-1']);
  });

  it('narrows to a different milestone independently', () => {
    const got = filterByMilestone(items, 'm-2').map((i) => i.id);
    expect(got).toEqual(['epic-c']);
  });
});

describe('buildMilestoneOptions', () => {
  it('puts All first, then milestones in M-number order', () => {
    const items = [
      milestone('m-late', 'M5 Late'),
      milestone('m-early', 'M1 Early'),
      milestone('m-legacy', 'Unnumbered legacy'),
      milestone('m-mid', 'M3 Mid'),
    ];
    const opts = buildMilestoneOptions(items);
    expect(opts.map((o) => o.id)).toEqual([
      MILESTONE_ALL,
      'm-early',
      'm-mid',
      'm-late',
      'm-legacy',
    ]);
  });

  it('returns just All when no milestones exist', () => {
    expect(buildMilestoneOptions([epic('e-1')]).map((o) => o.id)).toEqual([MILESTONE_ALL]);
  });
});
