// Milestone selector model (gm-l7hy). A separate axis from the
// Scope picker (gm-uekk): scope narrows to one epic's lineage,
// milestone narrows to one milestone's set of child epics + their
// descendants.
//
// URL param: ?milestone=<id>. Default = MILESTONE_ALL.
//
// The milestone bead itself is intentionally NOT included in the
// filtered set — clicking the milestone in the picker (or the "Show"
// affordance) opens the milestone bead in a side drawer instead
// (gm-98sq).

import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';
import { lineageIDs } from './scope';

// MILESTONE_ALL is the sentinel for "no milestone narrowing — every
// item is in scope." Default URL state.
export const MILESTONE_ALL = 'all';

// MilestoneID is either MILESTONE_ALL or a specific milestone work
// item's id.
export type MilestoneID = string;

// listMilestones returns every milestone-kind work item, sorted by
// the M<n> prefix in ascending numeric order. Items without an
// M<n> prefix are placed last in id order so an unnumbered legacy
// milestone still appears in the picker.
export function listMilestones(items: WorkItem[]): WorkItem[] {
  const out = items.filter((i) => i.kind === KIND_MILESTONE).slice();
  out.sort((a, b) => {
    const an = milestoneNumber(a.title);
    const bn = milestoneNumber(b.title);
    if (an !== bn) {
      // Unnumbered (0) sorts last; otherwise ascending.
      if (an === 0) return 1;
      if (bn === 0) return -1;
      return an - bn;
    }
    return a.id.localeCompare(b.id);
  });
  return out;
}

// milestoneNumber pulls the <n> off a `M<n>` title prefix. Returns 0
// when the title doesn't carry one.
export function milestoneNumber(title: string): number {
  const m = /^\s*M(\d+)\b/.exec(title);
  if (!m) return 0;
  const n = Number.parseInt(m[1] as string, 10);
  return Number.isFinite(n) ? n : 0;
}

// filterByMilestone returns the items inside the given milestone's
// tree, *excluding* the milestone bead itself. MILESTONE_ALL is a
// no-op pass-through.
export function filterByMilestone(items: WorkItem[], id: MilestoneID): WorkItem[] {
  if (id === MILESTONE_ALL) return items;
  const lineage = lineageIDs(items, id);
  return items.filter((i) => i.id !== id && lineage.has(i.id));
}

// MilestoneOption is one row in the picker's dropdown.
export interface MilestoneOption {
  id: MilestoneID;
  // label is the user-visible label. For MILESTONE_ALL this is "All";
  // otherwise it's the milestone's title (which carries its M<n>
  // prefix already, e.g. "M1 Beta release").
  label: string;
  // hint is the milestone bead id; rendered dim next to the label so
  // operators can deep-link via id.
  hint?: string;
}

// buildMilestoneOptions enumerates the picker rows: "All" + each
// milestone in M-number order.
export function buildMilestoneOptions(items: WorkItem[]): MilestoneOption[] {
  const out: MilestoneOption[] = [{ id: MILESTONE_ALL, label: 'All' }];
  for (const m of listMilestones(items)) {
    out.push({ id: m.id, label: m.title || m.id, hint: m.id });
  }
  return out;
}

// findMilestoneOption returns the option matching `id`, or undefined.
// Used by the picker to label its trigger from the canonical list.
export function findMilestoneOption(
  options: MilestoneOption[],
  id: MilestoneID,
): MilestoneOption | undefined {
  return options.find((o) => o.id === id);
}
