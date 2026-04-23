// React Query hooks for the /api/beads surface (gm-xgm / M1.6).
//
// These wrap the thin async functions in src/api/beads.ts so consumers
// get a uniform {data, isLoading, error} tuple without each call site
// wiring up its own useQuery. The board pane (M1.7a) and drawer
// (M1.7c) are the primary callers; SSE invalidation (gm-e2.5) will
// refresh specific keys once the event hub lands.
//
// Key layout:
//   ['beads']            — list
//   ['beads', id]        — single bead with full relationship graph
//
// beadsKeys is exported so callers can invalidate surgically:
//   queryClient.invalidateQueries({ queryKey: beadsKeys.detail(id) })

import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { getBead, listBeads } from '@/api/beads';
import { ApiError } from '@/api/client';
import type { WorkItem } from '@/types/core.gen';

export const beadsKeys = {
  all: ['beads'] as const,
  list: () => [...beadsKeys.all] as const,
  detail: (id: string) => [...beadsKeys.all, id] as const,
};

// Adaptor-degraded failures are expected while the adaptor is
// reconnecting; retrying at React Query's default cadence produces
// noise. We disable retry for that kind and let the default policy
// handle everything else.
function retry(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    if (error.isAdaptorDegraded || error.isNotFound) {
      return false;
    }
  }
  return failureCount < 1;
}

export function useBeads(): UseQueryResult<WorkItem[], ApiError> {
  return useQuery<WorkItem[], ApiError>({
    queryKey: beadsKeys.list(),
    queryFn: listBeads,
    retry,
  });
}

// useBead skips the query when id is empty — callers can pass "" to
// keep hook order stable when no row is selected.
export function useBead(id: string | undefined): UseQueryResult<WorkItem, ApiError> {
  return useQuery<WorkItem, ApiError>({
    queryKey: beadsKeys.detail(id ?? ''),
    queryFn: () => getBead(id as string),
    enabled: !!id,
    retry,
  });
}
