// builders/session.ts (gm-5v8v.9). Typed Session builder for spec
// setup. Mirrors fixtures/sessionStore.ts's local Session shape; the
// SPA's Session type is not importable here because web/src/api/
// sessions.ts uses the @/types alias the e2e tsconfig doesn't carry.

import type { Session } from '../fixtures/sessionStore';

let counter = 0;
const FIXED_TIMESTAMP = '2026-04-25T12:00:00Z';

function nextId(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}

export function session(overrides: Partial<Session> = {}): Session {
  const id = overrides.id ?? nextId('sess');
  return {
    id,
    assignment_id: 'gm-fixture-1',
    agent_id: 'agent',
    status: 'ready',
    started_at: FIXED_TIMESTAMP,
    provider_metadata: {
      bead_id: 'gm-fixture-1',
      agent_type: 'claude',
      worktree: '/tmp/fixture-worktree',
    },
    ...overrides,
  };
}

export function resetSessionIds(): void {
  counter = 0;
}
