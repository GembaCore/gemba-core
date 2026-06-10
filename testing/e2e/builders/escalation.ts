// builders/escalation.ts (gm-5v8v.9). Typed EscalationRequest builder
// for spec setup. Mirrors fixtures/escalationStore.ts's local
// EscalationRequest shape.

import type { EscalationRequest } from '../fixtures/escalationStore';

let counter = 0;
const FIXED_TIMESTAMP = '2026-04-25T12:00:00Z';

function nextId(): string {
  counter += 1;
  return `esc-${counter}`;
}

export function escalation(overrides: Partial<EscalationRequest> = {}): EscalationRequest {
  return {
    id: overrides.id ?? nextId(),
    source: 'permission_prompt',
    urgency: 'blocking',
    title: 'Approve operation?',
    prompt: 'The agent wants to do something — approve / deny / modify.',
    state: 'open',
    created_at: FIXED_TIMESTAMP,
    ...overrides,
  };
}

export function resetEscalationIds(): void {
  counter = 0;
}
