// Typed data-access for /api/sessions (gm-native.15). Mirrors
// internal/server/sessions.go's wire shape. Session is hand-written
// (rather than codegen) because the full struct isn't part of the
// shared core-types codegen yet — only SessionStatus is. When codegen
// grows Session, replace the local interface with the generated import.

import { apiFetch, ApiError } from './client';
import { CONFIRM_HEADER } from './workItems';
import type { AgentID, SessionStatus } from '@/types/core.gen';

export type SessionEndMode = 'completed' | 'failed' | 'canceled';

export type SessionCloseReason =
  | 'provider_exit'
  | 'transport_error'
  | 'user_stop'
  | 'idle_timeout'
  | 'fatal_stderr'
  | 'protocol_error'
  | 'budget_stop'
  | 'escalation_pause';

export interface Session {
  id: string;
  assignment_id: string;
  agent_id: AgentID;
  status: SessionStatus;
  active_turn_id?: string;
  started_at: string;
  ended_at?: string | null;
  last_heartbeat?: string | null;
  transcript_ref?: string;
  close_reason?: SessionCloseReason | null;
  provider_metadata?: Record<string, unknown>;
}

export interface SessionListEnvelope {
  sessions: Session[];
  total: number;
}

export interface SessionListFilter {
  include_terminal?: boolean;
  status?: SessionStatus[];
  agent_id?: string;
}

function buildListQuery(f?: SessionListFilter): string {
  if (!f) return '';
  const p = new URLSearchParams();
  if (f.include_terminal) p.set('include_terminal', 'true');
  if (f.agent_id) p.set('agent_id', f.agent_id);
  if (f.status && f.status.length > 0) p.set('status', f.status.join(','));
  const qs = p.toString();
  return qs ? `?${qs}` : '';
}

export async function listSessions(filter?: SessionListFilter): Promise<Session[]> {
  const env = await apiFetch<SessionListEnvelope>(`/sessions${buildListQuery(filter)}`);
  return env.sessions ?? [];
}

export interface StartSessionRequest {
  bead_id: string;
  agent_type: string;
  title?: string;
  workspace?: string;
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export async function startSession(
  body: StartSessionRequest,
  opts: { nonce?: string } = {}
): Promise<Session> {
  return apiFetch<Session>('/sessions', {
    method: 'POST',
    body: JSON.stringify(body),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

// SessionPeek mirrors core.SessionPeek (gm-e7.3). The wire shape is
// the snapshot the OrchestrationPlane returns: id + live status + the
// captured-at wall-clock + whichever of transcript/screenshot/structured
// the adaptor's peek_modes declared. Callers tolerate any subset of
// the optional fields.
export interface SessionPeek {
  session_id: string;
  status: SessionStatus;
  captured_at: string;
  transcript?: string;
  screenshot?: string;
  structured?: Record<string, unknown>;
}

export async function peekSession(id: string): Promise<SessionPeek> {
  if (!id) throw new Error('peekSession: id is required');
  return apiFetch<SessionPeek>(`/sessions/${encodeURIComponent(id)}/peek`);
}

export async function endSession(
  id: string,
  mode: SessionEndMode = 'canceled',
  opts: { nonce?: string } = {}
): Promise<Session> {
  if (!id) throw new Error('endSession: id is required');
  const url = `/sessions/${encodeURIComponent(id)}?mode=${encodeURIComponent(mode)}`;
  return apiFetch<Session>(url, {
    method: 'DELETE',
    headers: {
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

// Re-export ApiError so consumers don't need a second import.
export { ApiError };
