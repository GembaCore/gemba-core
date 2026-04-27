// /walks/:id read-only walk detail (gm-i65). Same agenda + transcript
// + cost surfaces as /walk, but the decision toolbar + composer are
// hidden because the walk is in a terminal state.

import { useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Footprints } from 'lucide-react';
import { getWalk, isTerminal, walkDuration, type Walk } from '@/api/walks';
import { fmtDuration } from '@/components/walk/BottomBar';

export function WalkDetailPage(): JSX.Element {
  const { id = '' } = useParams<{ id: string }>();
  const { data: walk, isLoading, error } = useQuery<Walk>({
    queryKey: ['walks', id],
    queryFn: () => getWalk(id),
    enabled: id !== '',
  });

  if (isLoading) {
    return (
      <div data-testid="walk-detail-loading" className="p-6 text-sm text-neutral-500">
        Loading walk…
      </div>
    );
  }
  if (error) {
    return (
      <div data-testid="walk-detail-error" className="p-6 text-sm text-rose-600">
        Failed to load walk: {error.message}
      </div>
    );
  }
  if (!walk) {
    return (
      <div data-testid="walk-detail-empty" className="p-6 text-sm text-neutral-500">
        No walk found.
      </div>
    );
  }

  const terminal = isTerminal(walk);
  const decisions = walk.decisions ?? [];
  const counts = decisionCountsByKind(walk);

  return (
    <div data-testid="walk-detail-page" className="flex h-full min-h-0 flex-col">
      <header className="border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
        <div className="flex items-baseline gap-2">
          <Footprints className="h-4 w-4 text-amber-600" aria-hidden />
          <h1 className="text-lg font-semibold">{walk.label || walk.id}</h1>
          <span
            data-testid="walk-detail-status"
            className="ml-2 rounded bg-neutral-100 px-2 py-0.5 text-[10px] uppercase tracking-wider text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
          >
            {walk.status}
          </span>
          {terminal ? (
            <span className="text-[11px] text-neutral-500">read-only</span>
          ) : null}
        </div>
        <dl className="mt-2 grid grid-cols-4 gap-4 text-xs text-neutral-600 dark:text-neutral-400">
          <Stat label="Workspace" value={walk.workspace} />
          <Stat label="Initiated by" value={String(walk.initiated_by)} />
          <Stat
            label="Duration"
            value={fmtDuration(walkDuration(walk))}
            testid="walk-detail-duration"
          />
          <Stat
            label="Cost"
            value={`$${walk.cost.dollars.toFixed(2)}`}
            testid="walk-detail-cost"
          />
        </dl>
      </header>
      <div className="grid flex-1 grid-cols-2 gap-6 overflow-y-auto px-6 py-4">
        <section>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-neutral-500">
            Agenda · {walk.agenda.length}
          </h2>
          <ul data-testid="walk-detail-agenda" className="space-y-1.5 text-xs">
            {walk.agenda.map((item) => (
              <li
                key={item.id}
                data-testid={`walk-detail-agenda-${item.id}`}
                className="rounded border border-neutral-200 bg-white px-2 py-1.5 dark:border-neutral-800 dark:bg-neutral-900"
              >
                <div className="text-[10px] uppercase tracking-wide text-neutral-500">
                  {item.source.kind} · {item.status}
                </div>
                <div className="font-medium">{item.topic}</div>
              </li>
            ))}
          </ul>
        </section>
        <section>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-neutral-500">
            Decisions · {decisions.length}
          </h2>
          <dl className="mb-3 grid grid-cols-5 gap-2 rounded border border-neutral-200 bg-neutral-50 px-3 py-2 text-[11px] dark:border-neutral-800 dark:bg-neutral-950">
            <Stat label="Ratify" value={counts.ratify} testid="walk-detail-counts-ratify" />
            <Stat label="Modify" value={counts.modify} testid="walk-detail-counts-modify" />
            <Stat label="Reject" value={counts.reject} testid="walk-detail-counts-reject" />
            <Stat label="Defer" value={counts.defer} testid="walk-detail-counts-defer" />
            <Stat label="Handoff" value={counts.handoff} testid="walk-detail-counts-handoff" />
          </dl>
          <ol data-testid="walk-detail-decisions" className="space-y-1 text-xs">
            {decisions.map((d, i) => (
              <li
                key={`${d.agenda_item_id}-${i}`}
                className="rounded border border-neutral-200 bg-white px-2 py-1 dark:border-neutral-800 dark:bg-neutral-900"
              >
                <span className="font-mono text-[10px] uppercase tracking-wide text-neutral-500">
                  {d.kind}
                </span>{' '}
                — <span>{d.agenda_item_id}</span>
                {d.rationale ? <div className="text-neutral-600 dark:text-neutral-400">{d.rationale}</div> : null}
              </li>
            ))}
          </ol>
        </section>
      </div>
    </div>
  );
}

function Stat({
  label,
  value,
  testid,
}: {
  label: string;
  value: string | number;
  testid?: string;
}): JSX.Element {
  return (
    <span
      data-testid={testid}
      className="inline-flex flex-col"
    >
      <span className="text-[10px] uppercase tracking-wide text-neutral-500">{label}</span>
      <span className="text-sm font-semibold text-neutral-800 dark:text-neutral-200">
        {value}
      </span>
    </span>
  );
}

function decisionCountsByKind(walk: Walk): {
  ratify: number;
  modify: number;
  reject: number;
  defer: number;
  handoff: number;
} {
  const out = { ratify: 0, modify: 0, reject: 0, defer: 0, handoff: 0 };
  for (const d of walk.decisions ?? []) out[d.kind]++;
  return out;
}
