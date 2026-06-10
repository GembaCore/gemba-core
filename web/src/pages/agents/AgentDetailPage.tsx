// AgentDetailPage (gm-e12.15). Route container for the
// provider-aware agent detail view. Resolves :id (which may be an
// agent id or a session id — both are operator-typeable) against
// the planner-coach feed and hands the matching context to the pure
// AgentDetail view.
//
// We reuse the planner-coach endpoint rather than introducing a new
// /api/agents/:id surface because every field the kind-specific
// panels need (workspace, agent, session, health) is already
// composed there. A dedicated endpoint can land later if traffic
// shape demands it.

import { useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { getCoach, type PlannerCoachResponse, type PlannerOperationalContext } from '@/api/planner';
import { AgentDetail } from '@/views/AgentDetail';

const POLL_MS = 30_000;

export function AgentDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const { data, isLoading, error } = useQuery<PlannerCoachResponse>({
    queryKey: ['planner', 'coach'],
    queryFn: getCoach,
    refetchInterval: POLL_MS,
    staleTime: POLL_MS / 2,
  });

  const ctx = useMemo<PlannerOperationalContext | undefined>(() => {
    if (!data) return undefined;
    return findContext(data.sessions, id);
  }, [data, id]);

  if (error) {
    return (
      <div className="m-8 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
        {(error as Error).message}
      </div>
    );
  }
  if (isLoading || !data) {
    return (
      <div data-testid="agent-detail-loading" className="p-8 text-sm text-neutral-500">
        Loading…
      </div>
    );
  }
  if (!ctx) {
    return (
      <div className="flex flex-col gap-3 p-8 text-sm" data-testid="agent-detail-not-found">
        <p className="text-neutral-500">
          No live session found for agent or session id{' '}
          <span className="font-mono">{id}</span>.
        </p>
        <Link
          to="/sessions"
          className="inline-flex w-fit items-center gap-1 text-xs text-blue-600 hover:underline"
        >
          <ArrowLeft className="h-3 w-3" aria-hidden />
          Back to Sessions
        </Link>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col" data-testid="agent-detail-page">
      <div className="flex items-center gap-2 border-b border-neutral-200 px-6 py-3 dark:border-neutral-800">
        <Link
          to="/sessions"
          className="inline-flex items-center gap-1 text-xs text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-100"
        >
          <ArrowLeft className="h-3 w-3" aria-hidden />
          Sessions
        </Link>
        <span className="text-xs text-neutral-400">/</span>
        <span className="font-mono text-xs">{id}</span>
      </div>
      <div className="flex-1 overflow-auto">
        <AgentDetail ctx={ctx} />
      </div>
    </div>
  );
}

// findContext resolves the route :id against the coach payload. The
// id is matched (in priority order) against:
//   1. session.id      — direct session deep-link
//   2. agent.id        — operator-typeable "open my agent" path
//   3. assignment_id   — escalation-link compatibility
// First match wins. If the operator passes a literal name (e.g. they
// hand-typed it), the agent.id fallback covers that.
export function findContext(
  sessions: PlannerOperationalContext[],
  id: string
): PlannerOperationalContext | undefined {
  if (!id) return undefined;
  return (
    sessions.find((c) => c.session?.id === id) ??
    sessions.find((c) => c.agent?.id === id) ??
    sessions.find((c) => c.session?.assignment_id === id)
  );
}
