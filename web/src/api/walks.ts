// Typed data-access for /api/v1/walks/* (gm-i65 / gm-wgv).
//
// Matches the wire shapes in internal/walk/types.go and the handlers
// in internal/server/walks.go. When core.gen grows the walk types,
// swap the local interfaces for the generated imports.

import { apiFetch } from './client';
import type { AgentID, WorkItemID } from '@/types/core.gen';

export type WalkStatus = 'active' | 'paused' | 'completed' | 'abandoned';

export type AgendaSourceKind =
  | 'user'
  | 'escalation'
  | 'hitl'
  | 'gate_failure'
  | 'bead_filed'
  | 'bead_closed'
  | 'suggested';

export type AgendaItemStatus =
  | 'queued'
  | 'active'
  | 'decided'
  | 'deferred'
  | 'dismissed';

export type DecisionKind =
  | 'ratify'
  | 'modify'
  | 'reject'
  | 'defer'
  | 'handoff';

export type Speaker = 'user' | 'persona' | 'system';

export interface AgendaSource {
  kind: AgendaSourceKind;
  ref?: string;
  worker?: AgentID;
}

export interface Citation {
  kind?: string;
  ref: string;
  snippet?: string;
}

export interface WalkDecision {
  agenda_item_id: string;
  kind: DecisionKind;
  rationale?: string;
  applied_action_ids?: string[];
  citations?: Citation[];
  decided_at: string;
  decided_by: AgentID;
}

export interface AgendaItem {
  id: string;
  topic: string;
  source: AgendaSource;
  priority: number;
  status: AgendaItemStatus;
  added_at: string;
  added_by?: AgentID;
  decision?: WalkDecision | null;
}

export interface WalkTurn {
  id: string;
  at: string;
  speaker: Speaker;
  persona_id?: string;
  user_id?: AgentID;
  content: string;
  citations?: Citation[];
  agenda_item_id?: string;
}

export interface Cost {
  tokens_in: number;
  tokens_out: number;
  dollars: number;
}

export interface Walk {
  id: string;
  workspace: string;
  label?: string;
  initiated_by: AgentID;
  primary_persona: string;
  assisting_personas?: string[];
  started_at: string;
  ended_at?: string | null;
  status: WalkStatus;
  agenda: AgendaItem[];
  transcript?: WalkTurn[];
  decisions?: WalkDecision[];
  beads_touched?: WorkItemID[];
  cost: Cost;
  resume_context?: string;
}

export interface WalkListEnvelope {
  walks: Walk[];
  total: number;
}

// ── start ──────────────────────────────────────────────────────

export interface StartWalkRequest {
  workspace?: string;
  label?: string;
  initiated_by?: string;
  primary_persona?: string;
  assisting_personas?: string[];
}

