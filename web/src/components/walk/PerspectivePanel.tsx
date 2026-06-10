// PerspectivePanel (gm-bms). Surfaces inline perspective contributions
// from every active persona on the currently-active agenda item.
//
// Renders inside RightPane.tsx's slot reserved for gm-bms (the prior
// PolecatPeekStub). Picking the right pane (rather than a fourth
// column) keeps the existing 3-pane layout intact and gives operators
// a consistent place to glance for "what does the rest of the team
// think about this item?".
//
// Data: pulled via the usePerspectives hook (react-query backed).
// Stale-revalidates when the active agenda item changes; the SSE
// walk.* listeners in WalkContext already invalidate the parent walk
// query, which propagates to a refetch here when the active item
// flips. The query is skipped (no fetch) when no item is active —
// keeps initial /walk paint cheap.

import { AlertOctagon, Edit3, Eye, Sparkles } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { cn } from '@/lib/utils';
import {
  getPerspectives,
  type PerspectiveContribution,
  type PerspectiveKind,
  type PerspectivesEnvelope,
} from '@/api/walks';
import { useWalk } from './WalkContext';

// usePerspectives fetches the per-agenda-item contributions. Caller
// passes the active walk id + item id; both must be non-empty for
// the query to run. Returns the react-query handle so a UI surface
// can render loading / error / data states without re-running the
// hook.
export function usePerspectives(
  walkID: string | undefined,
  agendaItemID: string | undefined
) {
  const enabled = !!walkID && !!agendaItemID;
  return useQuery<PerspectivesEnvelope, Error>({
    queryKey: ['walks', walkID, 'agenda', agendaItemID, 'perspectives'],
    queryFn: async () => {
      if (!walkID || !agendaItemID) {
        // Should be unreachable thanks to `enabled` — narrow for TS.
        throw new Error('usePerspectives: walkID and agendaItemID required');
      }
      return getPerspectives(walkID, agendaItemID);
    },
    enabled,
    // Modest stale window — perspectives are advisory and v1 returns
    // a deterministic stub, so a 30s TTL is plenty. The active-item
    // change in WalkContext drives the more important invalidation
    // via the queryKey shift.
    staleTime: 30_000,
  });
}

export function PerspectivePanel(): JSX.Element {
  const walk = useWalk();
  const active = walk.agenda[walk.activeIndex];
  const { data, isLoading, error } = usePerspectives(walk.walk?.id, active?.id);

  return (
    <section
      data-testid="walk-perspective-panel"
      className="flex-1 overflow-y-auto px-3 py-2"
    >
      <header className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
        <Eye className="h-3.5 w-3.5" aria-hidden />
        <span>Perspectives</span>
        {data && data.total > 0 ? (
          <span
            data-testid="walk-perspective-count"
            className="ml-auto text-[11px] text-neutral-500"
          >
            {data.total}
          </span>
        ) : null}
      </header>
      <PerspectiveBody
        active={!!active}
        loading={isLoading}
        error={error ?? null}
        contributions={data?.perspectives ?? []}
      />
    </section>
  );
}

function PerspectiveBody({
  active,
  loading,
  error,
  contributions,
}: {
  active: boolean;
  loading: boolean;
  error: Error | null;
  contributions: PerspectiveContribution[];
}): JSX.Element {
  if (!active) {
    return (
      <p
        data-testid="walk-perspective-empty"
        className="text-[11px] italic text-neutral-400"
      >
        Pick an active agenda item to see perspectives.
      </p>
    );
  }
  if (loading) {
    return (
      <p
        data-testid="walk-perspective-loading"
        className="text-[11px] italic text-neutral-400"
      >
        Loading perspectives…
      </p>
    );
  }
  if (error) {
    return (
      <p
        data-testid="walk-perspective-error"
        className="text-[11px] italic text-rose-500"
      >
        Couldn’t load perspectives: {error.message}
      </p>
    );
  }
  if (contributions.length === 0) {
    return (
      <p
        data-testid="walk-perspective-none"
        className="text-[11px] italic text-neutral-400"
      >
        No personas volunteered a perspective for this item.
      </p>
    );
  }
  return (
    <ul
      data-testid="walk-perspective-list"
      className="space-y-1.5"
    >
      {contributions.map((c) => (
        <PerspectiveRow key={c.persona_id} c={c} />
      ))}
    </ul>
  );
}

function PerspectiveRow({ c }: { c: PerspectiveContribution }): JSX.Element {
  const { Icon, tint } = visualForKind(c.kind);
  const label = c.persona_name ?? c.persona_id;
  return (
    <li
      data-testid={`walk-perspective-${c.persona_id}`}
      className={cn(
        'rounded border px-2 py-1.5 text-xs',
        tint
      )}
    >
      <div className="mb-0.5 flex items-center gap-1.5 text-[11px] font-semibold">
        <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span>
          {c.persona_icon ? `${c.persona_icon} ` : ''}
          {label}
        </span>
        <span
          data-testid={`walk-perspective-${c.persona_id}-kind`}
          className="ml-auto rounded bg-white/60 px-1 py-0 font-mono text-[10px] uppercase tracking-wide dark:bg-black/30"
        >
          {c.kind}
        </span>
      </div>
      <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-200">
        {c.content}
      </p>
    </li>
  );
}

function visualForKind(kind: PerspectiveKind): {
  Icon: typeof Eye;
  tint: string;
} {
  switch (kind) {
    case 'blocking':
      return {
        Icon: AlertOctagon,
        tint:
          'border-rose-200 bg-rose-50 text-rose-900 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-100',
      };
    case 'amendment':
      return {
        Icon: Edit3,
        tint:
          'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-100',
      };
    case 'note':
    default:
      return {
        Icon: Sparkles,
        tint:
          'border-neutral-200 bg-white text-neutral-800 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-200',
      };
  }
}
