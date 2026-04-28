// /sprints — sprint planning surface (gm-e11.5, gm-szz0.1).
//
// gm-e11.5 shipped the entity-only roster: every sprint the bound
// WorkPlane adaptor declares, with a "X / Y beads done" progress
// chip per row. Adaptors with sprint_native=false return an empty
// list — the page renders an explanatory empty state.
//
// gm-szz0.1 made /sprints the home of sprint *planning*: the PM
// persona's epic_order skill (RecommendOrderDrawer) used to mount
// on /coach, conflating dispatch with planning. It now surfaces
// here as the page's primary action — operators land on /sprints
// to ask the PM to compose the next sprint, then scan the roster
// underneath.
//
// Token-budget rollups, gauges, and three-tier enforcement are
// deferred to a follow-up under gm-root.14.

import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { CalendarRange, Sparkles } from 'lucide-react';

import { useSprints } from '@/hooks/useAgents';
import { useWorkItems } from '@/hooks/useWorkItems';
import { getCoach, type PlannerCoachResponse } from '@/api/planner';
import { RecommendOrderDrawer } from '@/components/persona/RecommendOrderDrawer';
import type { Sprint, WorkItem } from '@/types/core.gen';

const PLANNER_POLL_MS = 30_000;

interface SprintProgress {
  total: number;
  closed: number;
}

function progressFor(sprint: Sprint, items: WorkItem[]): SprintProgress {
  const inSprint = items.filter((it) => it.sprint_id === sprint.id);
  // We branch on state_category, not the adaptor's native status string —
  // the category is the adaptor-agnostic done signal (gm-knrm). "completed"
  // is the closed bucket; "canceled" is excluded from the done count
  // because the operator typically wants "delivered" vs "everything else".
  const closed = inSprint.filter((it) => it.state_category === 'completed').length;
  return { total: inSprint.length, closed };
}

function fmtRange(starts: string, ends: string): string {
  const s = new Date(starts);
  const e = new Date(ends);
  const opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' };
  return `${s.toLocaleDateString(undefined, opts)} – ${e.toLocaleDateString(undefined, opts)}`;
}

// PlannerHeader (gm-szz0.1) — the planning entrypoint that used
// to live on /coach. Fetches /api/planner/coach so the drawer has
// the same ready_beads payload the dispatch grid does. The
// planner data is independent of the sprint roster fetches; if
// the planner endpoint is unavailable the button surfaces an
// inline notice but the sprint roster underneath still renders.
function PlannerHeader(): JSX.Element {
  const [open, setOpen] = useState(false);
  const planner = useQuery<PlannerCoachResponse>({
    queryKey: ['planner', 'coach'],
    queryFn: getCoach,
    refetchInterval: PLANNER_POLL_MS,
    staleTime: PLANNER_POLL_MS / 2,
  });

  const readyBeads = planner.data?.ready_beads ?? [];
  const workspace = readyBeads[0]?.repository ?? 'default';
  const canRecommend = !planner.isLoading && !planner.isError;

  return (
    <section
      data-testid="sprints-planner-header"
      className="mb-6 rounded-md border border-neutral-200 bg-white px-4 py-3 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold tracking-tight">
            <Sparkles className="h-4 w-4" aria-hidden />
            Sprint planning
          </h2>
          <p className="mt-0.5 max-w-prose text-xs text-neutral-500 dark:text-neutral-400">
            Use <em>Recommend order</em> to ask the PM persona to rank
            candidate epics for the next sprint. Dispatch (&quot;what should
            I work on next?&quot;) lives on{' '}
            <Link to="/coach" className="text-sky-700 hover:underline dark:text-sky-300">
              /coach
            </Link>
            .
          </p>
        </div>
        <button
          type="button"
          onClick={() => setOpen(true)}
          disabled={!canRecommend}
          data-testid="sprints-recommend-order"
          className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-sky-600 bg-white px-3 py-1.5 text-sm font-medium text-sky-700 hover:bg-sky-50 disabled:cursor-not-allowed disabled:opacity-40 dark:bg-neutral-950 dark:text-sky-400 dark:hover:bg-sky-950/30"
        >
          Recommend order
        </button>
      </div>
      {planner.isLoading && (
        <p
          className="mt-2 text-xs text-neutral-500"
          data-testid="sprints-planner-loading"
        >
          Loading planner snapshot…
        </p>
      )}
      {planner.isError && (
        <p
          className="mt-2 text-xs text-rose-600 dark:text-rose-400"
          data-testid="sprints-planner-error"
        >
          Planner unavailable: {(planner.error as Error).message}
        </p>
      )}
      {!planner.isLoading && !planner.isError && readyBeads.length === 0 && (
        <p
          className="mt-2 text-xs text-neutral-500"
          data-testid="sprints-planner-empty"
        >
          No ready beads in the planner snapshot. Add a candidate or clear a
          soft-block, then re-open the drawer.
        </p>
      )}
      <RecommendOrderDrawer
        open={open}
        onClose={() => setOpen(false)}
        workspace={workspace}
        readyBeads={readyBeads}
      />
    </section>
  );
}

export function SprintsPage(): JSX.Element {
  const sprints = useSprints();
  const items = useWorkItems();

  if (sprints.isLoading || items.isLoading) {
    return (
      <div data-testid="sprints-loading" className="p-6 text-sm text-neutral-500">
        Loading sprints…
      </div>
    );
  }

  if (sprints.isError) {
    return (
      <div data-testid="sprints-error" className="p-6 text-sm text-red-600">
        Failed to load sprints: {sprints.error.message}
      </div>
    );
  }

  const list = sprints.data ?? [];
  const allItems = items.data ?? [];

  return (
    <div data-testid="sprints-page" className="p-6">
      <h1 className="text-xl font-semibold">Sprints</h1>
      <p className="mt-1 text-sm text-neutral-500">
        Sprint planning. Use &quot;Recommend order&quot; to ask the PM persona
        to rank candidate epics for the next sprint.
      </p>
      <div className="mt-4">
        <PlannerHeader />
      </div>
      {list.length === 0 ? (
        <div data-testid="sprints-empty">
          <p className="max-w-prose text-sm text-neutral-500">
            The bound WorkPlane adaptor doesn&apos;t expose a sprint roster.
            Sprints can still be set per work-item via the freeform editor on
            each card; this page only lists adaptors that publish a canonical
            sprint list (e.g. Jira).
          </p>
        </div>
      ) : (
        <ul className="mt-2 space-y-2">
          {list.map((sprint) => {
            const p = progressFor(sprint, allItems);
            return (
              <li key={sprint.id}>
                <Link
                  to={`/sprints/${encodeURIComponent(sprint.id)}`}
                  className="flex items-center justify-between rounded-md border border-neutral-200 bg-white p-3 hover:bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-900 dark:hover:bg-neutral-800"
                  data-testid={`sprint-row-${sprint.id}`}
                >
                  <div className="flex items-center gap-3">
                    <CalendarRange className="h-4 w-4 text-neutral-500" />
                    <div>
                      <div className="font-medium">{sprint.name}</div>
                      <div className="text-xs text-neutral-500">
                        {fmtRange(sprint.starts_at, sprint.ends_at)}
                      </div>
                    </div>
                  </div>
                  <div className="text-sm tabular-nums text-neutral-600 dark:text-neutral-300">
                    {p.closed} / {p.total} done
                  </div>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
