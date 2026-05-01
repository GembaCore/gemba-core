// Pure helpers for the Board's drag-to-restage flow (gm-75u / ui-spec
// §4.5). Split out of EpicView so the drag→PATCH pipeline can be unit
// tested without mounting the component, and to keep EpicView's
// react-refresh surface to components only.

import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';

// Droppable-cell id encoding. Cell ids need to round-trip a rootID
// (which itself may contain slashes — bd prefixes every id with
// "<workspace>/<repo>/") and a StateCategory. Using a pipe delimiter
// avoids collisions with any id the workspace prefix can produce.
const CELL_ID_PREFIX = 'cell|';

export function cellId(rootID: string, cat: StateCategory): string {
  return `${CELL_ID_PREFIX}${rootID}|${cat}`;
}

export function parseCellId(id: string): { rootID: string; cat: StateCategory } | null {
  if (!id.startsWith(CELL_ID_PREFIX)) return null;
  const rest = id.slice(CELL_ID_PREFIX.length);
  const lastPipe = rest.lastIndexOf('|');
  if (lastPipe < 0) return null;
  const rootID = rest.slice(0, lastPipe);
  const cat = rest.slice(lastPipe + 1) as StateCategory;
  if (!STATE_CATEGORIES.includes(cat)) return null;
  return { rootID, cat };
}

// resolveRestage turns a DndContext DragEnd into a concrete PATCH
// payload, or null when the drop is a no-op (same column, unknown
// over-target, unknown item). Pulled out of the component so the
// drag→mutation pipeline can be asserted directly in tests without
// driving jsdom-hostile pointer events through dnd-kit.
export interface RestageInputs {
  activeID: string | number | null | undefined;
  overID: string | number | null | undefined;
  itemById: Map<string, WorkItem>;
}

export interface RestagePatch {
  id: string;
  patch: { state_category: StateCategory };
}

export function resolveRestage({
  activeID,
  overID,
  itemById,
}: RestageInputs): RestagePatch | null {
  if (overID == null || activeID == null) return null;
  const parsed = parseCellId(String(overID));
  if (!parsed) return null;
  const item = itemById.get(String(activeID));
  if (!item) return null;
  if (item.state_category === parsed.cat) return null;
  // The board visually collapses Next Up (`unstarted`) and Staged into
  // the Ready column. Dropping an already-ready item back into that
  // visual column should not silently stage it; staging still happens
  // through wrapper propulsion or explicit detail actions.
  if (parsed.cat === 'staged' && item.state_category === 'unstarted') return null;
  return { id: item.id, patch: { state_category: parsed.cat } };
}

// shouldAutoStartSession encodes the leaf rule: a successful
// drag-to-restage into "In Progress" on a runnable bead kind is the
// operator saying "go do this work." Wrappers use cascade dispatch
// instead; they are never sent to an agent as the unit of work.
//
// gm-root.25 originally included `epic`; gm-1o9n moves wrapper starts
// to shouldCascadeDispatch(), leaving this predicate for runnable leaf
// kinds (`task`, `bug`, `feature`).
export const AUTOSTART_KINDS = ['task', 'bug', 'feature'] as const;
export const CASCADE_KINDS = ['epic', 'milestone'] as const;

type AutoStartKind = (typeof AUTOSTART_KINDS)[number];
type CascadeKind = (typeof CASCADE_KINDS)[number];

export function shouldAutoStartSession(item: WorkItem): boolean {
  return (
    AUTOSTART_KINDS.includes(item.kind as AutoStartKind) &&
    item.state_category === 'started'
  );
}

export function shouldCascadeDispatch(item: WorkItem): boolean {
  return (
    CASCADE_KINDS.includes(item.kind as CascadeKind) &&
    item.state_category === 'started'
  );
}
