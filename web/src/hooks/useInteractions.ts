import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ensureInteraction, sendInteractionTurn } from '@/api/interactions';
import type { InteractionKind, InteractionScope, InteractionSession } from '@/interactions/types';
import { ApiError } from '@/api/client';

export const interactionKeys = {
  all: ['interactions'] as const,
  ensure: (kind: InteractionKind, scope: InteractionScope) =>
    [...interactionKeys.all, kind, scope.type, scope.id] as const,
};

export function useEnsureInteraction(
  scope: InteractionScope,
  kind: InteractionKind = 'pm_consult'
) {
  return useQuery<InteractionSession, ApiError>({
    queryKey: interactionKeys.ensure(kind, scope),
    queryFn: () => ensureInteraction({ kind, scope }),
    staleTime: 30_000,
    retry: false,
  });
}

export function useSendInteractionTurn() {
  const qc = useQueryClient();
  return useMutation<
    InteractionSession,
    ApiError,
    { id: string; message: string; kind: InteractionKind; scope: InteractionScope }
  >({
    mutationFn: ({ id, message }) => sendInteractionTurn({ id, message }),
    onSuccess: (session, vars) => {
      qc.setQueryData(interactionKeys.ensure(vars.kind, vars.scope), session);
    },
  });
}
