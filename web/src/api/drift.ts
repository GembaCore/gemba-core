// Typed data-access for the /api/drift surface (gm-e12.13).
//
// The Go server stubs /api/drift with a 501 today
// (internal/server/router.go — TODO gm-e2.6). We treat 501 as "the
// endpoint exists in the contract but isn't wired up yet" and return
// an empty snapshot so the SPA's loaded path renders a clean
// "no drift detected" state instead of a hard error banner.
//
// Any other failure (network, 5xx with envelope, malformed body)
// surfaces as a thrown ApiError so the page falls into its
// error-state branch.

import { apiFetch, ApiError } from './client';
import { emptyDriftSnapshot, type DriftSnapshot } from '@/types/drift';

export async function getDriftSnapshot(): Promise<DriftSnapshot> {
  try {
    return await apiFetch<DriftSnapshot>('/drift');
  } catch (e) {
    if (e instanceof ApiError && e.status === 501) {
      // Stub endpoint — graceful degradation per gm-e12.13 scope note.
      return emptyDriftSnapshot();
    }
    throw e;
  }
}
