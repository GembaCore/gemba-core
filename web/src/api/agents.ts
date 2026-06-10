// Typed data-access for the /api/agents surface (gm-root.10).
//
// Empty-list semantics: the server returns 200 + {agents: []} when no
// orchestration plane is bound. Callers MUST treat empty as "picker
// has no roster" and fall back to the freeform AgentEditor — not as
// an error.

import { apiFetch } from './client';
import type { AgentRef } from '@/types/core.gen';

export interface ListAgentsEnvelope {
  agents: AgentRef[];
  total: number;
}

export async function listAgents(): Promise<AgentRef[]> {
  const env = await apiFetch<ListAgentsEnvelope>('/agents');
  return env.agents ?? [];
}
