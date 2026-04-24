import { describe, expect, it } from 'vitest';
import { canEdit, type EditableField } from '../canEdit';
import type { WorkItem } from '@/types/core.gen';

function item(
  state: WorkItem['state_category'],
  assigneeKind?: 'human' | 'agent'
): WorkItem {
  return {
    id: 'gm-x',
    kind: 'task',
    title: 't',
    status: 'open',
    state_category: state,
    created_at: '',
    updated_at: '',
    assignee: assigneeKind
      ? { id: 'a', name: 'a', agent_kind: assigneeKind }
      : undefined,
  };
}

const ALL_FIELDS: EditableField[] = [
  'status',
  'description',
  'title',
  'priority',
  'labels',
  'sprint_id',
  'assignee',
  'owner',
  'dod',
];

describe('canEdit', () => {
  it('locks every field when the adaptor is read-only', () => {
    const ctx = { item: item('backlog', 'human'), adaptorReadOnly: true };
    for (const f of ALL_FIELDS) {
      expect(canEdit(f, ctx), `${f} must be locked when adaptor read-only`).toBe(false);
    }
  });

  it('locks every field while the bead is in progress (state_category === started)', () => {
    for (const kind of ['agent', 'human'] as const) {
      const ctx = { item: item('started', kind), adaptorReadOnly: false };
      for (const f of ALL_FIELDS) {
        expect(canEdit(f, ctx), `${f} must be locked while started (${kind})`).toBe(false);
      }
    }
  });

  it('unlocks every field in every non-started state when the adaptor is writable', () => {
    for (const state of ['backlog', 'unstarted', 'completed', 'canceled'] as const) {
      const ctx = { item: item(state), adaptorReadOnly: false };
      for (const f of ALL_FIELDS) {
        expect(canEdit(f, ctx), `${f} must be editable @ ${state}`).toBe(true);
      }
    }
  });
});
