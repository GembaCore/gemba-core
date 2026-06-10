// RecentPage tests (gm-g5xz.2 + .3).
//
// Covers the pure helpers — preset → cutoff math, watermark
// localStorage round-trip, parent-grouping convention. The render
// path goes through useFilteredWorkItems → apiFetch which the
// existing fetch-mock infra handles; here we focus on the page's
// internal logic to keep the test fast and stable.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { __internal } from '../RecentPage';
import type { WorkItem } from '@/types/core.gen';

const {
  computeCutoff,
  readWatermarkPreset,
  writeWatermarkPreset,
  formatRelative,
  groupByParent,
  WATERMARK_KEY,
  PRESETS,
  DEFAULT_PRESET,
} = __internal;

function fixtureWorkItem(over: Partial<WorkItem> = {}): WorkItem {
  return {
    id: 'gemba/gemba/gm-test',
    kind: 'task',
    title: 'fixture',
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-30T10:00:00Z',
    updated_at: '2026-04-30T10:00:00Z',
    ...over,
  };
}

describe('computeCutoff', () => {
  it('returns now-Nh ISO for non-zero presets', () => {
    const fixedNow = Date.UTC(2026, 3, 30, 12, 0, 0);
    const realNow = Date.now;
    Date.now = () => fixedNow;
    try {
      const got = computeCutoff('1h');
      expect(got).toBe('2026-04-30T11:00:00.000Z');
    } finally {
      Date.now = realNow;
    }
  });

  it("'all' returns the unix epoch so the server sends everything", () => {
    expect(computeCutoff('all')).toBe('1970-01-01T00:00:00.000Z');
  });

  it('falls back to 24h for an unknown preset', () => {
    const fixedNow = Date.UTC(2026, 3, 30, 12, 0, 0);
    const realNow = Date.now;
    Date.now = () => fixedNow;
    try {
      const got = computeCutoff('bogus');
      // 24h before fixedNow
      expect(got).toBe('2026-04-29T12:00:00.000Z');
    } finally {
      Date.now = realNow;
    }
  });
});

describe('watermark localStorage round-trip', () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
  });

  it('returns the default when nothing is stored', () => {
    expect(readWatermarkPreset()).toBe(DEFAULT_PRESET);
  });

  it('round-trips a known preset', () => {
    writeWatermarkPreset('7d');
    expect(localStorage.getItem(WATERMARK_KEY)).toBe('7d');
    expect(readWatermarkPreset()).toBe('7d');
  });

  it('rejects an unknown stored value and returns the default', () => {
    localStorage.setItem(WATERMARK_KEY, 'pizza');
    expect(readWatermarkPreset()).toBe(DEFAULT_PRESET);
  });

  it('PRESETS contains the documented 8 stops', () => {
    expect(PRESETS.map((p) => p.id)).toEqual([
      '1h',
      '4h',
      '12h',
      '24h',
      '3d',
      '7d',
      '30d',
      'all',
    ]);
  });
});

describe('formatRelative', () => {
  it('handles seconds → "just now"', () => {
    const now = Date.UTC(2026, 3, 30, 12, 0, 0);
    expect(
      formatRelative(new Date(now - 30_000).toISOString(), now)
    ).toBe('just now');
  });

  it('formats minutes', () => {
    const now = Date.UTC(2026, 3, 30, 12, 0, 0);
    expect(
      formatRelative(new Date(now - 5 * 60_000).toISOString(), now)
    ).toBe('5m ago');
  });

  it('formats hours', () => {
    const now = Date.UTC(2026, 3, 30, 12, 0, 0);
    expect(
      formatRelative(new Date(now - 3 * 3600_000).toISOString(), now)
    ).toBe('3h ago');
  });

  it('formats days', () => {
    const now = Date.UTC(2026, 3, 30, 12, 0, 0);
    expect(
      formatRelative(new Date(now - 2 * 86400_000).toISOString(), now)
    ).toBe('2d ago');
  });
});

describe('groupByParent', () => {
  it('groups by the parent_child edge convention (from=parent, to=child)', () => {
    const epic = fixtureWorkItem({ id: 'gemba/gemba/gm-epic', kind: 'epic', title: 'big plan' });
    const child1 = fixtureWorkItem({
      id: 'gemba/gemba/gm-child-1',
      title: 'first task',
      relationships: [{ kind: 'parent_child', from: epic.id, to: 'gemba/gemba/gm-child-1' }],
    });
    const child2 = fixtureWorkItem({
      id: 'gemba/gemba/gm-child-2',
      title: 'second task',
      relationships: [{ kind: 'parent_child', from: epic.id, to: 'gemba/gemba/gm-child-2' }],
    });
    const orphan = fixtureWorkItem({ id: 'gemba/gemba/gm-orphan', title: 'lonely' });

    const groups = groupByParent([epic, child1, child2, orphan]);
    // Epic shows up under '__none__' (it has no parent edge); children under epic.id.
    const byKey = Object.fromEntries(
      groups.map((g) => [g.parentId ?? '__none__', g])
    );
    expect(byKey['__none__']?.items.map((i) => i.id)).toEqual([
      epic.id,
      orphan.id,
    ]);
    expect(byKey[epic.id]?.items.map((i) => i.id)).toEqual([
      child1.id,
      child2.id,
    ]);
    expect(byKey[epic.id]?.parentTitle).toBe('big plan');
  });

  it('leaves parentTitle null when the parent is outside the window', () => {
    const child = fixtureWorkItem({
      id: 'gemba/gemba/gm-child',
      relationships: [
        { kind: 'parent_child', from: 'gemba/gemba/gm-far-away-epic', to: 'gemba/gemba/gm-child' },
      ],
    });
    const groups = groupByParent([child]);
    expect(groups).toHaveLength(1);
    expect(groups[0]?.parentId).toBe('gemba/gemba/gm-far-away-epic');
    expect(groups[0]?.parentTitle).toBeNull();
  });
});
