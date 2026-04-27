// workItemViews unit tests (gm-uipx.18). Pure-data assertions
// covering the registry shape, lookup + alias resolution,
// post-filter / sort behavior, and the URL + localStorage
// migration helpers.

import { afterEach, describe, expect, it } from 'vitest';
import {
  WORK_ITEM_VIEWS,
  applyView,
  canonicaliseViewName,
  findView,
  migrateLegacyParams,
  readLegacyViewStorage,
  VIEW_PARAM,
  VIEW_STORAGE_KEY,
} from '../workItemViews';
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

describe('WORK_ITEM_VIEWS metadata', () => {
  it('ships the seven deduplicated views in canonical order', () => {
    const ids = WORK_ITEM_VIEWS.map((v) => v.id);
    expect(ids).toEqual([
      'staged',
      'in-progress',
      'blocked',
      'ready-to-stage',
      'backlog',
      'done-recent',
      'mine',
    ]);
  });

  it('every view declares a label and a defaultLayout', () => {
    for (const v of WORK_ITEM_VIEWS) {
      expect(v.label, `${v.id} label`).toBeTruthy();
      expect(v.defaultLayout, `${v.id} defaultLayout`).toBeTruthy();
    }
  });

  it('every view ids are unique even across aliases', () => {
    const seen = new Set<string>();
    for (const v of WORK_ITEM_VIEWS) {
      expect(seen.has(v.id), `dup id ${v.id}`).toBe(false);
      seen.add(v.id);
      for (const a of v.aliases ?? []) {
        expect(seen.has(a), `alias ${a} collides with another id`).toBe(false);
        seen.add(a);
      }
    }
  });
});

describe('findView + canonicaliseViewName', () => {
  it('resolves canonical ids', () => {
    expect(findView('staged')?.id).toBe('staged');
    expect(findView('done-recent')?.id).toBe('done-recent');
  });

  it('resolves aliases (recently-done → done-recent)', () => {
    expect(findView('recently-done')?.id).toBe('done-recent');
    expect(canonicaliseViewName('recently-done')).toBe('done-recent');
  });

  it('returns null for unknown names', () => {
    expect(findView('made-up')).toBeNull();
    expect(canonicaliseViewName('whatever')).toBeNull();
  });

  it('returns null for empty / null input', () => {
    expect(findView(null)).toBeNull();
    expect(findView(undefined)).toBeNull();
    expect(findView('')).toBeNull();
  });
});

describe('applyView post-filter + sort', () => {
  const ctx = { now: Date.parse('2026-04-26T12:00:00Z') };

  it('returns the input unchanged when view is null', () => {
    const items = [wi({ id: 'a' }), wi({ id: 'b' })];
    expect(applyView(items, null, ctx)).toBe(items);
  });

  it('applies post-filter for blocked (human_action_required)', () => {
    const items = [
      wi({ id: 'a', derived: { agent_claimable: false, human_action_required: true, review_pending: false } }),
      wi({ id: 'b', derived: { agent_claimable: false, human_action_required: false, review_pending: false } }),
      wi({ id: 'c' }),
    ];
    const view = findView('blocked')!;
    expect(applyView(items, view, ctx).map((i) => i.id)).toEqual(['a']);
  });

  it('done-recent filters by 7-day band against ctx.now', () => {
    const within = wi({
      id: 'in',
      state_category: 'completed',
      updated_at: '2026-04-23T00:00:00Z', // 3 days before ctx.now
    });
    const stale = wi({
      id: 'old',
      state_category: 'completed',
      updated_at: '2026-04-15T00:00:00Z', // 11 days before
    });
    const view = findView('done-recent')!;
    const out = applyView([within, stale], view, ctx).map((i) => i.id);
    expect(out).toEqual(['in']);
  });

  it('done-recent sorts newest first', () => {
    const items = [
      wi({ id: 'oldest', state_category: 'completed', updated_at: '2026-04-25T00:00:00Z' }),
      wi({ id: 'newest', state_category: 'completed', updated_at: '2026-04-26T11:00:00Z' }),
      wi({ id: 'mid',    state_category: 'completed', updated_at: '2026-04-26T03:00:00Z' }),
    ];
    const view = findView('done-recent')!;
    const out = applyView(items, view, ctx).map((i) => i.id);
    expect(out).toEqual(['newest', 'mid', 'oldest']);
  });

  it('mine matches by assignee.id OR assignee.name', () => {
    const items = [
      wi({ id: 'by-id',   assignee: { id: 'gemba/crew/mike3', name: 'mike3' } as any }),
      wi({ id: 'by-name', assignee: { id: 'something-else',    name: 'mike3' } as any }),
      wi({ id: 'other',   assignee: { id: 'someone',           name: 'someone' } as any }),
      wi({ id: 'unassigned' }),
    ];
    const view = findView('mine')!;
    const out = applyView(items, view, { ...ctx, currentAgent: 'mike3' }).map((i) => i.id);
    expect(out).toEqual(['by-id', 'by-name']);
  });

  it('mine returns nothing when currentAgent is empty', () => {
    const items = [wi({ id: 'a', assignee: { id: 'whoever', name: 'whoever' } as any })];
    const view = findView('mine')!;
    expect(applyView(items, view, { ...ctx, currentAgent: '' })).toEqual([]);
  });
});

