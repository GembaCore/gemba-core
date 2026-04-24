// React Query hooks for /api/sessions (gm-native.15). Mirrors the
// useAgents / useWorkItems pattern: small picker-friendly list,
// retry-shy on adaptor-degraded, mutation hooks invalidate the list.
//
// Polling cadence: 5s while no SSE feed is wired (gm-native.11 will
// graduate this to event-driven invalidation). Lower than work-items'
// staleTime because the SessionsPage is the operator's eyes on live
// panes — staleness here means End / New shows out-of-date state.

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import {
  endSession,
  listSessions,
  startSession,
  type Session,
  type SessionEndMode,
  type SessionListFilter,
  type StartSessionRequest,
} from '@/api/sessions';
import { ApiError } from '@/api/client';

export const sessionsKeys = {
  all: ['sessions'] as const,
  list: (filter?: SessionListFilter) =>
    [...sessionsKeys.all, normalizeFilter(filter)] as const,
};

function normalizeFilter(f?: SessionListFilter): SessionListFilter {
  if (!f) return {};
  const out: SessionListFilter = {};
  if (f.include_terminal) out.include_terminal = true;
  if (f.agent_id) out.agent_id = f.agent_id;
  if (f.status && f.status.length > 0) out.status = [...f.status].sort();
  return out;
}

function retry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    if (error.isAdaptorDegraded || error.isNotFound) return false;
  }
  return failureCount < 1;
}

export function useSessions(filter?: SessionListFilter): UseQueryResult<Session[], ApiError> {
  return useQuery<Session[], ApiError>({
    queryKey: sessionsKeys.list(filter),
    queryFn: () => listSessions(filter),
    retry,
    refetchInterval: 5_000,
    staleTime: 2_500,
  });
}

export function useStartSession(): UseMutationResult<Session, ApiError, StartSessionRequest> {
  const qc = useQueryClient();
  return useMutation<Session, ApiError, StartSessionRequest>({
    mutationFn: (body) => startSession(body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sessionsKeys.all });
    },
  });
}

export interface EndSessionInput {
  id: string;
  mode?: SessionEndMode;
}

export function useEndSession(): UseMutationResult<Session, ApiError, EndSessionInput> {
  const qc = useQueryClient();
  return useMutation<Session, ApiError, EndSessionInput>({
    mutationFn: ({ id, mode }) => endSession(id, mode),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sessionsKeys.all });
    },
  });
}
