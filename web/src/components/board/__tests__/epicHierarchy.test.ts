import { describe, expect, it } from 'vitest';
import {
  epicChildren,
  groupEpicsByRoot,
  isEpic,
  ORPHAN_ROOT_ID,
} from '../epicHierarchy';
import type { WorkItem } from '@/types/core.gen';

function epic(id: string, parent?: string): WorkItem {
  return {
    id,
    kind: 'epic',
    title: id,
    status: 'open',
    state_category: 'unstarted',
    created_at: '',
    updated_at: '',
    relationships: parent ? [{ kind: 'parent_child', from: parent, to: id }] : [],
  };
}

function task(id: string, parent?: string): WorkItem {
  return {
    id,
    kind: 'task',
    title: id,
    status: 'open',
    state_category: 'unstarted',
    created_at: '',
    updated_at: '',
    relationships: parent ? [{ kind: 'parent_child', from: parent, to: id }] : [],
  };
}

describe('isEpic', () => {
  it('only epic-kind WorkItems qualify', () => {
    expect(isEpic(epic('e1'))).toBe(true);
    expect(isEpic(task('t1'))).toBe(false);
  });
});

describe('groupEpicsByRoot', () => {
  it('returns no swimlanes for an empty set', () => {
    expect(groupEpicsByRoot([])).toEqual([]);
    expect(groupEpicsByRoot([task('t1')])).toEqual([]);
  });

  it('top-level epic with no parent is its own swimlane', () => {
    const out = groupEpicsByRoot([epic('root')]);
    expect(out).toHaveLength(1);
    expect(out[0].root.id).toBe('root');
    expect(out[0].members.map((m) => m.id)).toEqual(['root']);
  });

  it('multi-level chain rolls up to the root', () => {
    const items = [
      epic('root'),
      epic('mid', 'root'),
      epic('leaf', 'mid'),
    ];
    const out = groupEpicsByRoot(items);
    expect(out).toHaveLength(1);
    expect(out[0].root.id).toBe('root');
    // root first then descendants alphabetically.
    expect(out[0].members.map((m) => m.id)).toEqual(['root', 'leaf', 'mid']);
  });

  it('orphans (parent not in input) bucket into the synthetic swimlane', () => {
    const items = [
      epic('e1', 'gone-from-set'),
      epic('e2', 'also-gone'),
      epic('real-root'),
    ];
    const out = groupEpicsByRoot(items);
    expect(out).toHaveLength(2);
    // real-root sorts before orphans.
    expect(out[0].root.id).toBe('real-root');
    expect(out[1].root.id).toBe(ORPHAN_ROOT_ID);
    expect(out[1].members.map((m) => m.id).sort()).toEqual(['e1', 'e2']);
  });

  it('parent that exists in input but is NOT an epic still orphans the child', () => {
    // A task being someone's parent is unusual but can happen in bd; a
    // child epic whose parent is a task isn't really "under" anything.
    const items = [task('parent-task'), epic('child-epic', 'parent-task')];
    const out = groupEpicsByRoot(items);
    expect(out).toHaveLength(1);
    expect(out[0].root.id).toBe(ORPHAN_ROOT_ID);
    expect(out[0].members.map((m) => m.id)).toEqual(['child-epic']);
  });

  it('cycles in the parent chain do not loop forever', () => {
    // a → b → a (synthetic; bd would reject but the SPA shouldn't trust).
    const items = [
      { ...epic('a'), relationships: [{ kind: 'parent_child', from: 'b', to: 'a' } as const] },
      { ...epic('b'), relationships: [{ kind: 'parent_child', from: 'a', to: 'b' } as const] },
    ];
    const out = groupEpicsByRoot(items);
    expect(out.some((s) => s.root.id === ORPHAN_ROOT_ID)).toBe(true);
  });

  it('sorts swimlanes alphabetically by root id, with orphans last', () => {
    const items = [epic('z-root'), epic('a-root'), epic('orphan', 'gone')];
    const out = groupEpicsByRoot(items);
    expect(out.map((s) => s.root.id)).toEqual(['a-root', 'z-root', ORPHAN_ROOT_ID]);
  });
});

describe('epicChildren', () => {
  it('returns items whose parent_child.from is the epic id', () => {
    const items = [
      epic('e1'),
      task('t1', 'e1'),
      task('t2', 'e1'),
      task('t3', 'other'),
    ];
    const kids = epicChildren(items, 'e1');
    expect(kids.map((k) => k.id).sort()).toEqual(['t1', 't2']);
  });

  it('does not return the epic itself even if a self-edge exists', () => {
    const e: WorkItem = {
      ...epic('e1'),
      relationships: [{ kind: 'parent_child', from: 'e1', to: 'e1' }],
    };
    expect(epicChildren([e], 'e1')).toEqual([]);
  });
});
