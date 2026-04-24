// React Query hooks for the /api/work-items surface (gm-xgm / M1.6).
//
// These wrap the thin async functions in src/api/work-items.ts so consumers
// get a uniform {data, isLoading, error} tuple without each call site
// wiring up its own useQuery. The board pane (M1.7a) and drawer
// (M1.7c) are the primary callers; SSE invalidation (gm-e2.5) will
// refresh specific keys once the event hub lands.
//
// Key layout:
//   ['beads']            — list
//   ['beads', id]        — single bead with full relationship graph
//
// workItemsKeys is exported so callers can invalidate surgically:
//   queryClient.invalidateQueries({ queryKey: workItemsKeys.detail(id) })

import {
  useMutation,
  useQueryClient,
  useQuery,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import {
  getWorkItem,
  listWorkItems,
  updateWorkItem,
  type WorkItemListFilter,
  type WorkItemPatch,
} from '@/api/workItems';
import { ApiError } from '@/api/client';
import type { WorkItem } from '@/types/core.gen';

export const workItemsKeys = {
  all: ['beads'] as const,
  list: () => [...workItemsKeys.all] as const,
  detail: (id: string) => [...workItemsKeys.all, id] as const,
  // filtered is a sibling cache keyed by the filter payload so
  // different filter shapes don't collide with the unfiltered list.
  filtered: (filter: WorkItemListFilter) =>
    [...workItemsKeys.all, 'filtered', normalizeFilter(filter)] as const,
};

// normalizeFilter produces a stable key from a filter object — sorted
// arrays, empty fields stripped — so equivalent filters share a cache
// entry regardless of how the caller constructed them.
function normalizeFilter(filter: WorkItemListFilter): WorkItemListFilter {
  const out: WorkItemListFilter = {};
  const sortCopy = (xs: string[] | undefined) =>
    xs && xs.length > 0 ? [...xs].sort() : undefined;
  const states = sortCopy(filter.state_category);
  if (states) out.state_category = states as WorkItemListFilter['state_category'];
  const kind = sortCopy(filter.kind);
  if (kind) out.kind = kind;
  const label = sortCopy(filter.label);
  if (label) out.label = label;
  const status = sortCopy(filter.status);
  if (status) out.status = status;
  if (filter.assignee_id) out.assignee_id = filter.assignee_id;
  if (filter.sprint_id) out.sprint_id = filter.sprint_id;
  if (filter.limit != null) out.limit = filter.limit;
  return out;
}

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

export function useWorkItems(): UseQueryResult<WorkItem[], ApiError> {
  return useQuery<WorkItem[], ApiError>({
    queryKey: workItemsKeys.list(),
    queryFn: () => listWorkItems(),
    retry,
  });
}

// useFilteredWorkItems drives GET /api/work-items with a non-empty
// filter (gm-e12.9.1). Callers pass the canonical filter shape from
// api/workItems.ts; the hook memoises into a filter-keyed cache so
// filter changes cause a refetch only when the normalised payload
// differs.
export function useFilteredWorkItems(
  filter: WorkItemListFilter
): UseQueryResult<WorkItem[], ApiError> {
  return useQuery<WorkItem[], ApiError>({
    queryKey: workItemsKeys.filtered(filter),
    queryFn: () => listWorkItems(filter),
    retry,
  });
}

// useWorkItem skips the query when id is empty — callers can pass "" to
// keep hook order stable when no row is selected.
export function useWorkItem(id: string | undefined): UseQueryResult<WorkItem, ApiError> {
  return useQuery<WorkItem, ApiError>({
    queryKey: workItemsKeys.detail(id ?? ''),
    queryFn: () => getWorkItem(id as string),
    enabled: !!id,
    retry,
  });
}

// useUpdateWorkItem drives PATCH /api/work-items/{id} (gm-root.8 slice 2).
// Optimistically writes the patch into the react-query cache so the
// drawer reflects the change immediately; rolls back on error so a
// 405 / 503 / 422 from the server doesn't leave the UI desynced.
//
// On success: invalidates both the list and the detail entry so the
// next render fetches the materialized record (including any
// adaptor-derived fields the patch didn't touch — e.g. updated_at).
//
// Variables shape — `{ id, patch, nonce? }` — keeps the hook generic
// over which drawer/menu fired the mutation. Pass `nonce` only when
// retrying an in-flight request; the api layer mints fresh tokens
// otherwise.
export interface UpdateWorkItemVars {
  id: string;
  patch: WorkItemPatch;
  nonce?: string;
}

interface UpdateRollback {
  prevDetail: WorkItem | undefined;
  prevList: WorkItem[] | undefined;
}

export function useUpdateWorkItem(): UseMutationResult<
  WorkItem,
  ApiError,
  UpdateWorkItemVars,
  UpdateRollback
> {
  const qc = useQueryClient();
  return useMutation<WorkItem, ApiError, UpdateWorkItemVars, UpdateRollback>({
    mutationFn: ({ id, patch, nonce }) => updateWorkItem(id, patch, { nonce }),
    onMutate: async ({ id, patch }) => {
      // Cancel in-flight queries so an optimistic write isn't trampled
      // by a slow background refetch landing after our mutation.
      await qc.cancelQueries({ queryKey: workItemsKeys.all });

      const prevDetail = qc.getQueryData<WorkItem>(workItemsKeys.detail(id));
      const prevList = qc.getQueryData<WorkItem[]>(workItemsKeys.list());

      if (prevDetail) {
        qc.setQueryData<WorkItem>(workItemsKeys.detail(id), {
          ...prevDetail,
          ...applyPatch(patch),
        });
      }
      if (prevList) {
        qc.setQueryData<WorkItem[]>(
          workItemsKeys.list(),
          prevList.map((it) => (it.id === id ? { ...it, ...applyPatch(patch) } : it))
        );
      }
      return { prevDetail, prevList };
    },
    onError: (_err, vars, ctx) => {
      if (!ctx) return;
      if (ctx.prevDetail !== undefined) {
        qc.setQueryData(workItemsKeys.detail(vars.id), ctx.prevDetail);
      }
      if (ctx.prevList !== undefined) {
        qc.setQueryData(workItemsKeys.list(), ctx.prevList);
      }
    },
    onSettled: (_data, _err, vars) => {
      // Whether success or rollback, refetch from authoritative source
      // — the server may have applied additional adaptor-side mutations
      // (timestamps, derived signals).
      qc.invalidateQueries({ queryKey: workItemsKeys.detail(vars.id) });
      qc.invalidateQueries({ queryKey: workItemsKeys.list() });
    },
  });
}

// applyPatch projects a WorkItemPatch onto a WorkItem so the
// optimistic cache write reflects what the server *should* return.
// Mirrors the WorkItemPatch shape; fields not in the patch are left
// untouched (the caller spreads the original item alongside the
// projection in setQueryData, preserving anything we didn't touch).
function applyPatch(patch: WorkItemPatch): Partial<WorkItem> {
  const out: Partial<WorkItem> = {};
  if (patch.title !== undefined) out.title = patch.title;
  if (patch.description !== undefined) out.description = patch.description;
  if (patch.status !== undefined) out.status = patch.status;
  if (patch.state_category !== undefined) out.state_category = patch.state_category;
  if (patch.priority !== undefined) out.priority = patch.priority;
  if (patch.labels !== undefined) out.labels = patch.labels;
  if (patch.owner !== undefined) out.owner = patch.owner ?? undefined;
  if (patch.assignee !== undefined) out.assignee = patch.assignee ?? undefined;
  if (patch.sprint_id !== undefined) out.sprint_id = patch.sprint_id ?? undefined;
  if (patch.dod !== undefined) out.dod = patch.dod ?? undefined;
  if (patch.custom !== undefined) out.custom = patch.custom;
  // bump updated_at so relativeTime() in cards / drawer reflects the
  // optimistic change immediately. Server overwrites with its own
  // timestamp on the next refetch.
  out.updated_at = new Date().toISOString();
  return out;
}

