// EpicView (gm-root.6 / ui-spec §4): the default Board surface — Epic
// cards organized into horizontal swimlanes. The swimlane partition
// is operator-selectable per ui-spec §4.4 (by-parent-epic | by-label |
// none); by-parallel-group is deferred until parallel groups exist as
// a backend concept.

import { useMemo } from 'react';
import {
  STATE_CATEGORIES,
  type StateCategory,
  type WorkItem,
} from '@/types/core.gen';
import { EpicCard, type EpicChildCounts } from './EpicCard';
import {
  groupEpicsAsSingle,
  groupEpicsByLabel,
  groupEpicsByRoot,
  ORPHAN_ROOT_ID,
  SINGLE_LANE_ID,
  type EpicSwimlane,
} from './epicHierarchy';
import { DEFAULT_SWIMLANE_MODE, type SwimlaneMode } from './swimlaneMode';

// Spec wording per ui-spec §4.3 (Backlog → Next Up → Staged → In
// Progress → Done → Canceled). The STATE_CATEGORIES order from
// core.gen.ts already matches.
const COLUMN_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

export interface EpicViewProps {
  items: WorkItem[];
  onSelectEpic: (id: string) => void;
  // mode selects how the Epic set is partitioned into rows. Defaults
  // to by-parent-epic so callers that haven't migrated yet keep the
  // old behaviour.
  mode?: SwimlaneMode;
}

export function EpicView({ items, onSelectEpic, mode = DEFAULT_SWIMLANE_MODE }: EpicViewProps) {
  const swimlanes = useMemo(() => {
    switch (mode) {
      case 'by-label':
        return groupEpicsByLabel(items);
      case 'none':
        return groupEpicsAsSingle(items);
      case 'by-parent-epic':
      default:
        return groupEpicsByRoot(items);
    }
  }, [items, mode]);
  const childCountsByEpic = useMemo(() => buildChildCounts(items), [items]);
  const rootEpics = useMemo(() => findRootEpics(items), [items]);

  if (swimlanes.length === 0) {
    return (
      <div
        data-testid="board-epic-empty"
        className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center"
      >
        <p className="text-sm text-neutral-600 dark:text-neutral-300">No Epics yet.</p>
        <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">
          Import from Jira or Beads, analyze an existing repo, or start fresh with the Onboarder.
        </p>
      </div>
    );
  }

  return (
    <div
      data-testid="board-epic"
      className="flex h-full flex-col overflow-y-auto"
    >
      <RootEpicBanner roots={rootEpics} onSelectEpic={onSelectEpic} />
      <ColumnHeader />
      <div className="flex flex-col">
        {swimlanes.map((s) => (
          <SwimlaneRow
            key={s.root.id}
            swimlane={s}
            childCountsByEpic={childCountsByEpic}
            onSelectEpic={onSelectEpic}
          />
        ))}
      </div>
    </div>
  );
}

// findRootEpics returns Epics that have no parent_child edge pointing to
// them from another Epic — i.e., the top of the hierarchy. Used for the
// board's top banner so the operator sees product-level context once,
// not repeated in every swimlane gutter.
function findRootEpics(items: WorkItem[]): WorkItem[] {
  const epics = items.filter((i) => i.kind === 'epic');
  const epicIDs = new Set(epics.map((e) => e.id));
  return epics.filter((e) => {
    return !(e.relationships ?? []).some(
      (r) => r.kind === 'parent_child' && r.to === e.id && epicIDs.has(r.from)
    );
  });
}

interface RootEpicBannerProps {
  roots: WorkItem[];
  onSelectEpic: (id: string) => void;
}

// RootEpicBanner renders the board's top-level roots as a thin strip so
// operators keep product-level context without the per-swimlane gutter
// eating horizontal space.
function RootEpicBanner({ roots, onSelectEpic }: RootEpicBannerProps) {
  if (roots.length === 0) return null;
  return (
    <div
      data-testid="board-root-epic-banner"
      className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-neutral-200 bg-white/50 px-4 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950/50"
    >
      {roots.map((r) => (
        <button
          key={r.id}
          type="button"
          data-testid={`board-root-epic-${r.id}`}
          onClick={() => onSelectEpic(r.id)}
          className="inline-flex items-baseline gap-2 text-left hover:underline focus:outline-none focus:ring-1 focus:ring-sky-500 rounded"
        >
          <span className="font-mono text-[11px] text-neutral-500">{r.id}</span>
          <span className="font-semibold uppercase tracking-wide text-neutral-700 dark:text-neutral-300">
            {r.title}
          </span>
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
            {r.status}
          </span>
        </button>
      ))}
    </div>
  );
}

