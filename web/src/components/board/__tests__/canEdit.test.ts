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

describe('canEdit', () => {
  it('locks every field when the adaptor is read-only', () => {
    const ctx = { item: item('backlog', 'human'), adaptorReadOnly: true };
    const fields: EditableField[] = [
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
    for (const f of fields) {
      expect(canEdit(f, ctx), `${f} must be locked when adaptor read-only`).toBe(false);
    }
  });

  it('always allows status + description when writable', () => {
    for (const state of ['backlog', 'unstarted', 'started', 'completed', 'canceled'] as const) {
      const ctx = { item: item(state), adaptorReadOnly: false };
      expect(canEdit('status', ctx), `status @ ${state}`).toBe(true);
      expect(canEdit('description', ctx), `description @ ${state}`).toBe(true);
    }
  });

  it('locks title-group on agent-assigned `started`; unlocks elsewhere', () => {
    // Started + agent → locked (don't race the agent).
    expect(canEdit('title', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);
    expect(canEdit('priority', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);
    expect(canEdit('labels', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);
    expect(canEdit('sprint_id', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);
    expect(canEdit('dod', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);

    // Same state but human-assigned → editable (operator owns it).
    expect(canEdit('title', { item: item('started', 'human'), adaptorReadOnly: false })).toBe(true);

    // Other states with agent assignee → editable (agent isn't mid-work).
    for (const state of ['backlog', 'unstarted', 'completed', 'canceled'] as const) {
      expect(
        canEdit('title', { item: item(state, 'agent'), adaptorReadOnly: false }),
        `title @ ${state}`
      ).toBe(true);
    }
  });

  it('locks assignee/owner outside of staged states unless human-assigned', () => {
    // Started + agent → locked (reassigning mid-flight breaks the agent).
    expect(canEdit('assignee', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);
    expect(canEdit('owner', { item: item('started', 'agent'), adaptorReadOnly: false })).toBe(false);
    expect(canEdit('assignee', { item: item('completed', 'agent'), adaptorReadOnly: false })).toBe(false);

    // Staged-only states → editable.
    expect(canEdit('assignee', { item: item('backlog', 'agent'), adaptorReadOnly: false })).toBe(true);
    expect(canEdit('assignee', { item: item('unstarted', 'agent'), adaptorReadOnly: false })).toBe(true);

    // Human-assigned override.
    expect(canEdit('assignee', { item: item('started', 'human'), adaptorReadOnly: false })).toBe(true);
  });
});
