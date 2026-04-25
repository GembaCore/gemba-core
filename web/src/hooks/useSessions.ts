// React Query hooks for /api/sessions (gm-native.15). Mirrors the
// useAgents / useWorkItems pattern: small picker-friendly list,
// retry-shy on adaptor-degraded, mutation hooks invalidate the list.
//
// Invalidation: SSE-driven via @/data/sse (gm-e12.2). The /events
// stream maps session.transition / session.state_reported events to
// invalidations on ['sessions'], so the list refreshes on push
// instead of poll. A short staleTime keeps mounted pages from
// re-fetching on every render but doesn't hammer the server.

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
  peekSession,
  startSession,
  type Session,
  type SessionEndMode,
  type SessionListFilter,
  type SessionPeek,
  type StartSessionRequest,
} from '@/api/sessions';
import { ApiError } from '@/api/client';

export const sessionsKeys = {
  all: ['sessions'] as const,
  list: (filter?: SessionListFilter) =>
    [...sessionsKeys.all, normalizeFilter(filter)] as const,
  // Peek shares the ['sessions'] prefix so a global invalidate (the
  // SSE pipeline fires on session.transition / state_reported) also
  // refetches every open peek. gm-5uqu.
  peek: (id: string) => [...sessionsKeys.all, id, 'peek'] as const,
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
    staleTime: 2_500,
  });
}

// useSessionPeek fetches the live transcript snapshot for a session
// (gm-5uqu). Disabled when id is empty so the AgentDetailDrawer can
// render the peek pane unconditionally and gate the call by passing a
// nullable id. Refetches automatically on every ['sessions']
// invalidation thanks to the shared key prefix, so transcript stays
// inside the SSE-invalidation budget without a dedicated topic.
//
// Errors don't retry on degraded / not-found / unsupported — those are
// rendered as steady-state "transcript unavailable" affordances rather
// than transient failures worth re-trying.
export function useSessionPeek(id: string | null | undefined): UseQueryResult<SessionPeek, ApiError> {
  return useQuery<SessionPeek, ApiError>({
    queryKey: id ? sessionsKeys.peek(id) : ['sessions', 'peek', 'disabled'],
    queryFn: () => peekSession(id!),
    enabled: Boolean(id),
    retry,
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
