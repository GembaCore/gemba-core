// EpicView (gm-root.6 / ui-spec §4): the default Board surface — Epic
// cards organized into horizontal swimlanes. The swimlane partition
// is operator-selectable per ui-spec §4.4 (by-parent-epic | by-label |
// none); by-parallel-group is deferred until parallel groups exist as
// a backend concept.
//
// Drag-to-restage (gm-75u / ui-spec §4.5): whole card is draggable;
// dropping on a different column fires a PATCH state_category via
// useUpdateWorkItem which carries the X-GEMBA-Confirm nonce
// (gm-root.8 slice 2). Reorder-within-column isn't wired — needs a
// UserOrder backend field (separate follow-up).

import { useCallback, useMemo } from 'react';
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import {
  STATE_CATEGORIES,
  type StateCategory,
  type WorkItem,
} from '@/types/core.gen';
import { useUpdateWorkItem } from '@/hooks/useWorkItems';
import { useAgents } from '@/hooks/useAgents';
import { useStartSession } from '@/hooks/useSessions';
import { EpicCard, type EpicChildCounts } from './EpicCard';
import {
  groupEpicsByRoot,
  ORPHAN_ROOT_ID,
  type EpicSwimlane,
} from './epicHierarchy';
import { cellId, resolveRestage, shouldAutoStartSession } from './dragToRestage';
import { buildMilestoneByEpic } from './milestoneBadge';

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
}

export function EpicView({ items, onSelectEpic }: EpicViewProps) {
  // gm-uekk: scope filtering happens in the page; the view always
  // groups by-parent-epic. When scope is set, items is already
  // narrowed and naturally collapses to a single swimlane.
  const swimlanes = useMemo(() => {
    return groupEpicsByRoot(items);
  }, [items]);
  const childCountsByEpic = useMemo(() => buildChildCounts(items), [items]);
  // gm-4se1: resolve each epic's milestone ancestor once per render so
  // the cards don't re-walk the relationship graph individually.
  const milestoneByEpic = useMemo(() => buildMilestoneByEpic(items), [items]);

  // PointerSensor requires a 4px move before starting a drag so a plain
  // double-click doesn't accidentally begin a drag gesture. KeyboardSensor
  // keeps the card navigable + draggable for operators without a pointer.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor)
  );

  const updateWorkItem = useUpdateWorkItem();
  const startSession = useStartSession();
  const { data: agents = [] } = useAgents();
  // Pick a default agent dialect for auto-dispatch on drag-to-started.
  // Mirrors NewSessionDialog's fallback so operators see consistent
  // behavior whether they drag or use the manual dialog.
  const defaultAgentType = useMemo(() => {
    for (const a of agents) {
      if (a.dialect) return a.dialect;
    }
    return 'claude';
  }, [agents]);
  // Index items by id so the drag handler can read the source
  // state_category without re-walking the list.
  const itemById = useMemo(() => {
    const m = new Map<string, WorkItem>();
    for (const it of items) m.set(it.id, it);
    return m;
  }, [items]);

  const onDragEnd = useCallback(
    (event: DragEndEvent) => {
      const patch = resolveRestage({
        activeID: event.active.id,
        overID: event.over?.id,
        itemById,
      });
      if (!patch) return;
      updateWorkItem.mutate(patch, {
        onSuccess: (updated) => {
          // Drag-to-In-Progress on an epic means "go do this work" —
          // dispatch a session. Backend dedups by nonce.
          if (!shouldAutoStartSession(updated)) return;
          startSession.mutate({
            bead_id: updated.id,
            agent_type: defaultAgentType,
          });
        },
      });
    },
    [itemById, updateWorkItem, startSession, defaultAgentType]
  );

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
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
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
              milestoneByEpic={milestoneByEpic}
              onSelectEpic={onSelectEpic}
            />
          ))}
        </div>
      </div>
    </DndContext>
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
  milestoneByEpic: Map<string, WorkItem>;
  onSelectEpic: (id: string) => void;
}

