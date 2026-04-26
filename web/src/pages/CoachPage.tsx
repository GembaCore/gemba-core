// CoachPage (gm-s47n.6.1). Coach-mode dispatch surface: the agent
// context strip + the (ready bead × live session) affinity grid
// with conflict highlights.
//
// The page consumes a single GET /api/planner/coach response —
// server-side composition keeps math out of the SPA. Hovering a
// bead highlights its conflict-adjacent siblings (including
// workspace-conflict edges against active sessions in the strip).

import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { AlertTriangle, Boxes, Clock, GitBranch, Sparkles, Users } from 'lucide-react';
import {
  getCoach,
  type PlannerAffinityRow,
  type PlannerCoachResponse,
  type PlannerConflictEdge,
  type PlannerOperationalContext,
  type PlannerReadyBead,
  type PlannerWorkspaceCollision,
} from '@/api/planner';
import { cn } from '@/lib/utils';

const POLL_MS = 30_000;

export function CoachPage() {
  const { data, isLoading, error } = useQuery<PlannerCoachResponse>({
    queryKey: ['planner', 'coach'],
    queryFn: getCoach,
    refetchInterval: POLL_MS,
    staleTime: POLL_MS / 2,
  });
  const [hoverBead, setHoverBead] = useState<string | null>(null);

  if (error) {
    return (
      <div className="m-8 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
        {(error as Error).message}
      </div>
    );
  }
  if (isLoading || !data) {
    return <div className="p-8 text-sm text-neutral-500">Loading…</div>;
  }
  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="coach-page">
      <Header notices={data.notices ?? []} batchCount={data.batches.length} />
      <SessionStrip
        sessions={data.sessions}
        hoverBead={hoverBead}
        affinity={data.affinity}
        workspace={data.workspace}
      />
      <DispatchGrid
        beads={data.ready_beads}
        sessions={data.sessions}
        affinity={data.affinity}
        conflicts={data.conflicts}
        hoverBead={hoverBead}
        setHoverBead={setHoverBead}
      />
    </div>
  );
}

function Header({ notices, batchCount }: { notices: string[]; batchCount: number }) {
  return (
    <header className="border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
      <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
        <Boxes className="h-5 w-5" aria-hidden />
        Coach
      </h1>
      <p className="text-xs text-neutral-500 dark:text-neutral-400">
        Dispatch grid backed by the planner. Hover a bead row to see its
        conflict siblings; the strip shows what each session is primed for.
      </p>
      <div className="mt-2 flex items-center gap-3 text-xs text-neutral-500">
        <span data-testid="coach-batch-count">
          {batchCount} parallel-safe batch{batchCount === 1 ? '' : 'es'}
        </span>
        {notices.map((n) => (
          <span
            key={n}
            data-testid="coach-notice"
            className="rounded bg-amber-50 px-2 py-0.5 text-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
          >
            {n}
          </span>
        ))}
      </div>
    </header>
  );
}

function SessionStrip({
  sessions,
  hoverBead,
  affinity,
  workspace,
}: {
  sessions: PlannerOperationalContext[];
  hoverBead: string | null;
  affinity: PlannerAffinityRow[];
  workspace: PlannerWorkspaceCollision[];
}) {
  const liveConflict = useMemo(() => {
    // Map sessionID → boolean "this session is in conflict with the
    // hovered bead" (workspace-collision detail names the bead↔live
    // edges).
    if (!hoverBead) return new Set<string>();
    const out = new Set<string>();
    for (const w of workspace) {
      if (w.live_session_id && w.b === hoverBead) out.add(w.live_session_id);
    }
    return out;
  }, [hoverBead, workspace]);

  if (sessions.length === 0) {
    return (
      <div className="border-b border-neutral-200 p-6 text-sm text-neutral-500 dark:border-neutral-800">
        No live sessions.
      </div>
    );
  }
  return (
    <div className="overflow-x-auto border-b border-neutral-200 p-3 dark:border-neutral-800">
      <div className="flex gap-3" data-testid="coach-session-strip">
        {sessions.map((c) => (
          <SessionCard
            key={c.session?.id ?? Math.random()}
            ctx={c}
            hovered={hoverBead}
            affinity={affinity}
            inConflict={liveConflict.has(c.session?.id ?? '')}
          />
        ))}
      </div>
    </div>
  );
}

