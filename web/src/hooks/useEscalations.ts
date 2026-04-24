// React Query hooks for /api/escalations (gm-native.16).
//
// Invalidation: SSE-driven via @/data/sse (gm-e12.2). Both
// escalation.opened and escalation.resolved events on the /events
// stream invalidate ['escalations'], which refreshes the per-session
// panel and the SessionsPage badge in lock-step. No polling.

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import {
  listEscalations,
  respondEscalation,
  type EscalationRequest,
  type RespondEscalationRequest,
} from '@/api/escalations';
import { ApiError } from '@/api/client';

export const escalationsKeys = {
  all: ['escalations'] as const,
  list: (sessionId?: string) =>
    sessionId ? ([...escalationsKeys.all, 'session', sessionId] as const) : ([...escalationsKeys.all, 'all'] as const),
};

function retry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    if (error.isAdaptorDegraded || error.isNotFound) return false;
  }
  return failureCount < 1;
}

export function useEscalations(sessionId?: string): UseQueryResult<EscalationRequest[], ApiError> {
  return useQuery<EscalationRequest[], ApiError>({
    queryKey: escalationsKeys.list(sessionId),
    queryFn: () => listEscalations(sessionId),
    retry,
    staleTime: 2_500,
  });
}

export interface RespondEscalationInput {
  id: string;
  body: RespondEscalationRequest;
}

export function useRespondEscalation(): UseMutationResult<
  EscalationRequest,
  ApiError,
  RespondEscalationInput
> {
  const qc = useQueryClient();
  return useMutation<EscalationRequest, ApiError, RespondEscalationInput>({
    mutationFn: ({ id, body }) => respondEscalation(id, body),
    onSettled: () => {
      // Invalidate every escalation list — both the global badge feed
      // and any per-session panel — so a resolved escalation drops out
      // of all open panels at once.
      qc.invalidateQueries({ queryKey: escalationsKeys.all });
    },
  });
}
