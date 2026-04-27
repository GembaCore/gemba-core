// Drift snapshot types (gm-e12.13).
//
// Placeholder shapes for the desired-vs-actual reconciliation surface
// served by GET /api/drift. The Go server stubs that endpoint with a
// 501 today (internal/server/router.go — gm-e2.6 follow-up). When
// gm-e2.6 lands, the canonical types will be regenerated into
// core.gen.ts; this file is the SPA's stand-in until then.
//
// The shape mirrors core.OrchestrationPlaneAdaptor.DeclaredState() vs
// ObservedState(): a flat list of fields that differ between the two
// snapshots, plus per-snapshot entity counts so the page can show the
// "desired N / actual M" headline even when the diff is empty.

export interface DriftEntry {
  // Entity kind: "session", "agent", "pack", "rig", etc. Free-form
  // string until the canonical core type pins the universe.
  kind: string;
  // Stable id of the entity that drifted (e.g. session id, agent id).
  id: string;
  // Which field of the entity drifted.
  field: string;
  // The value the workspace's declared topology asks for.
  desired: unknown;
  // The value the adaptor actually observes.
  actual: unknown;
}

export interface DriftSnapshot {
  drift: DriftEntry[];
  desired_count: number;
  actual_count: number;
  // ISO timestamp; the server's CapturedAt for the snapshot pair.
  fetched_at: string;
}

// emptyDriftSnapshot is the value the SPA falls back to when the
// /api/drift endpoint returns 501 (stub today). The page renders a
// "no drift detected" empty state in that case so operators see a
// stable surface instead of an error banner.
export function emptyDriftSnapshot(): DriftSnapshot {
  return {
    drift: [],
    desired_count: 0,
    actual_count: 0,
    fetched_at: '',
  };
}
