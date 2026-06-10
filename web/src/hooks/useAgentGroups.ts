// React Query hook for /api/agent-groups (gm-e12.8).
//
// Drives the SPA's AgentGroupBoard, which dispatches on GroupMode
// (static | pool | graph). Same retry/staleTime convention as useAgents
// — group rosters change rarely, the page re-validates on focus.

import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { listAgentGroups } from '@/api/agentGroups';
import { ApiError } from '@/api/client';
import type { AgentGroup } from '@/types/core.gen';

export const agentGroupsKeys = {
  all: ['agent-groups'] as const,
  list: () => [...agentGroupsKeys.all] as const,
};

function retry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    if (error.isAdaptorDegraded || error.isNotFound) return false;
  }
  return failureCount < 1;
}

export function useAgentGroups(): UseQueryResult<AgentGroup[], ApiError> {
  return useQuery<AgentGroup[], ApiError>({
    queryKey: agentGroupsKeys.list(),
    queryFn: listAgentGroups,
    retry,
    staleTime: 30_000,
  });
}