function ColumnHeader() {
  return (
    <div
      data-testid="board-epic-header"
      className="sticky top-0 z-10 flex border-b border-neutral-200 bg-white/95 px-4 py-2 backdrop-blur dark:border-neutral-800 dark:bg-neutral-950/95"
    >
      <div className="flex flex-1 gap-3">
        {STATE_CATEGORIES.map((cat) => (
          <div
            key={cat}
            className="min-w-[14rem] flex-1 text-xs font-semibold uppercase tracking-wide text-neutral-600 dark:text-neutral-400"
          >
            {COLUMN_LABELS[cat]}
          </div>
        ))}
      </div>
    </div>
  );
}

interface SwimlaneRowProps {
  swimlane: EpicSwimlane;
  childCountsByEpic: Map<string, EpicChildCounts>;
  onSelectEpic: (id: string) => void;
}

function SwimlaneRow({ swimlane, childCountsByEpic, onSelectEpic }: SwimlaneRowProps) {
  const isOrphan = swimlane.root.id === ORPHAN_ROOT_ID;
  // The "none" mode returns a single synthetic swimlane labelled
  // "All epics" — suppress the row header so the layout reads as a
  // flat board rather than an awkwardly-labelled single lane.
  const isSingleLane = swimlane.root.id === SINGLE_LANE_ID;
  // Bucket members by state so each column renders only its slice.
  const byState: Record<StateCategory, WorkItem[]> = {
    backlog: [],
    unstarted: [],
    staged: [],
    started: [],
    completed: [],
    canceled: [],
  };
  for (const m of swimlane.members) {
    byState[m.state_category]?.push(m);
  }
  return (
    <section
      data-testid={`board-epic-swimlane-${swimlane.root.id}`}
      className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800"
    >
      {!isSingleLane && (
        <div className="mb-2 flex items-baseline gap-2 text-[11px] text-neutral-500">
          <span
            className={
              isOrphan
                ? 'italic text-neutral-400 dark:text-neutral-500'
                : 'font-mono text-neutral-600 dark:text-neutral-400'
            }
          >
            {isOrphan ? 'Orphan epics' : swimlane.root.id}
          </span>
          <span aria-hidden>·</span>
          <span>
            {swimlane.members.length} {swimlane.members.length === 1 ? 'epic' : 'epics'}
          </span>
        </div>
      )}
      <div className="flex gap-3">
        {STATE_CATEGORIES.map((cat) => (
          <div
            key={cat}
            data-testid={`board-epic-cell-${swimlane.root.id}-${cat}`}
            className="min-w-[14rem] flex-1 space-y-2"
          >
            {byState[cat].map((epicItem) => (
              <EpicCard
                key={epicItem.id}
                item={epicItem}
                childCounts={childCountsByEpic.get(epicItem.id) ?? emptyCounts()}
                onSelect={onSelectEpic}
              />
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}

// buildChildCounts pre-computes child counts so each EpicCard render
// doesn't re-walk the relationship graph. O(items) per call.
function buildChildCounts(items: WorkItem[]): Map<string, EpicChildCounts> {
  const out = new Map<string, EpicChildCounts>();
  // Collect direct children for every epic id we see in the dataset.
  const childrenByParent = new Map<string, WorkItem[]>();
  for (const it of items) {
    for (const r of it.relationships ?? []) {
      if (r.kind !== 'parent_child') continue;
      if (r.to !== it.id) continue;
      const list = childrenByParent.get(r.from) ?? [];
      list.push(it);
      childrenByParent.set(r.from, list);
    }
  }
  for (const [parentID, kids] of childrenByParent) {
    const counts = emptyCounts();
    for (const k of kids) {
      counts.byState[k.state_category] = (counts.byState[k.state_category] ?? 0) + 1;
      counts.total++;
    }
    out.set(parentID, counts);
  }
  return out;
}

function emptyCounts(): EpicChildCounts {
  return {
    total: 0,
    byState: { backlog: 0, unstarted: 0, staged: 0, started: 0, completed: 0, canceled: 0 },
  };
}
