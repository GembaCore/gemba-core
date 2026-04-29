// Typed data-access for /api/escalations (gm-native.13 + gm-native.16).
// EscalationRequest is hand-written here because the codegen surface
// only covers WorkItem + SessionStatus today; when codegen grows the
// orchestration types, swap the local interfaces for the generated
// imports.

import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';
import type { AgentID, WorkItemID } from '@/types/core.gen';

// EscalationKind mirrors core.EscalationKind in core/orchestration.go.
// Keep this union in lock-step with the Go const block; the SPA's
// severity-mapping + per-kind icon dispatch reads every member.
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

export type EscalationChannel = 'notification' | 'tool_call';
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
  resolved_by: AgentID;
  resolved_at: string;
}

export interface EscalationRequest {
  id: string;
  source: EscalationKind;
  channel?: EscalationChannel;
  assignment_id?: string;
  work_item_id?: WorkItemID;
  agent_id?: AgentID;
  urgency: EscalationUrgency;
  title: string;
  prompt: string;
  options?: EscalationOption[];
  deadline?: string | null;
  state: EscalationState;
  resolution?: EscalationResolution | null;
  created_at: string;
}

export interface EscalationListEnvelope {
  escalations: EscalationRequest[];
  total: number;
}

// listEscalations — when sessionId is set, the server returns the
// per-session pending list (gm-native.13 ListPendingRequests path);
// when omitted, it returns every open escalation across the bound
// orchestration plane.
export async function listEscalations(sessionId?: string): Promise<EscalationRequest[]> {
  const url = sessionId
    ? `/escalations?session_id=${encodeURIComponent(sessionId)}`
    : '/escalations';
  const env = await apiFetch<EscalationListEnvelope>(url);
  return env.escalations ?? [];
}

export interface RespondEscalationRequest {
  kind: EscalationResolutionKind;
  value?: string;
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export async function respondEscalation(
  id: string,
  body: RespondEscalationRequest,
  opts: { nonce?: string } = {}
): Promise<EscalationRequest> {
  if (!id) throw new Error('respondEscalation: id is required');
  return apiFetch<EscalationRequest>(`/escalations/${encodeURIComponent(id)}/respond`, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}
