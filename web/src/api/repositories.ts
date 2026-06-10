// Typed data-access for the /api/repositories surface (gm-uipx.8).
//
// The endpoint always returns 200; missing-dir / load-error states
// surface as `notice` strings on an empty-list payload so the SPA
// can render a helpful empty card without flagging a fault.

import { apiFetch } from './client';
import type { Repository } from '@/types/core.gen';

export interface ListRepositoriesEnvelope {
  repositories: Repository[];
  notice?: string;
}

export async function listRepositories(): Promise<ListRepositoriesEnvelope> {
  const env = await apiFetch<ListRepositoriesEnvelope>('/repositories');
  return {
    repositories: env.repositories ?? [],
    notice: env.notice,
  };
}
