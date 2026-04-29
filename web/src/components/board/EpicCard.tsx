// EpicCard (gm-root.6 / ui-spec §4.2 / gm-pd2): the primary card on the
// board's default Epic-primary view. The §4.2 anatomy:
//
//   1. Priority stripe (3px left edge, color-coded P0-P4)
//   2. Title (14px, bold, 3-line max then ellipsis)
//   3. Readiness counts ('3/7 ready · 2 blocked · 1 in progress · 1 done')
//   4. Token-budget gauge (horizontal bar, hidden until Sprint+TokenBudget
//      data is wired through — see gm-e11.5)
//   5. Badges row: parallel-group glyph (gm-lkx), escalation dot
//      (gm-e11.3), purview-violation dot, perspective indicators —
//      each rendered conditionally; row collapses when none present
//
// Differences from WorkItemCard:
//   - kind chip ("EPIC") replaces the implicit-task look
//   - child progress bar replaces the assignee/glyphs row
//   - clicking opens the EpicDrawer, not the WorkItem drawer

import type { KeyboardEvent } from 'react';
import type { StateCategory, WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';
import { relativeTime } from './relativeTime';
import { milestoneNumber } from './milestone';
import { milestoneTone } from './milestoneBadge';

// Stripe colors live as static Tailwind class strings so the JIT picks
// them up. Keep in sync with PRIORITY_STYLES in WorkItemCard so
// priority signal reads consistently across cards.
const PRIORITY_STRIPE: Record<string, string> = {
  P0: 'border-l-rose-500',
  P1: 'border-l-orange-500',
  P2: 'border-l-amber-500',
  P3: 'border-l-sky-500',
  P4: 'border-l-neutral-400 dark:border-l-neutral-500',
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
  // Optional readiness aggregate from child DerivedSignals (gm-gsh).
  // Populated by buildChildCounts when children carry the signals;
  // absent when the source data is unavailable, in which case the
  // readiness row is hidden rather than rendering as zeros (the
  // operator can't tell "everyone unblocked" from "no signal yet").
  readiness?: EpicReadinessCounts;
}

export interface EpicReadinessCounts {
  // Children with derived.agent_claimable === true.
  ready: number;
  // Children with derived.human_action_required === true.
  blocked: number;
}

// EpicCardTokenBudget is the shape the §4.2 gauge reads. Wired through
// once Sprint+TokenBudget (gm-e11.5) lands; today every caller passes
// undefined and the gauge stays hidden.
export interface EpicCardTokenBudget {
  used: number;
  budget: number;
}

// EpicCardBadges is the §4.2 badges row. Each sub-field is an
// independent toggle: the row renders the badges that have data and
// collapses entirely when none do. Sub-beads driving each:
//   parallelGroup    — gm-lkx
//   escalations      — gm-e11.3 (count of open EscalationRequests)
//   purviewViolation — purview gate when wired
//   perspectives     — persona perspectives surface
export interface EpicCardBadges {
  parallelGroup?: string;
  escalations?: number;
  purviewViolation?: boolean;
  perspectives?: string[];
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
  tokenBudget?: EpicCardTokenBudget;
  badges?: EpicCardBadges;
  // milestone is the parent_child ancestor of this epic whose kind
  // is `milestone`, when one exists. When set the card paints a
  // milestone-tinted top stripe (left edge already carries the
  // priority stripe, gm-4se1) and renders an M<n> pill in the
  // header. Resolved by the parent (EpicView) so each card render
  // stays O(1).
  milestone?: WorkItem;
}

function priorityLabel(priority: number | null | undefined): string | null {
  if (priority == null) return null;
  if (priority < 0 || priority > 4) return null;
  return `P${priority}`;
}

function hasAnyBadge(b: EpicCardBadges | undefined): boolean {
  if (!b) return false;
  return (
    !!b.parallelGroup ||
    (typeof b.escalations === 'number' && b.escalations > 0) ||
    !!b.purviewViolation ||
    (Array.isArray(b.perspectives) && b.perspectives.length > 0)
  );
}

export function EpicCard({
  item,
  childCounts,
  onSelect,
  draggable,
  tokenBudget,
  badges,
  milestone,
}: EpicCardProps) {
  const pri = priorityLabel(item.priority);
  const mNum = milestone ? milestoneNumber(milestone.title) : 0;
  const mTone = milestone ? milestoneTone(milestone.id) : null;
  const interactive = !!onSelect;
  const handleClick = !draggable && onSelect ? () => onSelect(item.id) : undefined;
  const handleDoubleClick = onSelect ? () => onSelect(item.id) : undefined;
  // In draggable mode dnd-kit's KeyboardSensor claims Space/Enter, so
  // 'o' opens the drawer (gm-fqiw); non-draggable contexts keep the
  // familiar Enter / Space.
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

  const showBadges = hasAnyBadge(badges);

  return (
    <article
      data-work-item-id={item.id}
      data-epic-card="true"
      data-priority={pri ?? undefined}
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? `Open epic ${item.id}` : undefined}
      onClick={handleClick}
      onDoubleClick={handleDoubleClick}
      onKeyDown={handleKeyDown}
      data-milestone-id={milestone?.id}
      className={cn(
        'group relative rounded-md border border-neutral-200 bg-white p-3 text-sm shadow-sm',
        // §4.2: 3px priority stripe along the left edge.
        'border-l-[3px]',
        pri ? PRIORITY_STRIPE[pri] : '',
        // gm-4se1: 3px milestone stripe along the top edge when the
        // epic belongs to a milestone. Top + left don't fight; the
        // two signals read independently.
        mTone ? cn('border-t-[3px]', mTone.stripe) : '',
        'hover:border-neutral-300 hover:shadow',
        'dark:border-neutral-800 dark:bg-neutral-900 dark:hover:border-neutral-700',
        interactive &&
          'cursor-pointer focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-2 focus:ring-offset-neutral-50 dark:focus:ring-offset-neutral-950'
      )}
    >
      {/* Screen-reader-only priority callout — the stripe is purely
          visual, so AT users get the equivalent signal from this label. */}
      {pri && <span className="sr-only">Priority {pri}</span>}

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
        {milestone && mTone && (
          <span
            className={cn(
              'rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
              mTone.pill
            )}
            title={milestone.title || milestone.id}
            data-testid="epic-milestone-pill"
          >
            {mNum > 0 ? `M${mNum}` : 'M?'}
          </span>
        )}
      </header>

      {/* §4.2: title is bold and clamps at 3 lines. */}
      <h3 className="line-clamp-3 font-bold leading-snug text-neutral-900 dark:text-neutral-100">
        {item.title}
      </h3>

      {childCounts.readiness && childCounts.total > 0 && (
        <ReadinessRow counts={childCounts} />
      )}

      {childCounts.total > 0 ? (
        <ProgressBar counts={childCounts} />
      ) : (
        <div className="mt-3 text-[11px] text-neutral-500 dark:text-neutral-400">
          no child items
        </div>
      )}

      {tokenBudget && <TokenBudgetGauge {...tokenBudget} />}

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

      {showBadges && <BadgesRow badges={badges!} />}
    </article>
  );
}

