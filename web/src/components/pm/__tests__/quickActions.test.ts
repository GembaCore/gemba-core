// Unit tests for the route → quick-action mapping (gm-96l).
// Pin the catalog so a future route change can't silently drop a
// verb the operator depends on.

import { describe, expect, it } from 'vitest';
import { labelPrefixForVariety, quickActionsForPath } from '../quickActions';

describe('quickActionsForPath', () => {
  it('returns plan-staging verbs on /board', () => {
    const ids = quickActionsForPath('/board').map((a) => a.id);
    expect(ids).toEqual(['recommend-order', 'what-remains', 'trim-to-budget']);
  });

  it('returns plan-staging verbs on a deep /board/:id route', () => {
    const ids = quickActionsForPath('/board/gemba/gemba/gm-e1').map((a) => a.id);
    expect(ids).toEqual(['recommend-order', 'what-remains', 'trim-to-budget']);
  });

  it('returns Triage on /escalations', () => {
    const actions = quickActionsForPath('/escalations');
    expect(actions).toHaveLength(1);
    expect(actions[0].id).toBe('triage');
  });

  it('returns no actions on /walk so the takeover owns the body', () => {
    expect(quickActionsForPath('/walk')).toEqual([]);
  });

  it('returns the generic Ask PM action on unknown routes', () => {
    const actions = quickActionsForPath('/insights');
    expect(actions).toHaveLength(1);
    expect(actions[0].id).toBe('ask-pm');
  });

  it('returns the generic action on root', () => {
    const actions = quickActionsForPath('/');
    expect(actions).toHaveLength(1);
    expect(actions[0].id).toBe('ask-pm');
  });

  it('every action carries a non-undefined prompt seed', () => {
    const all = [
      ...quickActionsForPath('/board'),
      ...quickActionsForPath('/escalations'),
      ...quickActionsForPath('/'),
    ];
    for (const a of all) {
      expect(typeof a.prompt).toBe('string');
    }
  });
});

describe('labelPrefixForVariety', () => {
  it('maps coach to Ask', () => {
    expect(labelPrefixForVariety('coach')).toBe('Ask');
  });
  it('maps manager to Consult', () => {
    expect(labelPrefixForVariety('manager')).toBe('Consult');
  });
  it('maps architect to Consult', () => {
    expect(labelPrefixForVariety('architect')).toBe('Consult');
  });
  it('falls back to Ask for unknown variety', () => {
    expect(labelPrefixForVariety(undefined)).toBe('Ask');
  });
});
