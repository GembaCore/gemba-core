// builders/agent.ts (gm-5v8v.9). Typed AgentRef builder for spec
// setup. Imports directly from web/src/types/core.gen — the
// generated module has no `@/` aliases to break the e2e tsconfig.

import type { AgentRef } from '../../../web/src/types/core.gen';

let counter = 0;

function nextId(): string {
  counter += 1;
  return `gemba/crew/test-${counter}`;
}

export function agent(overrides: Partial<AgentRef> = {}): AgentRef {
  return {
    id: overrides.id ?? nextId(),
    name: 'test-agent',
    agent_kind: 'agent',
    role: 'crew',
    dialect: 'claude',
    ...overrides,
  };
}

export function resetAgentIds(): void {
  counter = 0;
}
