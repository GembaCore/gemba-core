// Agenda pane (gm-uipx.2). Left side of /walk: 360px-wide
// kanban-mini with Queued / Active / Decided / Deferred columns.

import { AlertTriangle, BookCheck, BookOpen, CheckCircle2, GripVertical, HelpCircle, Star } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useWalk } from './WalkContext';
import type { AgendaItem, AgendaSource, Lane } from './types';

const LANES: Lane[] = ['queued', 'active', 'decided', 'deferred'];
const LANE_LABEL: Record<Lane, string> = {
  queued: 'Queued',
  active: 'Active',
  decided: 'Decided',
  deferred: 'Deferred',
};

// Source-kind icon legend per ui-spec §5.4. The spec uses unicode
// glyphs ●◉◆◇★; lucide icons stay closer to the rest of the app's
// chrome so the walk visual language matches Sidebar / Topbar.
const SOURCE_ICON: Record<AgendaSource, LucideIcon> = {
  escalation: AlertTriangle,
  hitl: HelpCircle,
  filed_bead: BookOpen,
  closed_bead: BookCheck,
  user_added: Star,
};

const URGENCY_TONE: Record<NonNullable<AgendaItem['urgency']>, string> = {
  urgent: 'bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-300',
  today: 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  soon: 'bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300',
  later: 'bg-neutral-100 text-neutral-700 dark:bg-neutral-900 dark:text-neutral-400',
};

export function AgendaPane(): JSX.Element {
  const walk = useWalk();
  const grouped = groupByLane(walk.agenda);
  return (
    <aside
      data-testid="walk-agenda-pane"
      className="flex h-full w-[360px] shrink-0 flex-col border-r border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950"
    >
      <header className="flex items-center justify-between border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <span className="text-xs font-semibold uppercase tracking-wide text-neutral-500">
          Agenda
        </span>
        <span className="text-xs text-neutral-500" data-testid="walk-agenda-summary">
          {walk.agenda.length} items
        </span>
      </header>
      <div className="flex-1 overflow-y-auto p-2 space-y-3">
        {LANES.map((lane) => (
          <LaneColumn key={lane} lane={lane} items={grouped[lane]} />
        ))}
      </div>
    </aside>
  );
}

function LaneColumn({ lane, items }: { lane: Lane; items: AgendaItem[] }): JSX.Element {
  return (
    <section data-testid={`walk-agenda-lane-${lane}`} className="space-y-1.5">
      <h3 className="px-1 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        {LANE_LABEL[lane]} <span className="text-neutral-400">· {items.length}</span>
      </h3>
      <ul className="space-y-1.5">
        {items.length === 0 ? (
          <li className="rounded border border-dashed border-neutral-300 px-2 py-2 text-[11px] italic text-neutral-400 dark:border-neutral-700">
            empty
          </li>
        ) : (
          items.map((item) => <AgendaCard key={item.id} item={item} />)
        )}
      </ul>
    </section>
  );
}

function AgendaCard({ item }: { item: AgendaItem }): JSX.Element {
  const walk = useWalk();
  const Icon = SOURCE_ICON[item.source];
  const isActive = item.lane === 'active';
  const isDecided = item.lane === 'decided';
  return (
    <li>
      <button
        type="button"
        data-testid={`walk-agenda-item-${item.id}`}
        data-lane={item.lane}
        onClick={() => walk.setActiveItem(item.id)}
        className={cn(
          'group flex w-full items-start gap-2 rounded border px-2 py-1.5 text-left text-xs transition-colors',
          isActive
            ? 'border-sky-400 bg-sky-50 text-sky-900 dark:border-sky-700 dark:bg-sky-950 dark:text-sky-100'
            : 'border-neutral-200 bg-white hover:border-neutral-300 dark:border-neutral-800 dark:bg-neutral-900 dark:hover:border-neutral-700'
        )}
      >
        <Icon
          className={cn('h-3.5 w-3.5 shrink-0', isActive ? 'text-sky-600' : 'text-neutral-500')}
          aria-hidden
        />
        <span className="flex-1 truncate" title={item.title}>
          {item.title}
        </span>
        {item.urgency ? (
          <span
            data-testid={`walk-agenda-item-${item.id}-urgency`}
            className={cn(
              'shrink-0 rounded px-1 py-0.5 text-[9px] uppercase tracking-wide',
              URGENCY_TONE[item.urgency]
            )}
          >
            {item.urgency}
          </span>
        ) : null}
        {isDecided ? (
          <CheckCircle2
            data-testid={`walk-agenda-item-${item.id}-decided`}
            className="h-3.5 w-3.5 shrink-0 text-emerald-600"
            aria-label="decided"
          />
        ) : (
          <GripVertical
            data-testid={`walk-agenda-item-${item.id}-drag`}
            className="h-3.5 w-3.5 shrink-0 cursor-grab text-neutral-400 opacity-0 transition-opacity group-hover:opacity-100"
            aria-label="drag handle"
          />
        )}
      </button>
    </li>
  );
}

function groupByLane(agenda: AgendaItem[]): Record<Lane, AgendaItem[]> {
  const out: Record<Lane, AgendaItem[]> = {
    queued: [],
    active: [],
    decided: [],
    deferred: [],
  };
  for (const item of agenda) out[item.lane].push(item);
  return out;
}
