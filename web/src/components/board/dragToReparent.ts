// Pure helpers for the Board's drag-to-reparent flow (gm-935r). The
// drop target is a milestone-option row in the MilestonePicker
// dropdown; dropping an epic card on it re-parents the epic under that
// milestone. The "All" option is the unset path — it clears parent_id.
//
// Mirrors dragToRestage's split: id encoding + a pure resolver so the
// drag→PATCH pipeline is unit-testable without dnd-kit pointer events.

import { MILESTONE_ALL, type MilestoneID } from './milestone';
import type { WorkItem } from '@/types/core.gen';

// Droppable id for a milestone-option row. Pipe delimiter avoids
// collisions with epic ids (which are workspace-prefixed and may
// contain slashes).
const OPTION_ID_PREFIX = 'milestone-option|';

export function milestoneOptionDroppableId(id: MilestoneID): string {
  return `${OPTION_ID_PREFIX}${id}`;
}

export function parseMilestoneOptionId(id: string): MilestoneID | null {
  if (!id.startsWith(OPTION_ID_PREFIX)) return null;
  return id.slice(OPTION_ID_PREFIX.length);
}

export interface ReparentInputs {
  activeID: string | number | null | undefined;
  overID: string | number | null | undefined;
  itemById: Map<string, WorkItem>;
}

export interface ReparentPatch {
  id: string;
  // parent_id three-state sentinel (gm-gsbj): "" clears, non-empty
  // re-parents. We never emit undefined here — that would mean "no
  // change" and we wouldn't have produced a patch in the first place.
  patch: { parent_id: string };
}

// resolveReparent turns a DragEnd into a parent-update PATCH, or null
// when the drop is a no-op (not over a milestone option, unknown item,
// already parented to that milestone). MILESTONE_ALL → empty string,
// which the API treats as "clear parent" (gm-gsbj).
export function resolveReparent({
  activeID,
  overID,
  itemById,
}: ReparentInputs): ReparentPatch | null {
  if (overID == null || activeID == null) return null;
  const target = parseMilestoneOptionId(String(overID));
  if (target == null) return null;
  const item = itemById.get(String(activeID));
  if (!item) return null;
  if (item.kind !== 'epic') return null;
  const nextParent = target === MILESTONE_ALL ? '' : target;
  // No-op when the epic's existing parent_child edge already points at
  // the target — the resolver finds the parent via the relationships
  // graph since WorkItem doesn't carry parent_id directly.
  const currentParent = currentParentId(item);
  if (currentParent === nextParent) return null;
  return { id: item.id, patch: { parent_id: nextParent } };
}

// currentParentId reads the existing parent_child edge from a
// WorkItem's relationships. Returns "" when the item has no parent.
function currentParentId(item: WorkItem): string {
  for (const r of item.relationships ?? []) {
    if (r.kind === 'parent_child' && r.to === item.id) return r.from;
  }
  return '';
}
