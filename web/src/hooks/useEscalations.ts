// React Query hooks for /api/escalations (gm-native.16). Polls at 2s
// when scoped to a session — the open EscalationPanel is the
// operator's interactive surface, so we trade a bit of network chatter
// for sub-2s latency on permission-prompt arrival. Global list polls
// at 5s (cheaper, mostly a count badge).
//
// Polling here is a stand-in until the orchestration SSE feed lands;
// then this hook switches to invalidate-on-event with the same key
// shape and the SessionsPage row badge upgrades to push-fresh.

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
    refetchInterval: sessionId ? 2_000 : 5_000,
    staleTime: sessionId ? 1_000 : 2_500,
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
