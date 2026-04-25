// fixtures/agentStore.ts (gm-5v8v.9).
//
// In-memory roster store. Fed to the fake /api/agents handler.

import type { AgentRef } from '../../../web/src/types/core.gen';

export interface AgentStore {
  seed(items: AgentRef[]): void;
  list(): AgentRef[];
  clear(): void;
}

export function createAgentStore(): AgentStore {
  let items: AgentRef[] = [];
  return {
    seed(next) {
      items = [...next];
    },
    list() {
      return items.slice();
    },
    clear() {
      items = [];
    },
  };
}
