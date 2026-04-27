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
