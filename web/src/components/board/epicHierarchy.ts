// Pure helpers for grouping Epics into swimlanes by their root ancestor.
// gm-root.6 / ui-spec §4.4: "by parent-epic (Epics grouped by their root
// epic; orphans in their own group)".
//
// Walks WorkItem.relationships to find each Epic's parent_child chain,
// stops at the first ancestor that itself has no parent (or whose parent
// isn't in the input set — orphans become their own swimlane).
//
// All functions are pure / side-effect-free so they can be unit-tested
// without React.

import type { WorkItem, WorkItemID } from '@/types/core.gen';

const EPIC_KIND = 'epic';

// isEpic — single source of truth for "is this a swimlane-eligible item".
// Centralised so a future "milestone counts as epic" rule is one diff.
export function isEpic(item: WorkItem): boolean {
  return item.kind === EPIC_KIND;
}

export interface EpicSwimlane {
  // root is the epic at the top of the chain. May equal a card in `members`
  // when the root is itself rendered (top-level epic with no parent).
  root: WorkItem;
  // members are every Epic in this swimlane, INCLUDING the root when it's
  // a top-level Epic. Order: root first, then descendants in BFS order.
  members: WorkItem[];
}

// ORPHAN_ROOT_ID is the synthetic id for the catch-all swimlane that
// holds Epics whose parent is unknown (parent_child edge points to a
// non-Epic, or to an Epic not in the current dataset). Distinct from
// any real bead id.
export const ORPHAN_ROOT_ID: WorkItemID = '__orphans__';

// groupEpicsByRoot returns one swimlane per top-level Epic, plus an
// optional orphan swimlane for Epics whose parent isn't an Epic in the
// input. Non-Epics in `items` are ignored — they're either rendered in
// the WorkItem-flat view or as children inside a drawer.
export function groupEpicsByRoot(items: WorkItem[]): EpicSwimlane[] {
  const epics = items.filter(isEpic);
  if (epics.length === 0) return [];

  const byID = new Map<WorkItemID, WorkItem>();
  for (const e of epics) byID.set(e.id, e);

  // parentOf maps an Epic id to the id of its parent Epic, when one
  // exists in the input set. Edges to non-Epics or to ids missing from
  // the set are ignored — those Epics end up in the orphan swimlane.
  const parentOf = new Map<WorkItemID, WorkItemID>();
  for (const e of epics) {
    const parent = findParentInSet(e, byID);
    if (parent) parentOf.set(e.id, parent);
  }

  // Walk parents to find each Epic's root. Cache so repeated lookups in
  // the same pass don't re-walk the chain.
  const rootCache = new Map<WorkItemID, WorkItemID | null>();
  function rootOf(id: WorkItemID): WorkItemID | null {
    if (rootCache.has(id)) return rootCache.get(id) ?? null;
    const seen = new Set<WorkItemID>();
    let cur: WorkItemID | undefined = id;
    while (cur) {
      if (seen.has(cur)) {
        // Cycle in the parent chain — treat as orphan rather than loop forever.
        rootCache.set(id, null);
        return null;
      }
      seen.add(cur);
      const parent = parentOf.get(cur);
      if (!parent) {
        rootCache.set(id, cur);
        return cur;
      }
      cur = parent;
    }
    rootCache.set(id, null);
    return null;
  }

  // Bucket every Epic under its root id (or ORPHAN_ROOT_ID).
  //   - If the Epic has no parent_child edge at all → top-level Epic;
  //     bucket under itself.
  //   - If it has a parent_child edge whose `from` resolves to an Epic
  //     in the set → walk up to the root and bucket there.
  //   - If it has a parent_child edge but the parent is missing from the
  //     set OR isn't an Epic OR the chain cycles → orphan.
  const buckets = new Map<WorkItemID, WorkItem[]>();
  for (const e of epics) {
    const hasAnyParentEdge = (e.relationships ?? []).some(
      (r) => r.kind === 'parent_child' && r.to === e.id
    );
    let bucketID: WorkItemID;
    if (!hasAnyParentEdge) {
      bucketID = e.id;
    } else if (parentOf.has(e.id)) {
      const root = rootOf(e.id);
      bucketID = root && byID.has(root) ? root : ORPHAN_ROOT_ID;
    } else {
      // Has a parent_child edge but the parent isn't in the Epic set.
      bucketID = ORPHAN_ROOT_ID;
    }
    const list = buckets.get(bucketID) ?? [];
    list.push(e);
    buckets.set(bucketID, list);
  }

  const swimlanes: EpicSwimlane[] = [];
  for (const [rootID, members] of buckets) {
    const root =
      rootID === ORPHAN_ROOT_ID
        ? syntheticOrphanRoot()
        : (byID.get(rootID) ?? syntheticOrphanRoot());
    members.sort(memberOrder(root.id));
    swimlanes.push({ root, members });
  }
  // Stable order: orphans last, real roots alphabetically by id so the
  // board layout doesn't jitter between renders.
  swimlanes.sort((a, b) => {
    if (a.root.id === ORPHAN_ROOT_ID) return 1;
    if (b.root.id === ORPHAN_ROOT_ID) return -1;
    return a.root.id.localeCompare(b.root.id);
  });
  return swimlanes;
}