function SwimlaneRow({
  swimlane,
  childCountsByEpic,
  milestoneByEpic,
  onSelectEpic,
}: SwimlaneRowProps) {
  const isOrphan = swimlane.root.id === ORPHAN_ROOT_ID;
  // gm-uekk: scope-driven filtering may leave only one swimlane —
  // suppress its label since it would just repeat the scope pill.
  const isSingleLane = false;
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
          <DroppableCell key={cat} rootID={swimlane.root.id} cat={cat}>
            {byState[cat].map((epicItem) => (
              <DraggableEpicCard
                key={epicItem.id}
                item={epicItem}
                childCounts={childCountsByEpic.get(epicItem.id) ?? emptyCounts()}
                milestone={milestoneByEpic.get(epicItem.id)}
                onSelect={onSelectEpic}
              />
            ))}
          </DroppableCell>
        ))}
      </div>
    </section>
  );
}

// DroppableCell is the column-per-swimlane target. The cell id encodes
// the (rootID, stateCategory) pair so onDragEnd can route the drop to
// the right PATCH.
interface DroppableCellProps {
  rootID: string;
  cat: StateCategory;
  children: React.ReactNode;
}
function DroppableCell({ rootID, cat, children }: DroppableCellProps) {
  const { setNodeRef, isOver } = useDroppable({ id: cellId(rootID, cat) });
  return (
    <div
      ref={setNodeRef}
      data-testid={`board-epic-cell-${rootID}-${cat}`}
      data-drop-over={isOver || undefined}
      className={
        'min-w-[14rem] flex-1 space-y-2 rounded-sm transition-colors ' +
        (isOver ? 'bg-sky-50 dark:bg-sky-950/40' : '')
      }
    >
      {children}
    </div>
  );
}

// DraggableEpicCard wraps the existing EpicCard with the dnd-kit
// pointer/keyboard drag handle. The card itself is the handle so the
// whole surface is grabbable (ui-spec §4.5). While dragging, the card
// gets a visual lift via translate + higher z-index; the original
// space is preserved so other cards don't reflow.
interface DraggableEpicCardProps {
  item: WorkItem;
  childCounts: EpicChildCounts;
  milestone?: WorkItem;
  onSelect: (id: string) => void;
}
function DraggableEpicCard({
  item,
  childCounts,
  milestone,
  onSelect,
}: DraggableEpicCardProps) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: item.id });
  const style: React.CSSProperties = {
    transform: CSS.Translate.toString(transform),
    opacity: isDragging ? 0.85 : undefined,
    zIndex: isDragging ? 50 : undefined,
    position: isDragging ? 'relative' : undefined,
    touchAction: 'none',
  };
  return (
    <div ref={setNodeRef} style={style} {...attributes} {...listeners}>
      <EpicCard
        item={item}
        childCounts={childCounts}
        milestone={milestone}
        onSelect={onSelect}
        draggable
      />
    </div>
  );
}

// buildChildCounts pre-computes child counts so each EpicCard render
// doesn't re-walk the relationship graph. O(items) per call. Also
// aggregates DerivedSignals (gm-pd2 readiness row) when at least one
// child carries them — when none do, readiness is left undefined so
// the EpicCard hides the row rather than rendering as zeros.
//
// Exported so the readiness-aggregation rules can be unit-tested
// without standing up the full EpicView render tree.
export function buildChildCounts(items: WorkItem[]): Map<string, EpicChildCounts> {
  const out = new Map<string, EpicChildCounts>();
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
    let sawDerived = false;
    let ready = 0;
    let blocked = 0;
    for (const k of kids) {
      counts.byState[k.state_category] = (counts.byState[k.state_category] ?? 0) + 1;
      counts.total++;
      if (k.derived) {
        sawDerived = true;
        if (k.derived.agent_claimable) ready++;
        if (k.derived.human_action_required) blocked++;
      }
    }
    if (sawDerived) {
      counts.readiness = { ready, blocked };
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
