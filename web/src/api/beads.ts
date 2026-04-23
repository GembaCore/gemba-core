// Typed data-access functions for the /api/beads surface (gm-xgm / M1.6).
//
// These are thin wrappers over apiFetch — React Query hooks in
// src/hooks/useBeads.ts own caching, invalidation, and retry. Keep this
// module side-effect-free so it composes cleanly with any query client
// (including tests that mount the hooks with a fresh QueryClient per run).

import { apiFetch } from './client';
import type { WorkItem } from '@/types/core.gen';

// listBeads — GET /api/beads. Returns every WorkItem the bound
// WorkPlane adaptor exposes. The server handler is still a stub
// (router.go `/beads` → notImplemented) until M1.3 ships; callers
// should treat 501 as adaptor_degraded for the banner.
export async function listBeads(): Promise<WorkItem[]> {
  return apiFetch<WorkItem[]>('/beads');
}

// getBead — GET /api/beads/{id}. Returns one WorkItem with its full
// Relationship graph plus any adaptor-native edges under
// Custom["beads:dependencies"] (see internal/server/beads.go, gm-kn2).
// Throws ApiError with status 404 / code "session_not_found" when the
// id is unknown.
export async function getBead(id: string): Promise<WorkItem> {
  if (!id) {
    throw new Error('getBead: id is required');
  }
  return apiFetch<WorkItem>(`/beads/${encodeURIComponent(id)}`);
}
