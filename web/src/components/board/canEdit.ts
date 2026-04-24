// Editability matrix (gm-root.8). Single source of truth for "should
// this drawer field render an edit affordance?" so the WorkItemDrawer
// doesn't sprinkle ad-hoc rules across components and the test suite
// can pin every cell of the matrix in one place.
//
// Rules (per gm-root.8 design):
//   - status / state_category    → always editable when adaptor writable
//   - description (the "notes")  → always editable when adaptor writable
//   - title / priority / labels / sprint_id
//                                → state ∈ {backlog, unstarted,
//                                   completed, canceled} OR human-assigned
//   - assignee / owner           → state ∈ {backlog, unstarted}
//                                   OR human-assigned
//   - dod, custom (excl. beads:notes)
//                                → follows title group
//
// Why: don't let a user race an agent that's mid-edit on a `started`
// work item, but never block the user when they own the bead
// themselves (`assignee.agent_kind === 'human'`).

import type { WorkItem } from '@/types/core.gen';

export type EditableField =
  | 'status'
  | 'description'
  | 'title'
  | 'priority'
  | 'labels'
  | 'sprint_id'
  | 'assignee'
  | 'owner'
  | 'dod';

export interface CanEditContext {
  item: WorkItem;
  // adaptorReadOnly is the manifest's read_only bit. Whole drawer goes
  // read-only when true (--dolt-url adaptor).
  adaptorReadOnly: boolean;
}

const STAGED_OR_DONE: WorkItem['state_category'][] = [
  'backlog',
  'unstarted',
  'completed',
  'canceled',
];

const STAGED_ONLY: WorkItem['state_category'][] = ['backlog', 'unstarted'];

function isHumanAssigned(item: WorkItem): boolean {
  return item.assignee?.agent_kind === 'human';
}

export function canEdit(field: EditableField, ctx: CanEditContext): boolean {
  if (ctx.adaptorReadOnly) return false;
  const { item } = ctx;
  switch (field) {
    case 'status':
    case 'description':
      return true;
    case 'title':
    case 'priority':
    case 'labels':
    case 'sprint_id':
    case 'dod':
      return STAGED_OR_DONE.includes(item.state_category) || isHumanAssigned(item);
    case 'assignee':
    case 'owner':
      return STAGED_ONLY.includes(item.state_category) || isHumanAssigned(item);
    default: {
      // Exhaustiveness check — a future field added to EditableField
      // surfaces here as a TS error rather than silently defaulting
      // to "no edit affordance".
      const _exhaustive: never = field;
      return _exhaustive;
    }
  }
}