export async function startWalk(body: StartWalkRequest = {}): Promise<Walk> {
  return apiFetch<Walk>('/v1/walks:start', {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
}

// ── get ────────────────────────────────────────────────────────

export async function getWalk(id: string): Promise<Walk> {
  if (!id) throw new Error('getWalk: id is required');
  return apiFetch<Walk>(`/v1/walks/${encodeURIComponent(id)}`);
}

// ── list recent ────────────────────────────────────────────────

export interface ListRecentWalksParams {
  workspace?: string;
  status?: WalkStatus | WalkStatus[];
}

export async function listRecentWalks(
  params: ListRecentWalksParams = {}
): Promise<Walk[]> {
  const qs = new URLSearchParams();
  if (params.workspace) qs.set('workspace', params.workspace);
  if (params.status) {
    const arr = Array.isArray(params.status) ? params.status : [params.status];
    if (arr.length > 0) qs.set('status', arr.join(','));
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : '';
  const env = await apiFetch<WalkListEnvelope>(`/v1/walks:recent${suffix}`);
  return env.walks ?? [];
}

// ── agenda ─────────────────────────────────────────────────────

export interface AddAgendaItemRequest {
  topic: string;
  source?: AgendaSource;
  priority?: number;
  added_by?: AgentID;
  status?: AgendaItemStatus;
  id?: string;
}

export async function addAgendaItem(
  walkID: string,
  body: AddAgendaItemRequest
): Promise<Walk> {
  if (!walkID) throw new Error('addAgendaItem: walkID is required');
  return apiFetch<Walk>(`/v1/walks/${encodeURIComponent(walkID)}/agenda`, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
}

export async function patchAgendaItemStatus(
  walkID: string,
  itemID: string,
  status: AgendaItemStatus
): Promise<Walk> {
  if (!walkID || !itemID) {
    throw new Error('patchAgendaItemStatus: walkID and itemID are required');
  }
  return apiFetch<Walk>(
    `/v1/walks/${encodeURIComponent(walkID)}/agenda/${encodeURIComponent(itemID)}`,
    {
      method: 'PATCH',
      body: JSON.stringify({ status }),
      headers: { 'Content-Type': 'application/json' },
    }
  );
}

// ── turn ───────────────────────────────────────────────────────

export interface AddTurnRequest {
  content: string;
  user_id?: AgentID;
  agenda_item_id?: string;
  citations?: Citation[];
}

export async function addTurn(
  walkID: string,
  body: AddTurnRequest
): Promise<Walk> {
  if (!walkID) throw new Error('addTurn: walkID is required');
  return apiFetch<Walk>(`/v1/walks/${encodeURIComponent(walkID)}/turn`, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
}

// ── decide ─────────────────────────────────────────────────────

export interface DecideRequest {
  kind: DecisionKind;
  rationale?: string;
  applied_action_ids?: string[];
  citations?: Citation[];
  decided_by?: AgentID;
}

export async function decideAgendaItem(
  walkID: string,
  itemID: string,
  body: DecideRequest
): Promise<Walk> {
  if (!walkID || !itemID) {
    throw new Error('decideAgendaItem: walkID and itemID are required');
  }
  return apiFetch<Walk>(
    `/v1/walks/${encodeURIComponent(walkID)}/decide/${encodeURIComponent(itemID)}`,
    {
      method: 'POST',
      body: JSON.stringify(body),
      headers: { 'Content-Type': 'application/json' },
    }
  );
}

// ── lifecycle ──────────────────────────────────────────────────

export async function pauseWalk(walkID: string): Promise<Walk> {
  if (!walkID) throw new Error('pauseWalk: walkID is required');
  return apiFetch<Walk>(`/v1/walks/${encodeURIComponent(walkID)}/pause`, {
    method: 'POST',
  });
}

export async function resumeWalk(walkID: string): Promise<Walk> {
  if (!walkID) throw new Error('resumeWalk: walkID is required');
  return apiFetch<Walk>(`/v1/walks/${encodeURIComponent(walkID)}/resume`, {
    method: 'POST',
  });
}

export async function endWalk(walkID: string): Promise<Walk> {
  if (!walkID) throw new Error('endWalk: walkID is required');
  return apiFetch<Walk>(`/v1/walks/${encodeURIComponent(walkID)}/end`, {
    method: 'POST',
  });
}

// ── perspectives (gm-bms) ──────────────────────────────────────
//
// Per-agenda-item inline perspective contributions from active
// personas with VolunteerMode == "always". v1 returns canned
// templated stubs; gm-4p6 will swap in real persona-dispatcher
// output without changing the wire shape.

export type PerspectiveKind = 'blocking' | 'amendment' | 'note';

export interface PerspectiveContribution {
  persona_id: string;
  persona_name?: string;
  persona_icon?: string;
  kind: PerspectiveKind;
  content: string;
  at: string;
}

export interface PerspectivesEnvelope {
  walk_id: string;
  agenda_item_id: string;
  perspectives: PerspectiveContribution[];
  total: number;
}

export async function getPerspectives(
  walkID: string,
  agendaItemID: string
): Promise<PerspectivesEnvelope> {
  if (!walkID || !agendaItemID) {
    throw new Error('getPerspectives: walkID and agendaItemID are required');
  }
  return apiFetch<PerspectivesEnvelope>(
    `/v1/walks/${encodeURIComponent(walkID)}/agenda/${encodeURIComponent(agendaItemID)}/perspectives`
  );
}

// ── derived helpers (pure) ─────────────────────────────────────

// isTerminal: walk is in a finite state and refuses further mutation.
// Useful for the read-only view in /walks/:id.
export function isTerminal(walk: Walk): boolean {
  return walk.status === 'completed' || walk.status === 'abandoned';
}

export function activeAgendaItem(walk: Walk): AgendaItem | undefined {
  return walk.agenda.find((i) => i.status === 'active');
}

export function nextQueuedItem(walk: Walk): AgendaItem | undefined {
  return walk.agenda.find((i) => i.status === 'queued');
}

// decisionCounts returns the aggregate the bottom bar renders.
export function decisionCounts(walk: Walk): {
  decided: number;
  deferred: number;
  total: number;
} {
  let decided = 0;
  let deferred = 0;
  for (const item of walk.agenda) {
    if (item.status === 'decided') decided++;
    else if (item.status === 'deferred') deferred++;
  }
  // Total is "decidable" — exclude already-deferred (parked for next walk).
  const total = walk.agenda.filter((i) => i.status !== 'deferred').length;
  return { decided, deferred, total };
}

// walkDuration returns ms between started and ended (or now). Pure.
export function walkDuration(walk: Walk, now: Date = new Date()): number {
  const start = new Date(walk.started_at).getTime();
  const end = walk.ended_at ? new Date(walk.ended_at).getTime() : now.getTime();
  return Math.max(0, end - start);
}
