import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';
import type { WorkItem } from '@/types/core.gen';

export interface SpecKitUserStory {
  id: string;
  title: string;
  priority?: string;
  acceptance_scenarios?: string[];
}

export interface SpecKitTask {
  id: string;
  title: string;
  phase?: string;
  story_id?: string;
  parallel: boolean;
  done: boolean;
  line?: number;
  description?: string;
}

export interface SpecKitFeature {
  id: string;
  title: string;
  directory: string;
  spec_path?: string;
  plan_path?: string;
  tasks_path?: string;
  has_spec: boolean;
  has_plan: boolean;
  has_tasks: boolean;
  spec: {
    title?: string;
    user_stories?: SpecKitUserStory[];
    acceptance_scenarios?: string[];
    functional_requirements?: string[];
  };
  tasks: SpecKitTask[];
  task_count: number;
  parallel_task_count: number;
}

export interface SpecKitFeatureList {
  configured: boolean;
  features: SpecKitFeature[];
  total: number;
}

export interface SpecKitWorkspaceFile {
  path: string;
  name: string;
  role: string;
  feature_id?: string;
  size: number;
  modified?: string;
}

export interface SpecKitWorkspace {
  configured: boolean;
  scaffold_present: boolean;
  root: string;
  files: SpecKitWorkspaceFile[];
  features: SpecKitFeature[];
  recommended_root: string;
  initialized_paths?: string[];
}

export interface SpecKitFileContent {
  path: string;
  content: string;
  modified?: string;
  size: number;
}

export interface SpecKitSyncResult {
  feature_id: string;
  milestone_id?: string;
  epic_id?: string;
  story_ids?: Record<string, string>;
  task_ids?: Record<string, string>;
  plan: SpecKitSyncPlan;
  created?: string[];
  updated?: string[];
  deleted?: string[];
  task_count: number;
  story_count: number;
}

export interface SpecKitSyncPlan {
  feature_id: string;
  changes: SpecKitSyncChange[];
  counts: { create: number; update: number; delete: number };
  hash: string;
  jsonl?: string;
  warnings?: string[];
}

export interface SpecKitSyncDraft {
  feature_id: string;
  plan: SpecKitSyncPlan;
  items: WorkItem[];
  warnings?: string[];
}

export interface SpecKitSyncChange {
  action: 'create' | 'update' | 'delete';
  key: string;
  kind: string;
  source_id?: string;
  id?: string;
  title: string;
  summary: string;
}

export async function listSpecKitFeatures(): Promise<SpecKitFeatureList> {
  const result = await apiFetch<SpecKitFeatureList>('/spec-kit/features');
  return {
    configured: result.configured,
    features: result.features ?? [],
    total: result.total ?? 0,
  };
}

export async function getSpecKitWorkspace(): Promise<SpecKitWorkspace> {
  const result = await apiFetch<SpecKitWorkspace>('/spec-kit/workspace');
  return {
    configured: result.configured,
    scaffold_present: result.scaffold_present,
    root: result.root,
    files: result.files ?? [],
    features: result.features ?? [],
    recommended_root: result.recommended_root || 'specs',
    initialized_paths: result.initialized_paths ?? [],
  };
}

export async function initializeSpecKitScaffold(): Promise<SpecKitWorkspace> {
  return apiFetch<SpecKitWorkspace>('/spec-kit/scaffold', {
    method: 'POST',
    headers: {
      [CONFIRM_HEADER]: freshNonce(),
    },
  });
}

export async function createSpecKitFeature(opts: {
  id?: string;
  title: string;
}): Promise<SpecKitFeature> {
  return apiFetch<SpecKitFeature>('/spec-kit/features', {
    method: 'POST',
    headers: {
      [CONFIRM_HEADER]: freshNonce(),
    },
    body: JSON.stringify({ id: opts.id, title: opts.title }),
  });
}

export async function getSpecKitFile(path: string): Promise<SpecKitFileContent> {
  if (!path) {
    throw new Error('getSpecKitFile: path is required');
  }
  return apiFetch<SpecKitFileContent>(`/spec-kit/files?path=${encodeURIComponent(path)}`);
}

export async function saveSpecKitFile(path: string, content: string): Promise<SpecKitFileContent> {
  if (!path) {
    throw new Error('saveSpecKitFile: path is required');
  }
  return apiFetch<SpecKitFileContent>(`/spec-kit/files?path=${encodeURIComponent(path)}`, {
    method: 'PUT',
    headers: {
      [CONFIRM_HEADER]: freshNonce(),
    },
    body: JSON.stringify({ content }),
  });
}

export async function getSpecKitSyncPlan(id: string): Promise<SpecKitSyncPlan> {
  if (!id) {
    throw new Error('getSpecKitSyncPlan: id is required');
  }
  return apiFetch<SpecKitSyncPlan>(`/spec-kit/features/${encodeURIComponent(id)}/sync-plan`);
}

export async function getSpecKitSyncDraft(id: string): Promise<SpecKitSyncDraft> {
  if (!id) {
    throw new Error('getSpecKitSyncDraft: id is required');
  }
  return apiFetch<SpecKitSyncDraft>(`/spec-kit/features/${encodeURIComponent(id)}/draft`);
}

export async function syncSpecKitFeature(
  id: string,
  opts: { planHash: string; allowDeletes?: boolean; items?: WorkItem[] }
): Promise<SpecKitSyncResult> {
  if (!id) {
    throw new Error('syncSpecKitFeature: id is required');
  }
  if (!opts.planHash) {
    throw new Error('syncSpecKitFeature: planHash is required');
  }
  return apiFetch<SpecKitSyncResult>(`/spec-kit/features/${encodeURIComponent(id)}/sync-to-beads`, {
    method: 'POST',
    headers: {
      [CONFIRM_HEADER]: freshNonce(),
    },
    body: JSON.stringify({
      plan_hash: opts.planHash,
      allow_deletes: !!opts.allowDeletes,
      items: opts.items,
    }),
  });
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
