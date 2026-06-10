// React Query hooks for /api/agents (gm-root.10) and /api/sprints
// (gm-root.11). Both share an identical shape — small list, low
// mutation rate, picker UI consumes them — so the hook structure is
// uniform across them.

import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { listAgents } from '@/api/agents';
import { listSprints } from '@/api/sprints';
import { ApiError } from '@/api/client';
import type { AgentRef, Sprint } from '@/types/core.gen';

export const agentsKeys = {
  all: ['agents'] as const,
  list: () => [...agentsKeys.all] as const,
};

export const sprintsKeys = {
  all: ['sprints'] as const,
  list: () => [...sprintsKeys.all] as const,
};

// Same retry policy as useWorkItems: don't spam adaptor_degraded /
// not-found, otherwise allow one retry.
function retry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    if (error.isAdaptorDegraded || error.isNotFound) return false;
  }
  return failureCount < 1;
}

export function useAgents(): UseQueryResult<AgentRef[], ApiError> {
  return useQuery<AgentRef[], ApiError>({
    queryKey: agentsKeys.list(),
    queryFn: listAgents,
    retry,
    // Agent rosters change rarely; keep results fresh-ish without
    // hammering the orchestrator. The picker re-validates on focus
    // anyway via react-query defaults.
    staleTime: 30_000,
  });
}

export function useSprints(): UseQueryResult<Sprint[], ApiError> {
  return useQuery<Sprint[], ApiError>({
    queryKey: sprintsKeys.list(),
    queryFn: listSprints,
    retry,
    staleTime: 30_000,
  });
}
