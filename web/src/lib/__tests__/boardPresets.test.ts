// Board preset metadata tests (gm-e12.19.1). Pure-data assertions
// covering the four v1 presets — semantic narrowing, post-filter
// behavior, default-view mapping, and the URL-string parser.

import { describe, expect, it } from 'vitest';
import {
  BOARD_PRESETS,
  BOARD_PRESET_DEFAULT_VIEW,
  BOARD_PRESET_FILTERS,
  BOARD_PRESET_POST_FILTERS,
  BOARD_PRESET_SORTS,
  isKnownPreset,
} from '../boardPresets';
import type { WorkItem } from '@/types/core.gen';

function wi(patch: Partial<WorkItem> & { id: string }): WorkItem {
  return {
    kind: 'task',
    title: 'title',
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-26T00:00:00Z',
    updated_at: '2026-04-26T00:00:00Z',
    ...patch,
  } as WorkItem;
}

describe('BOARD_PRESETS metadata', () => {
  it('ships exactly the four v1 presets in the canonical order', () => {
    expect(BOARD_PRESETS).toEqual(['staged', 'backlog', 'done-recent', 'mine']);
  });

  it('every preset carries a default view', () => {
    for (const p of BOARD_PRESETS) {
      expect(BOARD_PRESET_DEFAULT_VIEW[p]).toBeTruthy();
    }
    expect(BOARD_PRESET_DEFAULT_VIEW.backlog).toBe('list');
    expect(BOARD_PRESET_DEFAULT_VIEW['done-recent']).toBe('list');
  });

  it('staged preset narrows to staged + started', () => {
    expect(BOARD_PRESET_FILTERS.staged.state_category.sort()).toEqual(['staged', 'started']);
  });

  it('backlog preset narrows to backlog + unstarted', () => {
    expect(BOARD_PRESET_FILTERS.backlog.state_category.sort()).toEqual(['backlog', 'unstarted']);
  });

  it('done-recent post-filter keeps items updated within 7 days', () => {
    const post = BOARD_PRESET_POST_FILTERS['done-recent']!;
    const now = Date.parse('2026-04-26T12:00:00Z');
    const fresh = wi({ id: 'gm-1', updated_at: '2026-04-22T12:00:00Z' });
    const stale = wi({ id: 'gm-2', updated_at: '2026-04-10T12:00:00Z' });
    expect(post(fresh, { now })).toBe(true);
    expect(post(stale, { now })).toBe(false);
  });

  it('done-recent sort orders newest first', () => {
    const sort = BOARD_PRESET_SORTS['done-recent']!;
    const a = wi({ id: 'gm-a', updated_at: '2026-04-20T00:00:00Z' });
    const b = wi({ id: 'gm-b', updated_at: '2026-04-25T00:00:00Z' });
    const sorted = [a, b].sort(sort);
    expect(sorted[0]?.id).toBe('gm-b');
  });

  it('mine post-filter matches assignee.id or assignee.name', () => {
    const post = BOARD_PRESET_POST_FILTERS.mine!;
    const me = wi({
      id: 'gm-1',
      assignee: {
        id: 'gemba/crew/mike3',
        name: 'gemba/crew/mike3',
        agent_kind: 'human',
      },
    });
    const other = wi({
      id: 'gm-2',
      assignee: {
        id: 'gemba/crew/jasper',
        name: 'gemba/crew/jasper',
        agent_kind: 'human',
      },
    });
    const unassigned = wi({ id: 'gm-3' });
    expect(post(me, { currentAgent: 'gemba/crew/mike3' })).toBe(true);
    expect(post(other, { currentAgent: 'gemba/crew/mike3' })).toBe(false);
    expect(post(unassigned, { currentAgent: 'gemba/crew/mike3' })).toBe(false);
    expect(post(me, {})).toBe(false);
  });

  it('isKnownPreset narrows arbitrary strings', () => {
    expect(isKnownPreset('backlog')).toBe('backlog');
    expect(isKnownPreset('not-a-preset')).toBeNull();
    expect(isKnownPreset(null)).toBeNull();
    expect(isKnownPreset('')).toBeNull();
  });
});