function SessionCard({
  ctx,
  hovered,
  affinity,
  inConflict,
}: {
  ctx: PlannerOperationalContext;
  hovered: string | null;
  affinity: PlannerAffinityRow[];
  inConflict: boolean;
}) {
  const sid = ctx.session?.id ?? '';
  const score = useMemo(() => {
    if (!hovered) return null;
    return affinity.find((a) => a.session_id === sid && a.bead_id === hovered) ?? null;
  }, [hovered, affinity, sid]);

  const topConcepts = useMemo(() => {
    const m = ctx.profile?.concepts;
    if (!m) return [] as { tag: string; weight: number }[];
    return Object.entries(m)
      .map(([tag, weight]) => ({ tag, weight }))
      .sort((a, b) => b.weight - a.weight)
      .slice(0, 5);
  }, [ctx.profile?.concepts]);

  return (
    <div
      data-testid={`coach-session-${sid}`}
      data-in-conflict={inConflict || undefined}
      className={cn(
        'min-w-[260px] shrink-0 rounded-md border p-3 text-xs transition-colors',
        inConflict
          ? 'border-rose-400 bg-rose-50 dark:border-rose-700 dark:bg-rose-950/40'
          : 'border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900'
      )}
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="flex items-center gap-1 font-mono text-[11px] text-neutral-500">
          <Users className="h-3 w-3" />
          {ctx.agent?.id ?? '(anon)'}
        </span>
        <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] dark:bg-neutral-800">
          {ctx.session?.status ?? 'unknown'}
        </span>
      </div>
      <div className="mb-1 truncate font-medium">{ctx.agent?.name ?? sid}</div>
      {ctx.workspace ? (
        <div className="mb-2 text-[11px] text-neutral-500">
          <span className="inline-flex items-center gap-1">
            <GitBranch className="h-3 w-3" />
            {ctx.workspace.repository}/{ctx.workspace.branch}
          </span>
        </div>
      ) : null}
      {topConcepts.length > 0 ? (
        <div className="mb-2 flex flex-wrap gap-1">
          {topConcepts.map((c) => (
            <span
              key={c.tag}
              className="rounded bg-sky-50 px-1.5 py-0.5 font-mono text-[10px] text-sky-700 dark:bg-sky-950/40 dark:text-sky-300"
            >
              {c.tag}
            </span>
          ))}
        </div>
      ) : null}
      {ctx.health ? (
        <div className="grid grid-cols-3 gap-1 text-[10px] text-neutral-500">
          <Stat icon={<Sparkles className="h-3 w-3" />} value={ctx.health.context_pressure} />
          <Stat icon={<AlertTriangle className="h-3 w-3" />} value={ctx.health.concept_drift} />
          <Stat icon={<Clock className="h-3 w-3" />} value={ctx.health.time_on_task_ns / 1e9 / 60} suffix="m" precision={0} />
        </div>
      ) : null}
      {score ? (
        <div
          data-testid={`coach-session-${sid}-affinity`}
          className="mt-2 rounded border border-emerald-200 bg-emerald-50 px-2 py-1 text-[11px] text-emerald-800 dark:border-emerald-900/40 dark:bg-emerald-950/30 dark:text-emerald-200"
        >
          affinity for {score.bead_id}: {score.scores.combined.toFixed(2)}
        </div>
      ) : null}
    </div>
  );
}

function Stat({
  icon,
  value,
  suffix,
  precision = 2,
}: {
  icon: React.ReactNode;
  value: number;
  suffix?: string;
  precision?: number;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      {icon}
      <span className="font-mono">
        {value.toFixed(precision)}
        {suffix ?? ''}
      </span>
    </span>
  );
}

