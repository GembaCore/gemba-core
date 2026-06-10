// Typed data-access for the /api/agent-groups surface (gm-e12.8).
//
// The orchestrator's known AgentGroups, dispatched on GroupMode by the
// AgentGroupBoard (static | pool | graph). Empty list when no
// orchestration plane is bound — render a "no groups" empty state.

import { apiFetch } from './client';
import type { AgentGroup } from '@/types/core.gen';

export interface ListAgentGroupsEnvelope {
  groups: AgentGroup[];
  total: number;
}

export async function listAgentGroups(): Promise<AgentGroup[]> {
  const env = await apiFetch<ListAgentGroupsEnvelope>('/agent-groups');
  return env.groups ?? [];
}
