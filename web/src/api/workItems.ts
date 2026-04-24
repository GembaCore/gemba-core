// Typed data-access functions for the /api/work-items surface (gm-xgm / M1.6).
//
// These are thin wrappers over apiFetch — React Query hooks in
// src/hooks/useWorkItems.ts own caching, invalidation, and retry. Keep this
// module side-effect-free so it composes cleanly with any query client
// (including tests that mount the hooks with a fresh QueryClient per run).

import { apiFetch } from './client';
import type { WorkItem } from '@/types/core.gen';

// ListWorkItemsEnvelope is the wire shape the gm-peg list handler emits.
// The server normalises nil slices so `items` is always a JSON array,
// never null. `total` is the pre-pagination count of items (M1.3 has
// no pagination so it equals items.length, but callers MUST NOT assume
// that once filtering / pagination lands).
export interface ListWorkItemsEnvelope {
  items: WorkItem[];
  total: number;
}

// listWorkItems — GET /api/work-items. The handler returns the {items,total}
// envelope (gm-peg); this helper unwraps to a bare array so callers
// can treat listWorkItems like a query. Use listWorkItemsEnvelope() below when
// the caller also needs `total`.
export async function listWorkItems(): Promise<WorkItem[]> {
  const env = await apiFetch<ListWorkItemsEnvelope>('/work-items');
  return env.items ?? [];
}

// listWorkItemsEnvelope — same fetch, but surfaces the full envelope for
// callers that need `total` (pagination counts, empty-state copy).
export async function listWorkItemsEnvelope(): Promise<ListWorkItemsEnvelope> {
  const env = await apiFetch<ListWorkItemsEnvelope>('/work-items');
  return { items: env.items ?? [], total: env.total ?? 0 };
}

// getWorkItem — GET /api/work-items/{id}. Returns one WorkItem with its full
// Relationship graph plus any adaptor-native edges under
// Custom["beads:dependencies"] (see internal/server/work_items.go, gm-kn2).
// Throws ApiError with status 404 / code "session_not_found" when the
// id is unknown.
export async function getWorkItem(id: string): Promise<WorkItem> {
  if (!id) {
    throw new Error('getWorkItem: id is required');
  }
  return apiFetch<WorkItem>(`/work-items/${encodeURIComponent(id)}`);
}

// WorkItemPatch mirrors the Go shape (internal/core/workplane.go).
// Every field is optional; the wire encoding is `omitempty`-friendly,
// so undefined fields stay out of the JSON body and the adaptor
// treats unset == "no change".
export interface WorkItemPatch {
  title?: string;
  description?: string;
  status?: string;
  state_category?: WorkItem['state_category'];
  priority?: number | null;
  labels?: string[];
  // owner / assignee / dod / sprint_id / custom land in slice 3 — the
  // server already accepts them; the client surface stays narrow until
  // the drawer has UI for each.
}

// Confirm header MUST match internal/server/nonce.go ConfirmHeader.
// Kept on the api/ side so every mutation route uses the same token
// without duplicating the literal across hooks.
export const CONFIRM_HEADER = 'X-GEMBA-Confirm';

// updateWorkItem — PATCH /api/work-items/{id}. Generates a fresh UUID nonce
// per call so a SPA double-click can't double-apply (the server
// caches replays per nonce and returns the cached envelope verbatim).
// Caller can pass an explicit `nonce` for retry semantics — useful
// when a request is in-flight and the network drops; resending with
// the same nonce is idempotent.
export async function updateWorkItem(
  id: string,
  patch: WorkItemPatch,
  opts: { nonce?: string } = {}
): Promise<WorkItem> {
  if (!id) {
    throw new Error('updateWorkItem: id is required');
  }
  return apiFetch<WorkItem>(`/work-items/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

// freshNonce returns a UUID-like opaque token. crypto.randomUUID is
// available in every browser the SPA targets and in jsdom; the
// fallback covers older test environments without crypto wired up.
function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  // Math.random fallback — only the test environment hits this.
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
