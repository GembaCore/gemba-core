// Typed data-access for the /api/consults + /api/skills surfaces
// (gm-twp2). Empty-list semantics mirror /api/agents: the server
// returns 200 + {consults: []} when no dispatcher is attached, so
// callers treat empty as "no consults yet" rather than an error.
//
// The wire shape carries skill-specific JSON in the `validated_lines`
// slot — `unknown[]` here, the consumer caller type-narrows when it
// needs a concrete shape (e.g. epic_order's RecommendationLine).

import { apiFetch } from './client';
import type { RepositoryID } from '@/types/core.gen';

export type ConsultStatus = 'running' | 'completed' | 'failed';

export type ConsultSource = 'live' | 'audit';

export interface ConsultSummary {
  id: string;
  persona_id: string;
  skill_id: string;
  workspace: string;
  working_dir?: string;
  repository_id?: RepositoryID;
  status: ConsultStatus;
  started_at: string;
  ended_at?: string;
  line_count: number;
  line_error_count: number;
  model?: string;
  latency_ms?: number;
  dollars?: number;
  error?: string;
}

export interface ConsultLineError {
  index: number;
  raw: unknown;
  reason: string;
}

export interface ConsultComposed {
  System?: string;
  User?: string;
  Diagnostics?: unknown;
  TotalTokens?: number;
}

export interface ConsultTokens {
  input: number;
  output: number;
  total: number;
}

export interface ConsultDetail extends ConsultSummary {
  source: ConsultSource;
  composed: ConsultComposed;
  composed_persisted: boolean;
  raw_request?: unknown;
  validated_lines: unknown[];
  line_errors?: ConsultLineError[];
  applied_idx?: number[];
  tokens: ConsultTokens;
}

export interface ListConsultsEnvelope {
  consults: ConsultSummary[];
  total: number;
}

export async function listConsults(): Promise<ConsultSummary[]> {
  const env = await apiFetch<ListConsultsEnvelope>('/consults');
  return env.consults ?? [];
}

export async function getConsult(id: string): Promise<ConsultDetail> {
  return apiFetch<ConsultDetail>('/consults/' + encodeURIComponent(id));
}

export interface SkillSummary {
  id: string;
  name: string;
  description: string;
  output_tool_name?: string;
  has_output_schema: boolean;
}

export interface ListSkillsEnvelope {
  skills: SkillSummary[];
  total: number;
}

export async function listSkills(): Promise<SkillSummary[]> {
  const env = await apiFetch<ListSkillsEnvelope>('/skills');
  return env.skills ?? [];
}

// CreateConsultRequest mirrors the server's createConsultRequest
// shape (internal/server/consults_post.go). raw_input is opaque
// JSON the bound skill validates; the SPA assembles it per-skill.
export interface CreateConsultRequest {
  persona_id: string;
  skill_id: string;
  workspace: string;
  raw_input: unknown;
  guidance?: string;
  template?: {
    workspace_name?: string;
    role?: string;
    project_prefix?: string;
  };
  repository_override?: string;
  project_values?: string[];
  project_guardrails?: string[];
  project_goals?: string[];
  workspace_values?: string[];
  context_chunks?: string[];
  // spawn=false lets the operator iterate on prompt composition
  // without launching a real Claude Code session.
  spawn?: boolean;
}

export async function createConsult(
  body: CreateConsultRequest,
  nonce: string,
): Promise<ConsultSummary> {
  return apiFetch<ConsultSummary>('/consults', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-GEMBA-Confirm': nonce,
    },
    body: JSON.stringify(body),
  });
}

// ApplyConsultResponse mirrors the server's applyResponse shape
// (internal/server/consults_apply.go). `line` is the validated
// entry the operator chose; type-narrow at the call site.
export interface ApplyConsultResponse {
  consult_id: string;
  idx: number;
  line: unknown;
  applied_idx: number[];
}

export async function applyConsult(
  consultID: string,
  idx: number,
  nonce: string,
): Promise<ApplyConsultResponse> {
  return apiFetch<ApplyConsultResponse>(
    `/consults/${encodeURIComponent(consultID)}/apply/${idx}`,
    {
      method: 'POST',
      headers: { 'X-GEMBA-Confirm': nonce },
    },
  );
}

// freshNonce mirrors the helper in api/workItems.ts so callers don't
// have to import it across module boundaries. Per-call UUID is the
// idempotency key the X-GEMBA-Confirm middleware caches against.
export function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
