// fixtures/sessionStore.ts (gm-5v8v.9).
//
// In-memory Sessions store consulted by the fake-backend route
// dispatcher. Mirrors fixtures/workplane.ts shape; per-test scope so
// seeded data can't bleed across specs.
//
// Specs that need to seed sessions + then assert on what the SPA
// renders use this fixture; specs that just want the empty-state get
// the default empty list.

// Session shape mirrors web/src/api/sessions.ts's Session interface,
// re-declared here because that file imports from `@/types/core.gen`
// — an alias only the SPA's tsconfig knows about. Keep this shape in
// sync with the SPA file when the wire grows new fields.
import type { SessionStatus } from '../../../web/src/types/core.gen';

export interface Session {
  id: string;
  assignment_id: string;
  agent_id: string;
  status: SessionStatus;
  active_turn_id?: string;
  started_at: string;
  ended_at?: string | null;
  last_heartbeat?: string | null;
  transcript_ref?: string;
  close_reason?: string | null;
  provider_metadata?: Record<string, unknown>;
}

export interface SessionStore {
  seed(items: Session[]): void;
  add(item: Session): void;
  remove(id: string): void;
  get(id: string): Session | undefined;
  list(): Session[];
  clear(): void;
}

export function createSessionStore(): SessionStore {
  const byId = new Map<string, Session>();
  return {
    seed(items) {
      byId.clear();
      for (const it of items) byId.set(it.id, it);
    },
    add(item) {
      byId.set(item.id, item);
    },
    remove(id) {
      byId.delete(id);
    },
    get(id) {
      return byId.get(id);
    },
    list() {
      return Array.from(byId.values());
    },
    clear() {
      byId.clear();
    },
  };
}
