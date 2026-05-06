import { useMutation, useQueryClient } from '@tanstack/react-query';
import { applyBootstrapDraft, type BootstrapDraftApplyResult } from '@/api/bootstrapDrafts';
import { ApiError } from '@/api/client';
import { freshNonce } from '@/api/consults';
import { workItemsKeys } from '@/hooks/useWorkItems';
import type { WorkItem } from '@/types/core.gen';

export function useApplyBootstrapDraft() {
  const qc = useQueryClient();
  return useMutation<
    BootstrapDraftApplyResult,
    ApiError,
    { items: WorkItem[]; targetDatabase?: string }
  >({
    mutationFn: ({ items, targetDatabase }) =>
      applyBootstrapDraft(items, { targetDatabase, nonce: freshNonce() }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workItemsKeys.all });
    },
  });
}
