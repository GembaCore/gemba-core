// /sprints — list of sprints declared by the bound WorkPlane (gm-e11.5).
//
// Token-budget rollups, gauges, and three-tier enforcement are deferred
// to a follow-up under gm-root.14. This page is the entity-only surface:
// every sprint the adaptor reports, with a simple "X / Y beads done"
// progress chip per sprint. Adaptors with sprint_native=false return an
// empty list — the page renders an explanatory empty state rather than
// erroring.

import { Link } from 'react-router-dom';
import { CalendarRange } from 'lucide-react';

import { useSprints } from '@/hooks/useAgents';
import { useWorkItems } from '@/hooks/useWorkItems';
import type { Sprint, WorkItem } from '@/types/core.gen';

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
  if (list.length === 0) {
    return (
      <div data-testid="sprints-empty" className="p-6">
        <h1 className="text-xl font-semibold">Sprints</h1>
        <p className="mt-2 max-w-prose text-sm text-neutral-500">
          The bound WorkPlane adaptor doesn&apos;t expose a sprint roster. Sprints
          can still be set per work-item via the freeform editor on each card; this
          page only lists adaptors that publish a canonical sprint list (e.g. Jira).
        </p>
      </div>
    );
  }

  const allItems = items.data ?? [];
  return (
    <div data-testid="sprints-page" className="p-6">
      <h1 className="text-xl font-semibold">Sprints</h1>
      <p className="mt-1 text-sm text-neutral-500">
        Token-budget gauges and rollups are deferred — see gm-root.14.
      </p>
      <ul className="mt-4 space-y-2">
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
    </div>
  );
}