function DispatchGrid({
  beads,
  sessions,
  affinity,
  conflicts,
  hoverBead,
  setHoverBead,
}: {
  beads: PlannerReadyBead[];
  sessions: PlannerOperationalContext[];
  affinity: PlannerAffinityRow[];
  conflicts: PlannerConflictEdge[];
  hoverBead: string | null;
  setHoverBead: (s: string | null) => void;
}) {
  const adjacency = useMemo(() => {
    // bead id → set of conflict-adjacent bead ids.
    const out = new Map<string, Set<string>>();
    for (const e of conflicts) {
      add(out, e.From, e.To);
      add(out, e.To, e.From);
    }
    return out;
  }, [conflicts]);

  const cellByPair = useMemo(() => {
    const m = new Map<string, PlannerAffinityRow>();
    for (const a of affinity) m.set(`${a.bead_id}|${a.session_id}`, a);
    return m;
  }, [affinity]);

  if (beads.length === 0) {
    return (
      <div className="flex-1 p-6 text-sm text-neutral-500" data-testid="coach-empty">
        No ready beads.
      </div>
    );
  }
  return (
    <div className="flex-1 overflow-auto" data-testid="coach-grid">
      <table className="min-w-full text-sm">
        <thead className="sticky top-0 bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-950">
          <tr>
            <th className="border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">Bead</th>
            {sessions.map((s) => (
              <th
                key={s.session?.id}
                className="border-b border-neutral-200 px-3 py-2 dark:border-neutral-800"
              >
                {s.agent?.id ?? s.session?.id ?? '?'}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {beads.map((b) => {
            const dim =
              hoverBead !== null && hoverBead !== b.id && adjacency.get(hoverBead)?.has(b.id);
            return (
              <tr
                key={b.id}
                data-testid={`coach-row-${b.id}`}
                data-conflict-with-hovered={dim ? 'true' : undefined}
                onMouseEnter={() => setHoverBead(b.id)}
                onMouseLeave={() => setHoverBead(null)}
                className={cn(
                  'border-b border-neutral-100 transition-colors dark:border-neutral-900',
                  dim
                    ? 'bg-rose-50/60 text-rose-900 dark:bg-rose-950/30 dark:text-rose-200'
                    : hoverBead === b.id
                      ? 'bg-sky-50 dark:bg-sky-950/30'
                      : ''
                )}
              >
                <td className="px-3 py-1.5">
                  <div className="font-mono text-[11px] text-neutral-500">{b.id}</div>
                  <div className="truncate font-medium">{b.title}</div>
                </td>
                {sessions.map((s) => {
                  const cell = cellByPair.get(`${b.id}|${s.session?.id ?? ''}`);
                  return (
                    <td
                      key={s.session?.id}
                      data-testid={`coach-cell-${b.id}-${s.session?.id ?? ''}`}
                      className="px-3 py-1.5 align-top"
                    >
                      <CellScore row={cell} />
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function CellScore({ row }: { row: PlannerAffinityRow | undefined }) {
  if (!row) return <span className="text-neutral-400">—</span>;
  const tone = scoreTone(row.scores.combined);
  return (
    <div className="flex flex-col gap-0.5">
      <span className={cn('font-mono text-sm', tone)}>{row.scores.combined.toFixed(2)}</span>
      <span className="text-[10px] text-neutral-500">
        c={row.scores.concept_overlap.toFixed(2)} · f=
        {row.scores.file_familiarity.toFixed(2)} · w=
        {row.scores.workspace_match.toFixed(2)}
      </span>
    </div>
  );
}

function scoreTone(score: number): string {
  if (score >= 0.7) return 'text-emerald-700 dark:text-emerald-300';
  if (score >= 0.4) return 'text-sky-700 dark:text-sky-300';
  if (score >= 0.2) return 'text-amber-700 dark:text-amber-300';
  return 'text-neutral-500';
}

function add<K, V>(m: Map<K, Set<V>>, k: K, v: V) {
  let s = m.get(k);
  if (!s) {
    s = new Set();
    m.set(k, s);
  }
  s.add(v);
}
