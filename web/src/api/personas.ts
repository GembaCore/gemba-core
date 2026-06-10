// Typed data-access for /api/v1/personas (gm-8qr / gm-e11.8.7).
//
// The server's read shape is documented at internal/server/personas.go;
// the wire projection covers only the operator-facing axes (id, name,
// role, variety, scope) plus the optional gm-9rv blocks. The Hand-off
// modal in EscalationsPage consumes this list to populate its persona
// dropdown — the previous DEFAULT_PERSONAS constant in PmPanelContext
// stays in place for the legacy PM panel until that surface migrates.
//
// EscalationKind / consult shapes are hand-written elsewhere in the
// codebase; we follow the same pattern here rather than waiting on the
// /api/v1/personas response to land in the OpenAPI codegen surface.

import { apiFetch } from './client';
import type { RepositoryID } from '@/types/core.gen';

export type PersonaVariety = 'coach' | 'manager';
export type PersonaScopeKind = 'project' | 'repository' | 'any';

export interface PersonaScopeView {
  kind: PersonaScopeKind;
  repository?: RepositoryID;
}

export interface PersonaPersonality {
  id?: string;
  description?: string;
  examples?: string[];
}

export interface PersonaPerspective {
  statement?: string;
  triggers?: string[];
  volunteer_mode?: string;
  cost_tier?: string;
}

export interface PersonaPurview {
  domain?: string;
  active_phases?: string[];
  blocking_authority?: string;
}

// PersonaSummary mirrors internal/server/personas.go personaSummary.
// Optional fields stay optional on the wire; absent gm-9rv blocks
// are dropped (omitempty) and surface here as `undefined`.
export interface PersonaSummary {
  id: string;
  name: string;
  role: string;
  variety: PersonaVariety;
  scope: PersonaScopeView;
  description?: string;
  icon?: string;
  skills?: string[];
  personality?: PersonaPersonality;
  perspective?: PersonaPerspective;
  purview?: PersonaPurview;
}

export interface ListPersonasEnvelope {
  personas: PersonaSummary[];
  total: number;
}

// listPersonas returns the workspace's registered personas. The server
// returns 503 when AttachPersonaDispatcher hasn't bound a registry yet
// (pre-config-load Router) — callers see that as an ApiError and the
// Hand-off modal renders a "no personas registered" empty state.
export async function listPersonas(): Promise<PersonaSummary[]> {
  const env = await apiFetch<ListPersonasEnvelope>('/v1/personas');
  return env.personas ?? [];
}
