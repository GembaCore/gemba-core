import { describe, expect, it } from 'vitest';
import {
  buildEscalationsByItem,
  countEscalationsInScope,
} from '../escalationLookup';
import type { EscalationRequest } from '@/api/escalations';

function open(id: string, work_item_id?: string): EscalationRequest {
  return {
    id,
    source: 'question',
    urgency: 'advisory',
    title: id,
    prompt: '',
    state: 'open',
    created_at: '',
    work_item_id,
  } as EscalationRequest;
}

describe('buildEscalationsByItem', () => {
  it('buckets open escalations by their target work_item_id', () => {
    const m = buildEscalationsByItem([open('e1', 'gm-a'), open('e2', 'gm-a'), open('e3', 'gm-b')]);
    expect(m.get('gm-a')?.length).toBe(2);
    expect(m.get('gm-b')?.length).toBe(1);
  });

  it('skips escalations without a work_item_id', () => {
    const m = buildEscalationsByItem([open('e1'), open('e2', 'gm-a')]);
    expect(m.size).toBe(1);
    expect(m.get('gm-a')?.length).toBe(1);
  });

  it('skips non-open escalations (resolved / canceled / expired)', () => {
    const e1 = open('e1', 'gm-a');
    e1.state = 'resolved';
    const e2 = open('e2', 'gm-a');
    const m = buildEscalationsByItem([e1, e2]);
    expect(m.get('gm-a')?.length).toBe(1);
  });

  it('returns an empty map for undefined input', () => {
    const m = buildEscalationsByItem(undefined);
    expect(m.size).toBe(0);
  });
});

describe('countEscalationsInScope', () => {
  it('counts every targeted open escalation when scope is undefined', () => {
    const m = buildEscalationsByItem([open('e1', 'gm-a'), open('e2', 'gm-b')]);
    expect(countEscalationsInScope(m, undefined)).toBe(2);
  });

  it('restricts to the supplied scope id set', () => {
    const m = buildEscalationsByItem([
      open('e1', 'gm-a'),
      open('e2', 'gm-a'),
      open('e3', 'gm-b'),
    ]);
    expect(countEscalationsInScope(m, new Set(['gm-a']))).toBe(2);
    expect(countEscalationsInScope(m, new Set(['gm-b']))).toBe(1);
    expect(countEscalationsInScope(m, new Set(['gm-c']))).toBe(0);
  });
});
