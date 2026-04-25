// EpicCard (gm-root.6 / ui-spec §4.2): the primary card on the board's
// default Epic-primary view. Differences from WorkItemCard:
//   - kind chip ("EPIC") replaces the implicit-task look
//   - child progress bar replaces the assignee/glyphs row
//   - clicking opens the EpicDrawer, not the WorkItem drawer
//
// Per-state child counts are rendered as a tiny segmented bar so an
// operator can see "this epic is mostly Started" at a glance without
// expanding the swimlane.

import type { KeyboardEvent } from 'react';
import type { StateCategory, WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';
import { relativeTime } from './relativeTime';

const PRIORITY_STYLES: Record<string, string> = {
  P0: 'bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300',
  P1: 'bg-orange-100 text-orange-700 dark:bg-orange-950 dark:text-orange-300',
  P2: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
  P3: 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300',
  P4: 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400',
};

const STATE_DOT: Record<StateCategory, string> = {
  backlog: 'bg-neutral-400 dark:bg-neutral-500',
  unstarted: 'bg-sky-500',
  staged: 'bg-violet-500',
  started: 'bg-amber-500',
  completed: 'bg-emerald-500',
  canceled: 'bg-rose-500',
};

const STATE_BAR: Record<StateCategory, string> = {
  backlog: 'bg-neutral-300 dark:bg-neutral-700',
  unstarted: 'bg-sky-400',
  staged: 'bg-violet-400',
  started: 'bg-amber-400',
  completed: 'bg-emerald-400',
  canceled: 'bg-rose-400',
};

const STATE_ORDER: StateCategory[] = [
  'backlog',
  'unstarted',
  'staged',
  'started',
  'completed',
  'canceled',
];

export interface EpicChildCounts {
  // Total number of direct child WorkItems (any kind).
  total: number;
  // Per-state-category breakdown of those children.
  byState: Record<StateCategory, number>;
}

export interface EpicCardProps {
  item: WorkItem;
  childCounts: EpicChildCounts;
  onSelect?: (id: string) => void;
  // draggable=true switches the card's click semantics to match
  // ui-spec §4.5: single-click is a drag-start gesture (no drawer
  // open), double-click opens the drawer. When false the single-click
  // shortcut is preserved so non-DnD contexts stay accessible. Flipped
  // on by EpicView when gm-75u wires the DndContext.
  draggable?: boolean;
}

function priorityLabel(priority: number | null | undefined): string | null {
  if (priority == null) return null;
  if (priority < 0 || priority > 4) return null;
  return `P${priority}`;
}

export function EpicCard({ item, childCounts, onSelect, draggable }: EpicCardProps) {
  const pri = priorityLabel(item.priority);
  const interactive = !!onSelect;
  // ui-spec §4.5: primary card interaction is drag (restage);
  // double-click opens the drawer. In draggable mode we drop the
  // single-click handler because the pointer-down is the drag-start
  // gesture. Keyboard (Enter / Space) still opens the drawer so the
  // card is navigable without a pointer.
  const handleClick = !draggable && onSelect ? () => onSelect(item.id) : undefined;
  const handleDoubleClick = onSelect ? () => onSelect(item.id) : undefined;
  // In draggable mode dnd-kit's KeyboardSensor claims Space/Enter on
  // the draggable wrapper to run the keyboard-drag cycle (Space =
  // start, arrows = move, Space/Enter = drop). Handling the same keys
  // here would race the sensor — so we use `o` as the keyboard
  // drawer-open shortcut (gm-fqiw, mirrors DEFAULT_HOTKEYS' drawer-open
  // entry). When not draggable, Enter / Space stay wired so non-board
  // contexts (Backlog list etc.) keep their familiar shortcut.
  const handleKeyDown = onSelect
    ? (e: KeyboardEvent<HTMLElement>) => {
        if (e.key === 'o' || e.key === 'O') {
          e.preventDefault();
          onSelect(item.id);
          return;
        }
        if (!draggable && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault();
          onSelect(item.id);
        }
      }
    : undefined;

  return (
    <article
      data-work-item-id={item.id}
      data-epic-card="true"
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? `Open epic ${item.id}` : undefined}
      onClick={handleClick}
      onDoubleClick={handleDoubleClick}
      onKeyDown={handleKeyDown}
      className={cn(
        'group rounded-md border border-neutral-200 bg-white p-3 text-sm shadow-sm',
        'hover:border-neutral-300 hover:shadow',
        'dark:border-neutral-800 dark:bg-neutral-900 dark:hover:border-neutral-700',
        interactive &&
          'cursor-pointer focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-2 focus:ring-offset-neutral-50 dark:focus:ring-offset-neutral-950'
      )}
    >
      <header className="mb-1.5 flex items-center gap-2">
        <span
          aria-hidden
          className={cn('h-2 w-2 shrink-0 rounded-full', STATE_DOT[item.state_category])}
          title={item.state_category}
        />
        <span className="font-mono text-[11px] text-neutral-500 dark:text-neutral-400">
          {item.id}
        </span>
        <span className="rounded bg-violet-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-violet-700 dark:bg-violet-950 dark:text-violet-300">
          epic
        </span>
        {pri && (
          <span
            className={cn(
              'ml-auto rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
              PRIORITY_STYLES[pri]
            )}
          >
            {pri}
          </span>
        )}
      </header>

      <h3 className="line-clamp-2 font-medium text-neutral-900 dark:text-neutral-100">
        {item.title}
      </h3>

      {childCounts.total > 0 ? (
        <ProgressBar counts={childCounts} />
      ) : (
        <div className="mt-3 text-[11px] text-neutral-500 dark:text-neutral-400">
          no child items
        </div>
      )}

      <footer className="mt-2 flex items-center gap-2 text-[11px] text-neutral-500 dark:text-neutral-400">
        <span>
          {childCounts.total} {childCounts.total === 1 ? 'item' : 'items'}
        </span>
        {item.updated_at ? (
          <time className="ml-auto" dateTime={item.updated_at} title={item.updated_at}>
            {relativeTime(item.updated_at)}
          </time>
        ) : null}
      </footer>
    </article>
  );
}

// ProgressBar renders a 5-segment bar showing the proportion of
// children in each state. Zero-count segments are still allocated
// proportional space so the bar's total length communicates "how many
// children" not just "how varied the states".
function ProgressBar({ counts }: { counts: EpicChildCounts }) {
  if (counts.total === 0) return null;
  return (
    <div
      className="mt-3 flex h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800"
      data-testid="epic-progress"
    >
      {STATE_ORDER.map((cat) => {
        const n = counts.byState[cat] ?? 0;
        if (n === 0) return null;
        const pct = (n / counts.total) * 100;
        return (
          <div
            key={cat}
            className={STATE_BAR[cat]}
            style={{ width: `${pct}%` }}
            title={`${cat}: ${n}`}
            data-testid={`epic-progress-${cat}`}
          />
        );
      })}
    </div>
  );
}
