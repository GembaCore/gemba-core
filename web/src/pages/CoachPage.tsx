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
import { Boxes } from 'lucide-react';
import {
  getCoach,
  type PlannerAffinityRow,
  type PlannerCoachResponse,
  type PlannerConflictEdge,
  type PlannerOperationalContext,
  type PlannerReadyBead,
  type PlannerWorkspaceCollision,
} from '@/api/planner';
import { AgentContextStrip } from '@/components/agents/AgentContextStrip';
import { SessionHealthPanel } from '@/components/health/SessionHealthPanel';
import { RecommendOrderDrawer } from '@/components/persona/RecommendOrderDrawer';
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
  const [recommendOpen, setRecommendOpen] = useState(false);

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
      <Header
        notices={data.notices ?? []}
        batchCount={data.batches.length}
        onRecommendOrder={() => setRecommendOpen(true)}
      />
      <RecommendOrderDrawer
        open={recommendOpen}
        onClose={() => setRecommendOpen(false)}
        workspace={data.ready_beads[0]?.repository ?? 'default'}
        readyBeads={data.ready_beads}
      />
      <div className="px-8 py-3">
        <SessionHealthPanel sessions={data.sessions} sortBySeverity testid="coach-health-panel" />
      </div>
      <CoachSessionStrip
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

// CoachSessionStrip wraps the reusable AgentContextStrip
// (gm-s47n.6.7) with coach-specific overlay logic — the
// hovered-bead → conflict / affinity callbacks the strip itself
// stays agnostic about. Other surfaces (a future agents page, a
// debug roster) reuse AgentContextStrip directly with their own
// callbacks (or none).
function CoachSessionStrip({
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
    if (!hoverBead) return new Set<string>();
    const out = new Set<string>();
    for (const w of workspace) {
      if (w.live_session_id && w.b === hoverBead) out.add(w.live_session_id);
    }
    return out;
  }, [hoverBead, workspace]);

  const affinityBySession = useMemo(() => {
    if (!hoverBead) return new Map<string, { beadId: string; combined: number }>();
    const m = new Map<string, { beadId: string; combined: number }>();
    for (const a of affinity) {
      if (a.bead_id === hoverBead) {
        m.set(a.session_id, { beadId: a.bead_id, combined: a.scores.combined });
      }
    }
    return m;
  }, [hoverBead, affinity]);

  return (
    <AgentContextStrip
      sessions={sessions}
      inConflict={(sid) => liveConflict.has(sid)}
      affinityFor={(sid) => affinityBySession.get(sid) ?? null}
      testid="coach-session-strip"
    />
  );
}

function Header({
  notices,
  batchCount,
  onRecommendOrder,
}: {
  notices: string[];
  batchCount: number;
  onRecommendOrder: () => void;
}) {
  return (
    <header className="border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
            <Boxes className="h-5 w-5" aria-hidden />
            Coach
          </h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Dispatch grid backed by the planner. Hover a bead row to see its
            conflict siblings; the strip shows what each session is primed for.
          </p>
        </div>
        {/* gm-twp2: PM persona consult entrypoint. Spawns a Claude
            Code session that ranks the current ready beads. */}
        <button
          type="button"
          onClick={onRecommendOrder}
          data-testid="coach-recommend-order"
          className="inline-flex items-center gap-1.5 rounded-md border border-sky-600 bg-white px-3 py-1.5 text-sm font-medium text-sky-700 hover:bg-sky-50 dark:bg-neutral-950 dark:text-sky-400 dark:hover:bg-sky-950/30"
        >
          Recommend order
        </button>
      </div>
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

// SessionStrip / SessionCard / Stat were inlined here in
// gm-s47n.6.1; gm-s47n.6.7 extracted them into the reusable
// AgentContextStrip + AgentContextCard components under
// web/src/components/agents/. Coach-specific overlay logic
// (which sessions conflict with the hovered bead) lives in
// CoachSessionStrip above.

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
