// EpicView (gm-root.6 / ui-spec §4): the default Board surface — Epic
// cards organized as swimlanes by parent-epic. Each swimlane is a row;
// inside the row the five StateCategory columns hold the Epic's cards.
//
// MVP scope: one swimlane mode (by parent-epic). Switcher for
// parallel-group / label / none deferred per the gm-root.6 follow-ups
// section.

import { useMemo } from 'react';
import {
  STATE_CATEGORIES,
  type StateCategory,
  type WorkItem,
} from '@/types/core.gen';
import { EpicCard, type EpicChildCounts } from './EpicCard';
import { groupEpicsByRoot, ORPHAN_ROOT_ID, type EpicSwimlane } from './epicHierarchy';

const COLUMN_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Unstarted',
  started: 'Started',
  completed: 'Completed',
  canceled: 'Canceled',
};

export interface EpicViewProps {
  items: WorkItem[];
  onSelectEpic: (id: string) => void;
}

export function EpicView({ items, onSelectEpic }: EpicViewProps) {
  const swimlanes = useMemo(() => groupEpicsByRoot(items), [items]);
  const childCountsByEpic = useMemo(() => buildChildCounts(items), [items]);

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

function ColumnHeader() {
  return (
    <div
      data-testid="board-epic-header"
      className="sticky top-0 z-10 flex border-b border-neutral-200 bg-white/95 px-4 py-2 backdrop-blur dark:border-neutral-800 dark:bg-neutral-950/95"
    >
      <div className="w-48 shrink-0" /> {/* swimlane label gutter */}
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
  // Bucket members by state so each column renders only its slice.
  const byState: Record<StateCategory, WorkItem[]> = {
    backlog: [],
    unstarted: [],
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
      className="flex border-b border-neutral-200 px-4 py-3 dark:border-neutral-800"
    >
      <div className="w-48 shrink-0 pr-3">
        <div
          className={
            'text-xs font-semibold uppercase tracking-wide ' +
            (isOrphan
              ? 'text-neutral-400 dark:text-neutral-500'
              : 'text-neutral-700 dark:text-neutral-300')
          }
        >
          {swimlane.root.title}
        </div>
        {!isOrphan && (
          <div className="mt-0.5 font-mono text-[11px] text-neutral-500">
            {swimlane.root.id}
          </div>
        )}
        <div className="mt-1 text-[11px] text-neutral-500">
          {swimlane.members.length} {swimlane.members.length === 1 ? 'epic' : 'epics'}
        </div>
      </div>
      <div className="flex flex-1 gap-3">
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
    byState: { backlog: 0, unstarted: 0, started: 0, completed: 0, canceled: 0 },
  };
}
