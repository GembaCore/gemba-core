// Typed client for /api/pool-config + /api/personas + /api/orchestration/*
// (gm-s47n.16). Mirrors internal/server/pool_config.go's wire shape.

import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';

export interface PoolEntryRow {
  size: number;
  agent_type?: string;
  floor: number;
  recycle_after_beads: number;
  idle_ceiling_minutes: number;
  min_interval_per_session_seconds: number;
  max_concurrent: number;
}

export interface PoolConfigJSON {
  default_size: number;
  default_persona?: string;
  default_floor: number;
  reserved_for_manual: number;
  routing?: Record<string, string>;
  pools?: Record<string, Record<string, PoolEntryRow>>;
}

export interface PoolConfigEnvelope {
  path: string;
  body: string;
  parsed: PoolConfigJSON;
  max_parallel: number;
  reserved_for_manual: number;
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export async function getPoolConfig(): Promise<PoolConfigEnvelope> {
  return apiFetch<PoolConfigEnvelope>('/pool-config');
}

export async function putPoolConfig(
  cfg: PoolConfigJSON,
  opts: { nonce?: string } = {}
): Promise<PoolConfigEnvelope> {
  return apiFetch<PoolConfigEnvelope>('/pool-config', {
    method: 'PUT',
    body: JSON.stringify(cfg),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

// ---- /api/personas (filesystem enumeration) ----

export interface PersonaFile {
  id: string;
  name: string;
  agent_type: string;
  skills?: string[];
}

export interface PersonaFileListEnvelope {
  personas: PersonaFile[];
  dir: string;
  missing?: boolean;
}

export async function listPersonaFiles(): Promise<PersonaFile[]> {
  const env = await apiFetch<PersonaFileListEnvelope>('/personas');
  return env.personas ?? [];
}

// ---- /api/orchestration/* ----

export type ScopeKind = 'rig' | 'local';

export interface OrchestrationScope {
  id: string;
  kind: ScopeKind;
  personas: string[];
}

export interface OrchestrationPolecat {
  scope: string;
  persona: string;
  name: string;
  state?: string;
}

export interface OrchestrationState {
  adaptor_id: string;
  scopes: OrchestrationScope[];
  agent_types?: string[];
  polecats?: OrchestrationPolecat[];
}

export async function getOrchestrationState(): Promise<OrchestrationState> {
  return apiFetch<OrchestrationState>('/orchestration/state');
}

export interface RunResult {
  command: string;
  stdout: string;
  stderr: string;
  ok: boolean;
  error?: string;
}

export async function createScope(
  name: string,
  opts: { nonce?: string } = {}
): Promise<RunResult> {
  return apiFetch<RunResult>('/orchestration/scopes', {
    method: 'POST',
    body: JSON.stringify({ name }),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

export async function createPolecat(
  rig: string,
  persona: string,
  opts: { nonce?: string } = {}
): Promise<RunResult> {
  return apiFetch<RunResult>('/orchestration/polecats', {
    method: 'POST',
    body: JSON.stringify({ rig, persona }),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

export interface SchedulerConfig {
  max_polecats: number;
}

export async function getSchedulerConfig(): Promise<SchedulerConfig> {
  return apiFetch<SchedulerConfig>('/orchestration/scheduler-config');
}

export async function putSchedulerConfig(
  cfg: SchedulerConfig,
  opts: { nonce?: string } = {}
): Promise<RunResult> {
  return apiFetch<RunResult>('/orchestration/scheduler-config', {
    method: 'PUT',
    body: JSON.stringify(cfg),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}
