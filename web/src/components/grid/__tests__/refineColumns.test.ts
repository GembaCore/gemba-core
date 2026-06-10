// Helper tests for refine-specific column derivations (gm-51i2).

import { describe, expect, it } from 'vitest';
import {
  ageTimestamp,
  countIncomingBlockers,
  formatAge,
  getSuggestedEpic,
  shouldRenderDispatchStatus,
} from '../refineColumns';
import type { WorkItem } from '@/types/core.gen';

function wi(patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id: 'gm-x',
    kind: 'task',
    title: 'x',
    status: 'open',
    state_category: 'backlog',
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    ...patch,
  };
}

describe('formatAge', () => {
  const now = new Date('2026-04-29T00:00:00Z');

  it('renders days under 30', () => {
    expect(formatAge('2026-04-25T00:00:00Z', now)).toBe('4d');
    expect(formatAge('2026-04-29T00:00:00Z', now)).toBe('0d');
    expect(formatAge('2026-04-01T00:00:00Z', now)).toBe('28d');
  });

  it('renders months between 30 days and 12 months', () => {
    // 60 days back → 2mo
    expect(formatAge('2026-02-28T00:00:00Z', now)).toBe('2mo');
    // exactly 30 days → 1mo
    expect(formatAge('2026-03-30T00:00:00Z', now)).toBe('1mo');
  });

  it('renders years at and beyond 12 months', () => {
    expect(formatAge('2025-04-29T00:00:00Z', now)).toBe('1y');
    expect(formatAge('2023-01-01T00:00:00Z', now)).toBe('3y');
  });

  it('handles missing or malformed timestamps', () => {
    expect(formatAge(undefined, now)).toBe('—');
    expect(formatAge('not-a-date', now)).toBe('—');
  });

  it('clamps future timestamps to 0d', () => {
    expect(formatAge('2027-01-01T00:00:00Z', now)).toBe('0d');
  });
});

describe('ageTimestamp', () => {
  it('returns parseable timestamps as numbers', () => {
    expect(ageTimestamp('2026-04-01T00:00:00Z')).toBe(Date.parse('2026-04-01T00:00:00Z'));
  });

  it('returns 0 for missing or unparseable input', () => {
    expect(ageTimestamp(undefined)).toBe(0);
    expect(ageTimestamp('garbage')).toBe(0);
  });
});

describe('countIncomingBlockers', () => {
  it('counts blocks edges where this item is the to side', () => {
    const item = wi({
      id: 'gm-target',
      relationships: [
        { kind: 'blocks', from: 'gm-a', to: 'gm-target' },
        { kind: 'blocks', from: 'gm-b', to: 'gm-target' },
        { kind: 'blocks', from: 'gm-target', to: 'gm-c' }, // outgoing — ignored
        { kind: 'parent_child', from: 'gm-d', to: 'gm-target' }, // wrong kind — ignored
      ],
    });
    expect(countIncomingBlockers(item)).toBe(2);
  });

  it('returns 0 when there are no relationships', () => {
    expect(countIncomingBlockers(wi({ id: 'gm-x' }))).toBe(0);
    expect(countIncomingBlockers(wi({ id: 'gm-x', relationships: [] }))).toBe(0);
  });
});

describe('getSuggestedEpic', () => {
  it('reads gemba.suggested_epic from custom', () => {
    expect(getSuggestedEpic(wi({ custom: { 'gemba.suggested_epic': 'gm-epic-7' } }))).toBe('gm-epic-7');
  });

  it('returns null when absent or non-string', () => {
    expect(getSuggestedEpic(wi())).toBeNull();
    expect(getSuggestedEpic(wi({ custom: {} }))).toBeNull();
    expect(getSuggestedEpic(wi({ custom: { 'gemba.suggested_epic': '' } }))).toBeNull();
    expect(getSuggestedEpic(wi({ custom: { 'gemba.suggested_epic': 42 } }))).toBeNull();
  });
});

describe('shouldRenderDispatchStatus', () => {
  it('renders only non-default values', () => {
    expect(shouldRenderDispatchStatus(wi({ dispatch_status: 'awaiting-review' }))).toBe(true);
    expect(shouldRenderDispatchStatus(wi({ dispatch_status: 'not-now' }))).toBe(true);
  });

  it('suppresses ready / empty / undefined', () => {
    expect(shouldRenderDispatchStatus(wi({ dispatch_status: 'ready' }))).toBe(false);
    expect(shouldRenderDispatchStatus(wi({ dispatch_status: '' }))).toBe(false);
    expect(shouldRenderDispatchStatus(wi({}))).toBe(false);
  });
});
