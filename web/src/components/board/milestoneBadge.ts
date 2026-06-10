// Milestone badge model (gm-4se1). Pure helpers for the EpicCard's
// per-milestone color stripe + M-pill. The caller (EpicView) walks the
// graph once and threads the resolved milestone into each card; the
// card itself never re-walks.
//
// Why a separate file: gm-l7hy's milestone.ts is the picker model and
// is "settled" — keep this purely render-side concern out of it.

import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';

// findMilestoneForEpic walks parent_child edges upward from `epicID`
// until it finds an ancestor whose kind === KIND_MILESTONE, or runs
// out. Returns the milestone WorkItem, or undefined when none.
//
// Read-only: never mutates `items`. Cycle-safe via a visited set —
// parent_child cycles shouldn't exist but the walker is the wrong
// place to assume the data is clean.
export function findMilestoneForEpic(
  items: WorkItem[],
  epicID: string,
): WorkItem | undefined {
  const byID = new Map<string, WorkItem>();
  for (const it of items) byID.set(it.id, it);

  const visited = new Set<string>();
  let cursor: string | undefined = epicID;
  while (cursor && !visited.has(cursor)) {
    visited.add(cursor);
    const node = byID.get(cursor);
    if (!node) return undefined;
    if (node.id !== epicID && node.kind === KIND_MILESTONE) return node;
    // Find a parent edge: relationship with kind=parent_child whose
    // .to is this node — its .from is the parent's id.
    let nextParent: string | undefined;
    for (const r of node.relationships ?? []) {
      if (r.kind === 'parent_child' && r.to === node.id) {
        nextParent = r.from;
        break;
      }
    }
    cursor = nextParent;
  }
  return undefined;
}

// buildMilestoneByEpic precomputes the milestone (if any) for every
// epic in `items`. EpicView calls this once per render so each card's
// lookup is O(1) rather than O(items) per card.
export function buildMilestoneByEpic(items: WorkItem[]): Map<string, WorkItem> {
  const out = new Map<string, WorkItem>();
  for (const it of items) {
    if (it.kind !== 'epic') continue;
    const m = findMilestoneForEpic(items, it.id);
    if (m) out.set(it.id, m);
  }
  return out;
}

// MilestoneTone bundles the Tailwind utility classes for one palette
// slot. Static strings so the JIT can pick them up.
export interface MilestoneTone {
  // border-t-* class for the EpicCard's top stripe — the left edge
  // already carries the priority stripe (gm-4se1).
  stripe: string;
  // bg-* + text-* for the M-pill in the header. Same hue across light
  // and dark mode; saturation tuned per scheme.
  pill: string;
}

// PALETTE: 8 distinct hues spaced around the wheel. Hand-picked so
// adjacent slots are visually distinguishable. Order is stable —
// indices are addressed by hash, so rearranging would re-color every
// existing milestone.
const PALETTE: MilestoneTone[] = [
  {
    stripe: 'border-t-rose-500',
    pill: 'bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-300',
  },
  {
    stripe: 'border-t-amber-500',
    pill: 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  },
  {
    stripe: 'border-t-emerald-500',
    pill: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300',
  },
  {
    stripe: 'border-t-teal-500',
    pill: 'bg-teal-100 text-teal-800 dark:bg-teal-950 dark:text-teal-300',
  },
  {
    stripe: 'border-t-sky-500',
    pill: 'bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300',
  },
  {
    stripe: 'border-t-indigo-500',
    pill: 'bg-indigo-100 text-indigo-800 dark:bg-indigo-950 dark:text-indigo-300',
  },
  {
    stripe: 'border-t-fuchsia-500',
    pill: 'bg-fuchsia-100 text-fuchsia-800 dark:bg-fuchsia-950 dark:text-fuchsia-300',
  },
  {
    stripe: 'border-t-orange-500',
    pill: 'bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300',
  },
];

export const MILESTONE_PALETTE_SIZE = PALETTE.length;

// hashMilestoneID is a tiny string hash (FNV-1a 32-bit). Deterministic,
// fast, and good enough to spread ids across an 8-slot palette without
// needing a crypto dep.
export function hashMilestoneID(id: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < id.length; i++) {
    h ^= id.charCodeAt(i);
    // FNV prime 16777619; emulate uint32 with Math.imul + >>> 0.
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

// milestoneTone picks a palette slot from the milestone id. Same id →
// same tone, every render, every machine.
export function milestoneTone(id: string): MilestoneTone {
  const slot = hashMilestoneID(id) % PALETTE.length;
  return PALETTE[slot] as MilestoneTone;
}
