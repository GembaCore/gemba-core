// Agenda pane (gm-i65). Left side of /walk: 360px-wide kanban-mini
// with Queued / Active / Decided / Deferred columns. Source-kind +
// priority badges per ui-spec §5.4.

import { AlertTriangle, BookCheck, BookOpen, CheckCircle2, GripVertical, HelpCircle, Star, ShieldAlert } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useWalk } from './WalkContext';
import type { AgendaItem, AgendaItemStatus, AgendaSourceKind, Lane } from './types';

const LANES: Lane[] = ['queued', 'active', 'decided', 'deferred'];
const LANE_LABEL: Record<Lane, string> = {
  queued: 'Queued',
  active: 'Active',
  decided: 'Decided',
  deferred: 'Deferred',
};

// Source-kind icon legend (backend-aligned). Matches the seven
// AgendaSourceKind values in api/walks.ts.
const SOURCE_ICON: Record<AgendaSourceKind, LucideIcon> = {
  user: Star,
  escalation: AlertTriangle,
  hitl: HelpCircle,
  gate_failure: ShieldAlert,
  bead_filed: BookOpen,
  bead_closed: BookCheck,
  suggested: Star,
};

// Urgency derived from priority — the agenda aggregator's priority
// constants (escalation=400+, hitl=300, gate=200, bead_filed=100,
// bead_closed=50). Bands here match those tiers so the badge
// reflects how the planner ranked the item.
type Urgency = 'urgent' | 'today' | 'soon' | 'later';

function urgencyOf(priority: number): Urgency {
  if (priority >= 400) return 'urgent';
  if (priority >= 200) return 'today';
  if (priority >= 100) return 'soon';
  return 'later';
}

const URGENCY_TONE: Record<Urgency, string> = {
  urgent: 'bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-300',
  today: 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  soon: 'bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300',
  later: 'bg-neutral-100 text-neutral-700 dark:bg-neutral-900 dark:text-neutral-400',
};

// statusToLane folds 'dismissed' (a backend-only admin status) into
// 'decided' for swimlane rendering — the operator doesn't need a
// separate column for items they dropped without a verdict.
function statusToLane(status: AgendaItemStatus): Lane {
  if (status === 'dismissed') return 'decided';
  return status;
}

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
  const Icon = SOURCE_ICON[item.source.kind] ?? Star;
  const isActive = item.status === 'active';
  const isDecided = item.status === 'decided' || item.status === 'dismissed';
  const urgency = urgencyOf(item.priority);
  return (
    <li>
      <button
        type="button"
        data-testid={`walk-agenda-item-${item.id}`}
        data-lane={statusToLane(item.status)}
        onClick={() => {
          void walk.setActiveItem(item.id);
        }}
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
        <span className="flex-1 truncate" title={item.topic}>
          {item.topic}
        </span>
        <span
          data-testid={`walk-agenda-item-${item.id}-urgency`}
          className={cn(
            'shrink-0 rounded px-1 py-0.5 text-[9px] uppercase tracking-wide',
            URGENCY_TONE[urgency]
          )}
        >
          {urgency}
        </span>
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
  for (const item of agenda) {
    out[statusToLane(item.status)].push(item);
  }
  return out;
}
