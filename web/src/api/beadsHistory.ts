import { apiFetch } from './client';

export interface BeadsHistoryEntity {
  type: string;
  id: string;
  title?: string;
}

export interface BeadsHistoryEvent {
  event_id: string;
  occurred_at: string;
  actor: string;
  mode: 'beads_only';
  action: string;
  entity: BeadsHistoryEntity;
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  summary: string;
}

export interface BeadsHistoryResponse {
  mode: string;
  path?: string;
  entries: BeadsHistoryEvent[];
  malformed?: number;
  error?: string;
}

export async function getBeadsHistory(): Promise<BeadsHistoryResponse> {
  const env = await apiFetch<BeadsHistoryResponse>('/beads-history');
  return { ...env, entries: env.entries ?? [] };
}
