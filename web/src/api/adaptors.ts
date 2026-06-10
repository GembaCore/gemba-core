import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';

export interface AdaptorStatus {
  name: string;
  plane: 'work' | 'orchestration';
  healthy: boolean;
  reason?: string;
}

export interface AdaptorsResponse {
  instance_id?: string;
  adaptors: AdaptorStatus[];
}

export interface BeadsSource {
  kind: string;
  label?: string;
  detail?: string;
}

export interface BeadsHealthAction {
  id: string;
  label: string;
  description: string;
  destructive?: boolean;
}

export interface BeadsHealthActionResult {
  action: string;
  ok: boolean;
  message: string;
  output?: string;
  exit_code?: number;
}

export interface BeadsHealthResponse {
  source: BeadsSource;
  current_db: string;
  remote_configured: boolean;
  remote_kind: string;
  remote_status_label: string;
  adaptor?: AdaptorStatus;
  actions: BeadsHealthAction[];
  last_action?: BeadsHealthActionResult;
}

export async function getAdaptors(options: { refresh?: boolean } = {}): Promise<AdaptorsResponse> {
  const env = await apiFetch<AdaptorsResponse>(options.refresh ? '/adaptors?refresh=1' : '/adaptors');
  return { ...env, adaptors: env.adaptors ?? [] };
}

export async function getBeadsHealth(): Promise<BeadsHealthResponse> {
  return apiFetch<BeadsHealthResponse>('/beads/health');
}

export async function runBeadsHealthAction(action: string): Promise<BeadsHealthResponse> {
  return apiFetch<BeadsHealthResponse>('/beads/health/actions', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: freshNonce(),
    },
    body: JSON.stringify({ action }),
  });
}

function freshNonce(): string {
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
