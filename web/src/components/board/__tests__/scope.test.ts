import { describe, it, expect } from 'vitest';
import {
  SCOPE_ALL,
  rootEpics,
  childEpicsOf,
  lineageIDs,
  filterByScope,
  buildScopeOptions,
} from '../scope';
import type { WorkItem } from '@/types/core.gen';

const ITEM_DEFAULTS = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function epic(id: string, parentID?: string): WorkItem {
  return {
    ...ITEM_DEFAULTS,
    id,
    title: id,
    kind: 'epic',
    relationships: parentID
      ? [{ kind: 'parent_child', from: parentID, to: id }]
      : [],
  };
}

function task(id: string, parentID: string): WorkItem {
  return {
    ...ITEM_DEFAULTS,
    id,
    title: id,
    kind: 'task',
    relationships: [{ kind: 'parent_child', from: parentID, to: id }],
  };
}

describe('rootEpics', () => {
  it('returns epics with no incoming parent_child from another epic', () => {
    const items = [
      epic('root-a'),
      epic('root-b'),
      epic('child-of-a', 'root-a'),
      task('t-1', 'root-a'),
    ];
    const got = rootEpics(items).map((e) => e.id).sort();
    expect(got).toEqual(['root-a', 'root-b']);
  });
});

describe('childEpicsOf', () => {
  it('returns one-level-down epic children only (not tasks, not grandchildren)', () => {
    const items = [
      epic('root-a'),
      epic('child-1', 'root-a'),
      epic('child-2', 'root-a'),
      epic('grandchild', 'child-1'),
      task('task', 'root-a'),
    ];
    const got = childEpicsOf(items, 'root-a').map((e) => e.id).sort();
    expect(got).toEqual(['child-1', 'child-2']);
  });
});

describe('lineageIDs', () => {
  it('walks the descendant tree from the scope id', () => {
    const items = [
      epic('root'),
      epic('mid', 'root'),
      epic('leaf', 'mid'),
      task('t', 'leaf'),
      epic('unrelated'),
    ];
    const got = lineageIDs(items, 'root');
    expect(Array.from(got).sort()).toEqual(['leaf', 'mid', 'root', 't']);
  });

  it('terminates on cycles', () => {
    // Pathological — parent_child cycle. lineageIDs must not loop.
    const items: WorkItem[] = [
      {
        ...ITEM_DEFAULTS,
        id: 'a',
        title: 'a',
        kind: 'epic',
        relationships: [{ kind: 'parent_child', from: 'b', to: 'a' }],
      },
      {
        ...ITEM_DEFAULTS,
        id: 'b',
        title: 'b',
        kind: 'epic',
        relationships: [{ kind: 'parent_child', from: 'a', to: 'b' }],
      },
    ];
    const got = lineageIDs(items, 'a');
    expect(Array.from(got).sort()).toEqual(['a', 'b']);
  });

  it('returns just the scope id when nothing descends', () => {
    const items = [epic('lone'), epic('other')];
    expect(Array.from(lineageIDs(items, 'lone'))).toEqual(['lone']);
  });
});

describe('filterByScope', () => {
  const items = [
    epic('root-a'),
    epic('child-of-a', 'root-a'),
    epic('root-b'),
    task('t-of-a', 'root-a'),
  ];

  it('SCOPE_ALL is a pass-through', () => {
    expect(filterByScope(items, SCOPE_ALL)).toEqual(items);
  });

  it('narrowing to a root keeps the lineage', () => {
    const got = filterByScope(items, 'root-a').map((i) => i.id).sort();
    expect(got).toEqual(['child-of-a', 'root-a', 't-of-a']);
  });

  it('narrowing to a non-root scope is a leaf', () => {
    const got = filterByScope(items, 'child-of-a').map((i) => i.id);
    expect(got).toEqual(['child-of-a']);
  });
});

describe('buildScopeOptions', () => {
  it('puts All first, then root epics with their direct children indented', () => {
    const items = [
      epic('root-z'),       // sorted last
      epic('root-a'),
      epic('child-of-a-2', 'root-a'),
      epic('child-of-a-1', 'root-a'),
      task('not-an-epic', 'root-a'), // tasks excluded from picker
    ];
    const opts = buildScopeOptions(items);
    expect(opts.map((o) => `${o.depth}:${o.id}`)).toEqual([
      '0:all',
      '0:root-a',
      '1:child-of-a-1',
      '1:child-of-a-2',
      '0:root-z',
    ]);
  });

  it('returns just All when there are no epics', () => {
    expect(buildScopeOptions([]).map((o) => o.id)).toEqual([SCOPE_ALL]);
  });
});