describe('migrateLegacyParams', () => {
  it('rewrites ?preset=staged → ?view=staged', () => {
    const p = new URLSearchParams('preset=staged');
    expect(migrateLegacyParams(p)).toBe(true);
    expect(p.get(VIEW_PARAM)).toBe('staged');
    expect(p.has('preset')).toBe(false);
  });

  it('canonicalises ?view=recently-done → ?view=done-recent', () => {
    const p = new URLSearchParams('view=recently-done');
    expect(migrateLegacyParams(p)).toBe(true);
    expect(p.get(VIEW_PARAM)).toBe('done-recent');
  });

  it('drops unknown view names', () => {
    const p = new URLSearchParams('view=made-up');
    expect(migrateLegacyParams(p)).toBe(true);
    expect(p.has(VIEW_PARAM)).toBe(false);
  });

  it('is a no-op when nothing to migrate', () => {
    const p = new URLSearchParams('view=staged&q=auth');
    expect(migrateLegacyParams(p)).toBe(false);
    expect(p.get(VIEW_PARAM)).toBe('staged');
    expect(p.get('q')).toBe('auth');
  });

  it('canonical view wins over a legacy preset on the same URL', () => {
    // Should not happen in practice, but if both are present we
    // keep the canonical one and just drop the legacy key.
    const p = new URLSearchParams('view=mine&preset=staged');
    expect(migrateLegacyParams(p)).toBe(true);
    expect(p.get(VIEW_PARAM)).toBe('mine');
    expect(p.has('preset')).toBe(false);
  });

  it('migrates Board layout values from ?view= to ?layout=', () => {
    // Board's old URLs used ?view=epic|workitem|list for the
    // layout selection. After unification, ?view= is the
    // named-view slot and layout moves to ?layout=.
    for (const layout of ['epic', 'workitem', 'list']) {
      const p = new URLSearchParams(`view=${layout}`);
      expect(migrateLegacyParams(p)).toBe(true);
      expect(p.has(VIEW_PARAM)).toBe(false);
      expect(p.get('layout')).toBe(layout);
    }
  });

  it('explicit ?layout= survives a legacy ?view=epic', () => {
    const p = new URLSearchParams('view=epic&layout=workitem');
    expect(migrateLegacyParams(p)).toBe(true);
    expect(p.get('layout')).toBe('workitem');
    expect(p.has(VIEW_PARAM)).toBe(false);
  });

  it('?view=list&preset=backlog migrates to ?layout=list&view=backlog', () => {
    // The /backlog redirect target. Both halves should land in the
    // right slot after migration.
    const p = new URLSearchParams('view=list&preset=backlog');
    expect(migrateLegacyParams(p)).toBe(true);
    expect(p.get('layout')).toBe('list');
    expect(p.get(VIEW_PARAM)).toBe('backlog');
  });
});

describe('readLegacyViewStorage', () => {
  function fakeStorage(initial: Record<string, string> = {}): Storage {
    const data: Record<string, string> = { ...initial };
    return {
      getItem: (k) => (k in data ? data[k] : null),
      setItem: (k, v) => {
        data[k] = v;
      },
      removeItem: (k) => {
        delete data[k];
      },
      clear: () => {
        for (const k of Object.keys(data)) delete data[k];
      },
      key: (i) => Object.keys(data)[i] ?? null,
      get length() {
        return Object.keys(data).length;
      },
    } as Storage;
  }

  afterEach(() => {
    // Defensive: tests use injected storage so no global state to reset,
    // but documenting the intent for future readers.
  });

  it('returns null when nothing is stored', () => {
    expect(readLegacyViewStorage(fakeStorage())).toBeNull();
  });

  it('reads the canonical key when set', () => {
    const s = fakeStorage({ [VIEW_STORAGE_KEY]: 'staged' });
    expect(readLegacyViewStorage(s)).toBe('staged');
  });

  it('migrates from gemba.grid.view → gemba.workitem.view on first read', () => {
    const s = fakeStorage({ 'gemba.grid.view': 'in-progress' });
    expect(readLegacyViewStorage(s)).toBe('in-progress');
    expect(s.getItem(VIEW_STORAGE_KEY)).toBe('in-progress');
  });

  it('canonicalises an alias on read (recently-done → done-recent)', () => {
    const s = fakeStorage({ 'gemba.grid.view': 'recently-done' });
    expect(readLegacyViewStorage(s)).toBe('done-recent');
    expect(s.getItem(VIEW_STORAGE_KEY)).toBe('done-recent');
  });

  it('canonicalises a stale canonical entry (recently-done → done-recent)', () => {
    // An operator with the alias already migrated into the canonical
    // key (e.g. by an old build) gets it re-canonicalised on next read.
    const s = fakeStorage({ [VIEW_STORAGE_KEY]: 'recently-done' });
    expect(readLegacyViewStorage(s)).toBe('done-recent');
    expect(s.getItem(VIEW_STORAGE_KEY)).toBe('done-recent');
  });

  it('returns null when an unknown view is stored', () => {
    const s = fakeStorage({ [VIEW_STORAGE_KEY]: 'made-up' });
    expect(readLegacyViewStorage(s)).toBeNull();
  });
});
