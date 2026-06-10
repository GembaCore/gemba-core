// Tests for the JSONL import planner (gm-e12.3 DoD piece 2). Pure
// function, no React — so each case here is just (input, existing) →
// expected ImportPlan slices.

import { describe, expect, it } from 'vitest';
import { planJsonlImport } from '../jsonlImport';
import type { WorkItem } from '@/types/core.gen';

function wi(id: string, patch: Partial<WorkItem> = {}): WorkItem {
  return {
    id,
    kind: 'task',
    title: `title-${id}`,
    status: 'open',
    state_category: 'unstarted',
    created_at: '2026-04-24T00:00:00Z',
    updated_at: '2026-04-24T00:00:00Z',
    ...patch,
  };
}

describe('planJsonlImport', () => {
  it('classifies an unknown id as create and a known id as update', () => {
    const plan = planJsonlImport(
      [
        '{"title":"new thing","kind":"task"}',
        '{"id":"gm-1","status":"completed"}',
      ].join('\n'),
      [wi('gm-1')]
    );
    expect(plan.counts).toEqual({ create: 1, update: 1, skip: 0 });
    expect(plan.actions[0]).toMatchObject({
      kind: 'create',
      payload: { title: 'new thing', kind: 'task' },
    });
    expect(plan.actions[1]).toMatchObject({
      kind: 'update',
      id: 'gm-1',
      patch: { status: 'completed' },
    });
  });

  it('fills create-required defaults (kind, status, state_category) when omitted', () => {
    const plan = planJsonlImport('{"title":"sparse"}', []);
    expect(plan.counts).toEqual({ create: 1, update: 0, skip: 0 });
    if (plan.actions[0].kind === 'create') {
      expect(plan.actions[0].payload).toMatchObject({
        title: 'sparse',
        kind: 'task',
        status: 'open',
        state_category: 'backlog',
      });
    }
  });

  it('treats a row with an id that does not exist as a create', () => {
    const plan = planJsonlImport('{"id":"gm-999","title":"orphan"}', [wi('gm-1')]);
    expect(plan.counts).toEqual({ create: 1, update: 0, skip: 0 });
    expect(plan.actions[0]).toMatchObject({
      kind: 'create',
      payload: { title: 'orphan' },
    });
  });

  it('skips invalid JSON with a parse-error reason', () => {
    const plan = planJsonlImport('{not json}', []);
    expect(plan.counts).toEqual({ create: 0, update: 0, skip: 1 });
    expect(plan.actions[0]).toMatchObject({ kind: 'skip', line: 1 });
    if (plan.actions[0].kind === 'skip') {
      expect(plan.actions[0].reason).toMatch(/parse error/);
    }
  });

  it('skips a row that is not an object (array, string, null)', () => {
    const plan = planJsonlImport(['[]', '"hi"', 'null'].join('\n'), []);
    expect(plan.counts.skip).toBe(3);
  });

  it('skips a row containing unknown fields (typos must not silently drop)', () => {
    const plan = planJsonlImport('{"title":"x","priorty":1}', []);
    expect(plan.counts).toEqual({ create: 0, update: 0, skip: 1 });
    if (plan.actions[0].kind === 'skip') {
      expect(plan.actions[0].reason).toMatch(/unknown field/);
    }
  });

  it('skips a create that has no title', () => {
    const plan = planJsonlImport('{"kind":"task"}', []);
    expect(plan.counts).toEqual({ create: 0, update: 0, skip: 1 });
  });

  it('skips an update that has no fields to change', () => {
    const plan = planJsonlImport('{"id":"gm-1"}', [wi('gm-1')]);
    expect(plan.counts).toEqual({ create: 0, update: 0, skip: 1 });
  });

  it('ignores blank lines and trailing newlines', () => {
    const plan = planJsonlImport(
      '\n\n{"title":"a"}\n\n{"title":"b"}\n',
      []
    );
    expect(plan.counts).toEqual({ create: 2, update: 0, skip: 0 });
  });

  it('preserves line numbers in reported skips', () => {
    const plan = planJsonlImport(
      ['{"title":"ok"}', '{not json}', '{"title":"ok"}'].join('\n'),
      []
    );
    const skip = plan.actions.find((a) => a.kind === 'skip');
    expect(skip).toBeDefined();
    if (skip && skip.kind === 'skip') {
      expect(skip.line).toBe(2);
    }
  });

  it('drops user-supplied id on creates (adaptor owns the id namespace)', () => {
    const plan = planJsonlImport('{"id":"gm-999","title":"x"}', []);
    expect(plan.counts.create).toBe(1);
    if (plan.actions[0].kind === 'create') {
      // id is intentionally NOT on the WorkItemCreate shape — the
      // backend assigns it on the way back. Ensure our payload obeys.
      expect((plan.actions[0].payload as unknown as Record<string, unknown>).id).toBeUndefined();
    }
  });

  it('round-trips priority and labels', () => {
    const plan = planJsonlImport(
      '{"title":"x","priority":2,"labels":["a","b"]}',
      []
    );
    if (plan.actions[0].kind === 'create') {
      expect(plan.actions[0].payload).toMatchObject({
        priority: 2,
        labels: ['a', 'b'],
      });
    }
  });

  it('null priority on update is preserved (clears the field)', () => {
    const plan = planJsonlImport('{"id":"gm-1","priority":null}', [
      wi('gm-1', { priority: 3 }),
    ]);
    if (plan.actions[0].kind === 'update') {
      expect(plan.actions[0].patch.priority).toBeNull();
    }
  });
});
