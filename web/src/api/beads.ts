// Typed data-access functions for the /api/beads surface (gm-xgm / M1.6).
//
// These are thin wrappers over apiFetch — React Query hooks in
// src/hooks/useBeads.ts own caching, invalidation, and retry. Keep this
// module side-effect-free so it composes cleanly with any query client
// (including tests that mount the hooks with a fresh QueryClient per run).

import { apiFetch } from './client';
import type { WorkItem } from '@/types/core.gen';

// ListBeadsEnvelope is the wire shape the gm-peg list handler emits.
// The server normalises nil slices so `items` is always a JSON array,
// never null. `total` is the pre-pagination count of items (M1.3 has
// no pagination so it equals items.length, but callers MUST NOT assume
// that once filtering / pagination lands).
export interface ListBeadsEnvelope {
  items: WorkItem[];
  total: number;
}

// listBeads — GET /api/beads. The handler returns the {items,total}
// envelope (gm-peg); this helper unwraps to a bare array so callers
// can treat listBeads like a query. Use listBeadsEnvelope() below when
// the caller also needs `total`.
export async function listBeads(): Promise<WorkItem[]> {
  const env = await apiFetch<ListBeadsEnvelope>('/beads');
  return env.items ?? [];
}

// listBeadsEnvelope — same fetch, but surfaces the full envelope for
// callers that need `total` (pagination counts, empty-state copy).
export async function listBeadsEnvelope(): Promise<ListBeadsEnvelope> {
  const env = await apiFetch<ListBeadsEnvelope>('/beads');
  return { items: env.items ?? [], total: env.total ?? 0 };
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
