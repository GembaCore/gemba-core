// Tests for the milestone-badge helpers (gm-4se1).

import { describe, expect, it } from 'vitest';
import {
  buildMilestoneByEpic,
  findMilestoneForEpic,
  hashMilestoneID,
  milestoneTone,
  MILESTONE_PALETTE_SIZE,
} from '../milestoneBadge';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';

const DEFAULTS = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function milestone(id: string, title = id): WorkItem {
  return { ...DEFAULTS, id, title, kind: KIND_MILESTONE, relationships: [] };
}
function epic(id: string, parentID?: string): WorkItem {
  return {
    ...DEFAULTS,
    id,
    title: id,
    kind: 'epic',
    relationships: parentID ? [{ kind: 'parent_child', from: parentID, to: id }] : [],
  };
}

describe('findMilestoneForEpic', () => {
  it('returns the direct milestone parent', () => {
    const items = [milestone('m-1', 'M1 Beta'), epic('e-a', 'm-1')];
    const got = findMilestoneForEpic(items, 'e-a');
    expect(got?.id).toBe('m-1');
  });

  it('walks past intermediate epics to a milestone ancestor', () => {
    const items = [
      milestone('m-2', 'M2 Q3'),
      epic('e-root', 'm-2'),
      epic('e-mid', 'e-root'),
      epic('e-leaf', 'e-mid'),
    ];
    expect(findMilestoneForEpic(items, 'e-leaf')?.id).toBe('m-2');
    expect(findMilestoneForEpic(items, 'e-mid')?.id).toBe('m-2');
  });

  it('returns undefined when no milestone exists in the chain', () => {
    const items = [epic('e-orphan'), epic('e-child', 'e-orphan')];
    expect(findMilestoneForEpic(items, 'e-child')).toBeUndefined();
  });

  it('returns undefined when the epic has no parent', () => {
    const items = [epic('e-loose')];
    expect(findMilestoneForEpic(items, 'e-loose')).toBeUndefined();
  });

  it('survives a parent_child cycle without hanging', () => {
    // Synthetic cycle: e-a -> e-b -> e-a. Should terminate cleanly.
    const items: WorkItem[] = [
      {
        ...DEFAULTS,
        id: 'e-a',
        title: 'A',
        kind: 'epic',
        relationships: [{ kind: 'parent_child', from: 'e-b', to: 'e-a' }],
      },
      {
        ...DEFAULTS,
        id: 'e-b',
        title: 'B',
        kind: 'epic',
        relationships: [{ kind: 'parent_child', from: 'e-a', to: 'e-b' }],
      },
    ];
    expect(findMilestoneForEpic(items, 'e-a')).toBeUndefined();
  });

  it('does not return the epic itself when it happens to be a milestone', () => {
    // The walker is for "milestone of an epic" — the epic argument's
    // own kind doesn't matter here, but the result MUST be a strict
    // ancestor, never the input id.
    const items = [milestone('m-1'), epic('e-a', 'm-1')];
    const got = findMilestoneForEpic(items, 'm-1');
    expect(got).toBeUndefined();
  });
});

describe('buildMilestoneByEpic', () => {
  it('maps every epic with a milestone ancestor; skips orphans', () => {
    const items = [
      milestone('m-1', 'M1'),
      milestone('m-2', 'M2'),
      epic('e-a', 'm-1'),
      epic('e-b', 'e-a'),
      epic('e-c', 'm-2'),
      epic('e-orphan'),
    ];
    const map = buildMilestoneByEpic(items);
    expect(map.get('e-a')?.id).toBe('m-1');
    expect(map.get('e-b')?.id).toBe('m-1');
    expect(map.get('e-c')?.id).toBe('m-2');
    expect(map.has('e-orphan')).toBe(false);
    // Non-epics are not keyed.
    expect(map.has('m-1')).toBe(false);
  });
});

describe('hashMilestoneID + milestoneTone', () => {
  it('is deterministic — same id yields the same tone every call', () => {
    const a = milestoneTone('m-1');
    const b = milestoneTone('m-1');
    expect(a).toEqual(b);
    expect(hashMilestoneID('m-1')).toBe(hashMilestoneID('m-1'));
  });

  it('spreads distinct ids across the palette (smoke check)', () => {
    // Not every id will land on a unique slot, but a handful of
    // varied ids should hit at least three different palette slots.
    const ids = ['m-1', 'm-2', 'm-3', 'm-4', 'm-5', 'm-foo', 'm-bar', 'm-baz'];
    const slots = new Set(ids.map((id) => hashMilestoneID(id) % MILESTONE_PALETTE_SIZE));
    expect(slots.size).toBeGreaterThanOrEqual(3);
  });

  it('returns valid Tailwind classes (smoke)', () => {
    const tone = milestoneTone('m-1');
    expect(tone.stripe).toMatch(/^border-t-/);
    expect(tone.pill).toContain('bg-');
    expect(tone.pill).toContain('text-');
  });
});
