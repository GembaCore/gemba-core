// Editability matrix (gm-root.8 → gm-root.8.1). Single source of truth
// for "should this drawer field render an edit affordance?" so the
// WorkItemDrawer doesn't sprinkle ad-hoc rules across components and
// the test suite can pin every cell of the matrix in one place.
//
// Rule:
//   - Adaptor read-only → nothing is editable.
//   - state_category === 'started' (in-progress) → nothing is editable.
//     Don't let the user race an agent that's mid-work.
//   - Any other state → every field is editable.
//
// This replaces the earlier per-field matrix; the drawer is now fully
// editable except while the bead is actively being worked.

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
  | 'dod'
  | 'custom';

export interface CanEditContext {
  item: WorkItem;
  // adaptorReadOnly is the manifest's read_only bit. Whole drawer goes
  // read-only when true (--dolt-url adaptor).
  adaptorReadOnly: boolean;
}

export function canEdit(_field: EditableField, ctx: CanEditContext): boolean {
  if (ctx.adaptorReadOnly) return false;
  if (ctx.item.state_category === 'started') return false;
  return true;
}