function findParentInSet(
  item: WorkItem,
  set: Map<WorkItemID, WorkItem>
): WorkItemID | undefined {
  for (const r of item.relationships ?? []) {
    if (r.kind !== 'parent_child') continue;
    // parent_child semantics (core/types.go RelParentChild): from=parent,
    // to=child. So this Epic's parent is `from` when `to===this.id`.
    if (r.to === item.id && set.has(r.from)) {
      return r.from;
    }
  }
  return undefined;
}

function memberOrder(rootID: WorkItemID) {
  return (a: WorkItem, b: WorkItem) => {
    // Root first so the swimlane header card stays prominent.
    if (a.id === rootID) return -1;
    if (b.id === rootID) return 1;
    return a.id.localeCompare(b.id);
  };
}

function syntheticOrphanRoot(): WorkItem {
  return {
    id: ORPHAN_ROOT_ID,
    kind: EPIC_KIND,
    title: 'Orphan epics',
    status: 'open',
    state_category: 'unstarted',
    created_at: '',
    updated_at: '',
  };
}

// epicChildren returns the WorkItems whose parent_child edge points to
// the given epic id. Used by the EpicDetail tab to list child WorkItems
// without re-fetching.
export function epicChildren(items: WorkItem[], epicID: WorkItemID): WorkItem[] {
  const out: WorkItem[] = [];
  for (const it of items) {
    if (it.id === epicID) continue;
    for (const r of it.relationships ?? []) {
      if (r.kind === 'parent_child' && r.from === epicID && r.to === it.id) {
        out.push(it);
        break;
      }
    }
  }
  return out;
}

// SINGLE_LANE_ID is the synthetic root id used when the operator picks
// "no swimlanes" — every epic falls into one bucket and the row gutter
// renders empty.
export const SINGLE_LANE_ID: WorkItemID = '__all__';

// groupEpicsAsSingle returns one swimlane containing every Epic in the
// input. Used for the "none" swimlane mode (ui-spec §4.4) — same layout
// as the multi-lane modes but the row gutter stays blank.
export function groupEpicsAsSingle(items: WorkItem[]): EpicSwimlane[] {
  const epics = items.filter(isEpic);
  if (epics.length === 0) return [];
  const sorted = [...epics].sort((a, b) => a.id.localeCompare(b.id));
  return [
    {
      root: {
        id: SINGLE_LANE_ID,
        kind: EPIC_KIND,
        title: 'All epics',
        status: 'open',
        state_category: 'unstarted',
        created_at: '',
        updated_at: '',
      },
      members: sorted,
    },
  ];
}

// groupEpicsByLabel returns one swimlane per distinct label that
// appears on at least one Epic. Epics carrying multiple labels show
// up in every matching swimlane (the spec's by-label mode is union,
// not partition — operators want to see "all the epics tagged
// `risk:high`" without wondering which lane stole them). Epics with
// no labels collapse into the ORPHAN_ROOT_ID swimlane so they're
// visible.
export function groupEpicsByLabel(items: WorkItem[]): EpicSwimlane[] {
  const epics = items.filter(isEpic);
  if (epics.length === 0) return [];
  const byLabel = new Map<string, WorkItem[]>();
  const orphans: WorkItem[] = [];
  for (const e of epics) {
    const labels = e.labels ?? [];
    if (labels.length === 0) {
      orphans.push(e);
      continue;
    }
    for (const l of labels) {
      const list = byLabel.get(l) ?? [];
      list.push(e);
      byLabel.set(l, list);
    }
  }
  const swimlanes: EpicSwimlane[] = [];
  for (const [label, members] of byLabel) {
    members.sort((a, b) => a.id.localeCompare(b.id));
    swimlanes.push({
      root: {
        id: label,
        kind: EPIC_KIND,
        title: label,
        status: 'open',
        state_category: 'unstarted',
        created_at: '',
        updated_at: '',
      },
      members,
    });
  }
  swimlanes.sort((a, b) => a.root.id.localeCompare(b.root.id));
  if (orphans.length > 0) {
    orphans.sort((a, b) => a.id.localeCompare(b.id));
    swimlanes.push({
      root: {
        id: ORPHAN_ROOT_ID,
        kind: EPIC_KIND,
        title: 'No labels',
        status: 'open',
        state_category: 'unstarted',
        created_at: '',
        updated_at: '',
      },
      members: orphans,
    });
  }
  return swimlanes;
}

