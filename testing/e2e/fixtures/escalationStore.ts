// fixtures/escalationStore.ts (gm-5v8v.9).
//
// In-memory Escalations store consulted by the fake-backend route
// dispatcher. Per-test scope; specs seed open escalations and assert
// on the resolution writeback through POST /escalations/:id/respond.

// EscalationRequest shape mirrors web/src/api/escalations.ts. Redeclared
// here because that file imports from `@/types/core.gen` — an alias the
// SPA's tsconfig owns. Keep in sync when the wire grows new fields.

export type EscalationKind =
  | 'mcp_elicitation'
  | 'a2a_input_required'
  | 'permission_prompt'
  | 'hitl_approval'
  | 'orchestrator_pause'
  | 'blocker'
  | 'question'
  | 'witness_finding'
  | 'refinery_rejection'
  | 'beads_degraded';

export type EscalationUrgency = 'blocking' | 'advisory';
export type EscalationState = 'open' | 'resolved' | 'canceled' | 'expired';
export type EscalationResolutionKind = 'approve' | 'deny' | 'modify' | 'defer';

export interface EscalationOption {
  value: string;
  label: string;
}

export interface EscalationResolution {
  kind: EscalationResolutionKind;
  value?: unknown;
  resolved_by: string;
  resolved_at: string;
}

export interface EscalationRequest {
  id: string;
  source: EscalationKind;
  channel?: 'notification' | 'tool_call';
  // assignment_id is the bead/work-item the escalation belongs to
  // (core.EscalationRequest.AssignmentID). Used by the global inbox
  // badge count (keyed on session.assignment_id).
  assignment_id?: string;
  // session_id is the session whose ID the fake-backend filter receives
  // (?session_id= query param). The real server indexes by session ID
  // (native escalationIndex.bySession[ev.SessionID]); the fake mirrors
  // that by filtering on this field (gm-e11.8.6).
  session_id?: string;
  work_item_id?: string;
  agent_id?: string;
  urgency: EscalationUrgency;
  title: string;
  prompt: string;
  options?: EscalationOption[];
  deadline?: string | null;
  state: EscalationState;
  resolution?: EscalationResolution | null;
  created_at: string;
}

export interface ResponseLog {
  id: string;
  body: Record<string, unknown>;
}

export interface EscalationStore {
  seed(items: EscalationRequest[]): void;
  add(item: EscalationRequest): void;
  resolve(id: string, body: Record<string, unknown>): void;
  list(sessionId?: string): EscalationRequest[];
  responses(): ResponseLog[];
  clear(): void;
}

export function createEscalationStore(): EscalationStore {
  let items: EscalationRequest[] = [];
  const responses: ResponseLog[] = [];
  return {
    seed(next) {
      items = [...next];
    },
    add(item) {
      items.push(item);
    },
    resolve(id, body) {
      responses.push({ id, body });
      const idx = items.findIndex((e) => e.id === id);
      if (idx < 0) return;
      items[idx] = {
        ...items[idx]!,
        state: 'resolved',
        resolution: {
          kind: (body.kind as EscalationResolutionKind) ?? 'approve',
          value: body.value,
          resolved_by: 'gemba/test/e2e',
          resolved_at: new Date().toISOString(),
        },
      };
    },
    // sessionId filter mirrors the gm-native.13 server-side semantics:
    // when the SPA scopes the fetch (?session_id= query param), the
    // canonical filter happens server-side via ListPendingRequests
    // which indexes by session ID (native escalationIndex.bySession).
    // The fake mirrors that by matching on the escalation's session_id
    // field. Specs that seed per-session escalations must set
    // session_id to the owning session's id (gm-e11.8.6).
    list(sessionId) {
      if (sessionId == null) return items.slice();
      return items.filter((e) => e.session_id === sessionId);
    },
    responses() {
      return responses.slice();
    },
    clear() {
      items = [];
      responses.length = 0;
    },
  };
}
