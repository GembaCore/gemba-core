// React Query hook for /api/v1/personas (gm-e11.8.7).
//
// Used by the EscalationsPage Hand-off modal to populate the persona
// dropdown with the live registry instead of the DEFAULT_PERSONAS
// constants. Cached aggressively because the persona registry is
// loaded at server startup and only mutates on workspace switch — a
// 60s stale time is plenty.

import {
  useQuery,
  type UseQueryResult,
} from '@tanstack/react-query';
import { listPersonas, type PersonaSummary } from '@/api/personas';
import { ApiError } from '@/api/client';

export const personasKeys = {
  all: ['personas'] as const,
};

function retry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    if (error.isAdaptorDegraded || error.isNotFound) return false;
  }
  return failureCount < 1;
}

export function usePersonas(): UseQueryResult<PersonaSummary[], ApiError> {
  return useQuery<PersonaSummary[], ApiError>({
    queryKey: personasKeys.all,
    queryFn: () => listPersonas(),
    retry,
    staleTime: 60_000,
  });
}
