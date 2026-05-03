import { describe, expect, it } from 'vitest';
import type { WorkItem } from '@/types/core.gen';
import { sortWorkItems } from '../boardOrder';

function item(id: string, created_at: string, updated_at: string): WorkItem {
  return {
    id,
    kind: 'task',
    title: id,
    status: 'open',
    state_category: 'unstarted',
    created_at,
    updated_at,
  };
}

describe('boardOrder', () => {
  const rows = [
    item('gm-2', '2026-04-02T00:00:00Z', '2026-04-04T00:00:00Z'),
    item('gm-10', '2026-04-03T00:00:00Z', '2026-04-03T00:00:00Z'),
    item('gm-1', '2026-04-04T00:00:00Z', '2026-04-02T00:00:00Z'),
  ];

  it('sorts by created time newest first', () => {
    expect(sortWorkItems(rows, 'created').map((r) => r.id)).toEqual(['gm-1', 'gm-10', 'gm-2']);
  });

  it('sorts by modified time newest first', () => {
    expect(sortWorkItems(rows, 'modified').map((r) => r.id)).toEqual(['gm-2', 'gm-10', 'gm-1']);
  });

  it('sorts ids with numeric collation', () => {
    expect(sortWorkItems(rows, 'id').map((r) => r.id)).toEqual(['gm-1', 'gm-2', 'gm-10']);
  });
});
