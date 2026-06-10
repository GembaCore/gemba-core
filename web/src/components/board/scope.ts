// Scope picker model (gm-uekk). Replaces the old RootEpicBanner +
// swimlane-mode dropdown with a single "Scope" axis: scope=all OR
// scope=<id-of-an-epic>. When a scope is set, the board narrows to
// the scope's *lineage*: the scope item itself plus every WorkItem
// reachable from it via parent_child edges (the scope's tree of
// descendants).
//
// One mental model: project > scope > layout > view-filter.

import type { WorkItem } from '@/types/core.gen';

// SCOPE_ALL is the sentinel value for "no narrowing — show every
// epic and every workitem the project surfaces." Default URL state.
export const SCOPE_ALL = 'all';

// ScopeID is either SCOPE_ALL or a specific WorkItem id (typically
// an epic). The picker lists root epics + their direct child epics
// as selectable scopes.
export type ScopeID = string;

// rootEpics returns the epics with no parent_child edge pointing at
// them from another epic — the top of the hierarchy. Same definition
// findRootEpics used in EpicView, lifted out so the ScopePicker can
// share it without importing from a default-export site.
export function rootEpics(items: WorkItem[]): WorkItem[] {
  const epics = items.filter((i) => i.kind === 'epic');
  const epicIDs = new Set(epics.map((e) => e.id));
  return epics.filter((e) => {
    return !(e.relationships ?? []).some(
      (r) => r.kind === 'parent_child' && r.to === e.id && epicIDs.has(r.from),
    );
  });
}

// childEpicsOf returns the epic-kind work items that name `parentID`
// as their parent via a parent_child edge. Pure; one-deep, not a
// recursive descent (the picker uses this to render one indent level
// under each root).
export function childEpicsOf(items: WorkItem[], parentID: string): WorkItem[] {
  return items.filter((i) => {
    if (i.kind !== 'epic') return false;
    return (i.relationships ?? []).some(
      (r) => r.kind === 'parent_child' && r.to === i.id && r.from === parentID,
    );
  });
}

// lineageIDs returns the set of WorkItem ids in `scopeID`'s lineage:
// the scope item itself plus every item reachable from it via
// parent_child edges. Used to filter the board's data array down to
// just the items the operator wants to see at this scope.
//
// Implementation: iteratively expand a frontier. Each round pulls in
// every item whose parent_child edge points from a member of the
// current set. Terminates when no new items are added — finite, no
// recursion, robust to relationship cycles.
export function lineageIDs(items: WorkItem[], scopeID: string): Set<string> {
  const out = new Set<string>([scopeID]);
  let added = true;
  while (added) {
    added = false;
    for (const it of items) {
      if (out.has(it.id)) continue;
      for (const r of it.relationships ?? []) {
        if (r.kind !== 'parent_child') continue;
        // r.from = parent, r.to = child (this item)
        if (r.to === it.id && out.has(r.from)) {
          out.add(it.id);
          added = true;
          break;
        }
      }
    }
  }
  return out;
}

// filterByScope returns the items belonging to `scopeID`'s lineage.
// SCOPE_ALL is a no-op pass-through. Stable order — the caller's
// sort survives.
export function filterByScope(items: WorkItem[], scopeID: ScopeID): WorkItem[] {
  if (scopeID === SCOPE_ALL) return items;
  const ids = lineageIDs(items, scopeID);
  return items.filter((i) => ids.has(i.id));
}

// ScopeOption is one row in the picker's dropdown. depth=0 = "All"
// or a root epic; depth=1 = a child epic indented under its root.
export interface ScopeOption {
  id: ScopeID;
  label: string;       // user-visible label (e.g. "All" or a title)
  hint?: string;       // small dim suffix (e.g. the workitem id)
  depth: 0 | 1;
}

// buildScopeOptions enumerates the picker's dropdown rows: "All"
// at the top, then each root epic, then that root's direct child
// epics indented underneath. Sorted by id within each tier so the
// rendered list is stable across reloads.
export function buildScopeOptions(items: WorkItem[]): ScopeOption[] {
  const options: ScopeOption[] = [
    { id: SCOPE_ALL, label: 'All', depth: 0 },
  ];
  const roots = rootEpics(items).slice().sort((a, b) => a.id.localeCompare(b.id));
  for (const root of roots) {
    options.push({
      id: root.id,
      label: root.title || root.id,
      hint: root.id,
      depth: 0,
    });
    const children = childEpicsOf(items, root.id)
      .slice()
      .sort((a, b) => a.id.localeCompare(b.id));
    for (const child of children) {
      options.push({
        id: child.id,
        label: child.title || child.id,
        hint: child.id,
        depth: 1,
      });
    }
  }
  return options;
}

// scopeOptionByID lets the picker label its trigger pill from the
// canonical list (so renaming a label in one place updates both
// trigger + dropdown).
export function findScopeOption(
  options: ScopeOption[],
  scopeID: ScopeID,
): ScopeOption | undefined {
  return options.find((o) => o.id === scopeID);
}
