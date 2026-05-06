import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createSpecKitFeature,
  getSpecKitFile,
  getSpecKitWorkspace,
  getSpecKitSyncPlan,
  getSpecKitSyncDraft,
  initializeSpecKitScaffold,
  listSpecKitFeatures,
  saveSpecKitFile,
  syncSpecKitFeature,
  type SpecKitFeature,
  type SpecKitFeatureList,
  type SpecKitFileContent,
  type SpecKitSyncDraft,
  type SpecKitSyncPlan,
  type SpecKitSyncResult,
  type SpecKitWorkspace,
} from '@/api/specKit';
import type { WorkItem } from '@/types/core.gen';
import { ApiError } from '@/api/client';
import { workItemsKeys } from '@/hooks/useWorkItems';

export const specKitKeys = {
  all: ['spec-kit'] as const,
  features: () => [...specKitKeys.all, 'features'] as const,
  workspace: () => [...specKitKeys.all, 'workspace'] as const,
  file: (path: string) => [...specKitKeys.all, 'file', path] as const,
  syncPlan: (id: string) => [...specKitKeys.all, 'sync-plan', id] as const,
  syncDraft: (id: string) => [...specKitKeys.all, 'sync-draft', id] as const,
};

export function useSpecKitFeatures(enabled = true) {
  return useQuery<SpecKitFeatureList, ApiError>({
    queryKey: specKitKeys.features(),
    queryFn: listSpecKitFeatures,
    enabled,
    retry: false,
  });
}

export function useSpecKitWorkspace(enabled = true) {
  return useQuery<SpecKitWorkspace, ApiError>({
    queryKey: specKitKeys.workspace(),
    queryFn: getSpecKitWorkspace,
    enabled,
    retry: false,
  });
}

export function useSpecKitFile(path: string | undefined, enabled = true) {
  return useQuery<SpecKitFileContent, ApiError>({
    queryKey: specKitKeys.file(path ?? ''),
    queryFn: () => getSpecKitFile(path as string),
    enabled: enabled && !!path,
    retry: false,
  });
}

export function useInitializeSpecKitScaffold() {
  const qc = useQueryClient();
  return useMutation<SpecKitWorkspace, ApiError>({
    mutationFn: initializeSpecKitScaffold,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: specKitKeys.all });
    },
  });
}

export function useCreateSpecKitFeature() {
  const qc = useQueryClient();
  return useMutation<SpecKitFeature, ApiError, { id?: string; title: string }>({
    mutationFn: createSpecKitFeature,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: specKitKeys.all });
    },
  });
}

export function useSaveSpecKitFile() {
  const qc = useQueryClient();
  return useMutation<SpecKitFileContent, ApiError, { path: string; content: string }>({
    mutationFn: ({ path, content }) => saveSpecKitFile(path, content),
    onSuccess: (_file, vars) => {
      qc.invalidateQueries({ queryKey: specKitKeys.file(vars.path) });
      qc.invalidateQueries({ queryKey: specKitKeys.workspace() });
      qc.invalidateQueries({ queryKey: specKitKeys.features() });
      qc.invalidateQueries({ queryKey: specKitKeys.all });
    },
  });
}

export function useSpecKitSyncPlan(id: string | undefined, enabled = true) {
  return useQuery<SpecKitSyncPlan, ApiError>({
    queryKey: specKitKeys.syncPlan(id ?? ''),
    queryFn: () => getSpecKitSyncPlan(id as string),
    enabled: enabled && !!id,
    retry: false,
  });
}

export function useSpecKitSyncDraft(id: string | undefined, enabled = true) {
  return useQuery<SpecKitSyncDraft, ApiError>({
    queryKey: specKitKeys.syncDraft(id ?? ''),
    queryFn: () => getSpecKitSyncDraft(id as string),
    enabled: enabled && !!id,
    retry: false,
  });
}

export function useSyncSpecKitFeature() {
  const qc = useQueryClient();
  return useMutation<
    SpecKitSyncResult,
    ApiError,
    { id: string; planHash: string; allowDeletes?: boolean; items?: WorkItem[] }
  >({
    mutationFn: ({ id, planHash, allowDeletes, items }) =>
      syncSpecKitFeature(id, { planHash, allowDeletes, items }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: specKitKeys.features() });
      qc.invalidateQueries({ queryKey: specKitKeys.all });
      qc.invalidateQueries({ queryKey: workItemsKeys.all });
    },
  });
}