// ReadinessRow renders the "3/7 ready · 2 blocked · 1 in progress · 1
// done" line. "in progress" and "done" are state-derived (started /
// completed); "ready" and "blocked" come from DerivedSignals
// aggregates the parent provides via childCounts.readiness.
function ReadinessRow({ counts }: { counts: EpicChildCounts }) {
  const r = counts.readiness!;
  const inProgress = counts.byState.started ?? 0;
  const done = counts.byState.completed ?? 0;
  return (
    <div
      className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-neutral-600 dark:text-neutral-400"
      data-testid="epic-readiness"
    >
      <span data-testid="epic-readiness-ready">
        <span className="font-semibold text-emerald-700 dark:text-emerald-400">{r.ready}</span>
        /{counts.total} ready
      </span>
      <span aria-hidden>·</span>
      <span data-testid="epic-readiness-blocked">
        <span
          className={cn(
            'font-semibold',
            r.blocked > 0 ? 'text-rose-700 dark:text-rose-400' : ''
          )}
        >
          {r.blocked}
        </span>{' '}
        blocked
      </span>
      <span aria-hidden>·</span>
      <span data-testid="epic-readiness-in-progress">
        <span className="font-semibold text-amber-700 dark:text-amber-400">{inProgress}</span> in
        progress
      </span>
      <span aria-hidden>·</span>
      <span data-testid="epic-readiness-done">
        <span className="font-semibold text-neutral-700 dark:text-neutral-300">{done}</span> done
      </span>
    </div>
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

// TokenBudgetGauge renders the §4.2 horizontal token-spend bar with a
// usage label. Color shifts to amber once usage crosses 70% and rose
// once over 100% so the operator sees a visual warning before the
// hard stop tier (gm-e11.5 inform/warn/stop ladder will eventually
// drive the thresholds; the static 70/100 split here matches the
// inform/warn defaults named in the design).
function TokenBudgetGauge({ used, budget }: EpicCardTokenBudget) {
  const safeBudget = Math.max(budget, 0);
  const safeUsed = Math.max(used, 0);
  const pct = safeBudget === 0 ? 0 : Math.min(100, (safeUsed / safeBudget) * 100);
  const tone =
    safeBudget > 0 && safeUsed > safeBudget
      ? 'bg-rose-500'
      : pct >= 70
        ? 'bg-amber-500'
        : 'bg-emerald-500';
  return (
    <div className="mt-2" data-testid="epic-token-budget">
      <div className="mb-0.5 flex items-baseline justify-between text-[10px] text-neutral-600 dark:text-neutral-400">
        <span>Tokens</span>
        <span>
          {safeUsed.toLocaleString()} / {safeBudget.toLocaleString()}
        </span>
      </div>
      <div className="h-1 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800">
        <div className={cn('h-full', tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

// BadgesRow renders the §4.2 badges. Each badge appears only when its
// data is set; absent the row collapses (the parent gates on
// hasAnyBadge before rendering this at all).
function BadgesRow({ badges }: { badges: EpicCardBadges }) {
  return (
    <div
      className="mt-2 flex flex-wrap items-center gap-1.5 text-[10px]"
      data-testid="epic-badges"
    >
      {badges.parallelGroup && (
        <span
          className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-indigo-100 px-1 font-semibold text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300"
          title={`Parallel group ${badges.parallelGroup}`}
          data-testid="epic-badge-parallel-group"
        >
          {badges.parallelGroup}
        </span>
      )}
      {typeof badges.escalations === 'number' && badges.escalations > 0 && (
        <span
          className="inline-flex h-4 items-center gap-0.5 rounded-full bg-rose-100 px-1.5 font-semibold text-rose-700 dark:bg-rose-950 dark:text-rose-300"
          title={`${badges.escalations} open escalation${badges.escalations === 1 ? '' : 's'}`}
          data-testid="epic-badge-escalations"
        >
          <span aria-hidden>!</span>
          <span>{badges.escalations}</span>
        </span>
      )}
      {badges.purviewViolation && (
        <span
          className="inline-flex h-4 items-center rounded-full bg-amber-100 px-1.5 font-semibold text-amber-800 dark:bg-amber-950 dark:text-amber-300"
          title="Purview violation"
          data-testid="epic-badge-purview"
        >
          purview
        </span>
      )}
      {badges.perspectives && badges.perspectives.length > 0 && (
        <span className="flex items-center gap-0.5" data-testid="epic-badge-perspectives">
          {badges.perspectives.slice(0, 3).map((p) => (
            <span
              key={p}
              className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-sky-100 text-[9px] font-semibold text-sky-700 dark:bg-sky-950 dark:text-sky-300"
              title={`Perspective: ${p}`}
            >
              {p.slice(0, 1).toUpperCase()}
            </span>
          ))}
        </span>
      )}
    </div>
  );
}
