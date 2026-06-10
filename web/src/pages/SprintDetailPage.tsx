// /sprints/:id — one sprint's bead roster (gm-e11.5).
//
// Reads the sprint by id from useSprints() and the bead set via
// useFilteredWorkItems({sprint_id}). No budget surface — gauges and
// three-tier enforcement are deferred to a follow-up under gm-root.14.

import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, CalendarRange } from 'lucide-react';

import { useSprints } from '@/hooks/useAgents';
import { useFilteredWorkItems } from '@/hooks/useWorkItems';
import type { WorkItem } from '@/types/core.gen';

function fmtRange(starts: string, ends: string): string {
  const s = new Date(starts);
  const e = new Date(ends);
  const opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', year: 'numeric' };
  return `${s.toLocaleDateString(undefined, opts)} – ${e.toLocaleDateString(undefined, opts)}`;
}

function StatusPill({ item }: { item: WorkItem }): JSX.Element {
  const cat = item.state_category;
  const tone =
    cat === 'completed' ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
    : cat === 'started' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
    : cat === 'canceled' ? 'bg-neutral-200 text-neutral-700 dark:bg-neutral-700 dark:text-neutral-200'
    : 'bg-neutral-100 text-neutral-800 dark:bg-neutral-800 dark:text-neutral-200';
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs ${tone}`} data-testid={`pill-${item.id}`}>
      {item.status}
    </span>
  );
}

export function SprintDetailPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const sprintId = id ? decodeURIComponent(id) : '';
  const sprints = useSprints();
  const items = useFilteredWorkItems({ sprint_id: sprintId });

  if (!sprintId) {
    return (
      <div data-testid="sprint-detail-no-id" className="p-6 text-sm text-red-600">
        Missing sprint id.
      </div>
    );
  }

  if (sprints.isLoading || items.isLoading) {
    return (
      <div data-testid="sprint-detail-loading" className="p-6 text-sm text-neutral-500">
        Loading sprint…
      </div>
    );
  }

  const sprint = (sprints.data ?? []).find((s) => s.id === sprintId);

  if (sprints.isError) {
    return (
      <div data-testid="sprint-detail-error" className="p-6 text-sm text-red-600">
        Failed to load sprints: {sprints.error.message}
      </div>
    );
  }

  if (!sprint) {
    return (
      <div data-testid="sprint-detail-not-found" className="p-6">
        <Link to="/sprints" className="inline-flex items-center text-sm text-neutral-600 hover:underline">
          <ArrowLeft className="mr-1 h-4 w-4" /> All sprints
        </Link>
        <h1 className="mt-2 text-xl font-semibold">Sprint not found</h1>
        <p className="mt-1 text-sm text-neutral-500">
          No sprint with id <code>{sprintId}</code> in the current roster.
        </p>
      </div>
    );
  }

  const list = items.data ?? [];
  const closed = list.filter((it) => it.state_category === 'completed').length;

  return (
    <div data-testid="sprint-detail-page" className="p-6">
      <Link to="/sprints" className="inline-flex items-center text-sm text-neutral-600 hover:underline">
        <ArrowLeft className="mr-1 h-4 w-4" /> All sprints
      </Link>

      <header className="mt-2 flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold">{sprint.name}</h1>
          <div className="mt-1 flex items-center gap-2 text-sm text-neutral-500">
            <CalendarRange className="h-4 w-4" />
            {fmtRange(sprint.starts_at, sprint.ends_at)}
          </div>
          {sprint.goal ? (
            <p className="mt-2 max-w-prose text-sm text-neutral-700 dark:text-neutral-300">
              {sprint.goal}
            </p>
          ) : null}
        </div>
        <div className="text-right">
          <div className="text-2xl font-semibold tabular-nums">
            {closed} <span className="text-neutral-400">/ {list.length}</span>
          </div>
          <div className="text-xs text-neutral-500">beads done</div>
        </div>
      </header>

      <p className="mt-3 text-xs text-neutral-500">
        Token-budget rollups and three-tier enforcement are deferred — see gm-root.14.
      </p>

      <ul className="mt-4 space-y-1">
        {list.length === 0 ? (
          <li className="rounded-md border border-dashed p-4 text-sm text-neutral-500" data-testid="sprint-detail-empty">
            No work items assigned to this sprint.
          </li>
        ) : (
          list.map((item) => (
            <li
              key={item.id}
              className="flex items-center justify-between rounded-md border border-neutral-200 bg-white p-3 dark:border-neutral-800 dark:bg-neutral-900"
              data-testid={`row-${item.id}`}
            >
              <div className="min-w-0">
                <div className="truncate font-medium">{item.title}</div>
                <div className="text-xs text-neutral-500">{item.id}</div>
              </div>
              <StatusPill item={item} />
            </li>
          ))
        )}
      </ul>
    </div>
  );
}
