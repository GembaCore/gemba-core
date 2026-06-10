// Typed data-access for the /api/sprints surface (gm-root.11).
//
// Empty-list semantics: bd / dolt have sprint_native=false and return
// {sprints: []}. Callers MUST treat empty as "no sprint roster" and
// fall back to the freeform SprintEditor.

import { apiFetch } from './client';
import type { Sprint } from '@/types/core.gen';

export interface ListSprintsEnvelope {
  sprints: Sprint[];
  total: number;
}

export async function listSprints(): Promise<Sprint[]> {
  const env = await apiFetch<ListSprintsEnvelope>('/sprints');
  return env.sprints ?? [];
}
