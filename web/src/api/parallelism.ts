// Per-session parallelism state (gm-root.16.2 / Workstream B).
//
// Backend contract (gm-root.16.1, internal/adapter/native/parallel.go):
//   GembaEvent.kind = "session_parallel_changed"
//   payload = { session_id, agent_type, in_flight, max_parallel, delta }
// Canonical fixture: internal/adapter/native/testdata/session_parallel_changed.json
// (a pinned-shape handshake test in parallel_test.go enforces fixture-vs-emitter parity).
//
// `intra_parallel` is NOT carried on the wire — derived in the SPA as
// `max_parallel > 1` since the only meaningful pill is one a multi-cap
// session would render. Single-cap sessions render no pill regardless
// of whether they fired an event.
//
// State lives in a tiny module-level store driven by useSyncExternalStore,
// not react-query. Why: the cache is purely SPA-internal (the backend
// has no list endpoint to invalidate), and the alternative — a
// queryFn-less useQuery — both warns at runtime and forces every test
// to wrap in QueryClientProvider.

export interface SessionParallelismState {
  // session_id (Session.id) the state describes.
  session_id: string;
  // agent_type from the registry (e.g. "claude").
  agent_type: string;
  // Number of beads currently in-flight on this session. 0 when idle.
  in_flight: number;
  // Hard cap declared on the agent type. Always >= 1.
  max_parallel: number;
}

// SessionParallelismChangedPayload mirrors the wire shape from
// internal/adapter/native/parallel.go's parallelChanged().
export interface SessionParallelismChangedPayload {
  session_id: string;
  agent_type: string;
  in_flight: number;
  max_parallel: number;
  delta: number;
}

// SESSION_PARALLEL_CHANGED is the canonical event Kind name. Matches
// EventKindSessionParallelChanged in internal/adapter/native/parallel.go.
export const SESSION_PARALLEL_CHANGED = 'session_parallel_changed';

// isIntraParallel returns true when the session's cap admits more
// than one in-flight bead. Used everywhere we'd otherwise check
// an explicit intra_parallel flag.
export function isIntraParallel(s: SessionParallelismState): boolean {
  return s.max_parallel > 1;
}

// SessionParallelismMap is the cached shape — keyed by session_id.
export type SessionParallelismMap = Record<string, SessionParallelismState>;

// applyParallelismEvent folds one event payload into the existing
// map and returns the next map. Pure — the store calls this; tests
// can also exercise the fold directly. Drops events whose session_id
// is missing (defensive against a malformed payload). `delta` is the
// signed transition the backend reports for audit purposes; the SPA
// only needs the absolute counts so we don't carry it forward.
export function applyParallelismEvent(
  prev: SessionParallelismMap,
  payload: SessionParallelismChangedPayload
): SessionParallelismMap {
  if (!payload.session_id) return prev;
  return {
    ...prev,
    [payload.session_id]: {
      session_id: payload.session_id,
      agent_type: payload.agent_type,
      in_flight: payload.in_flight,
      max_parallel: payload.max_parallel,
    },
  };
}

// totalInFlight sums in_flight across the map. Used by the global
// counter. Excludes sessions whose effective cap is 1 because those
// don't participate in the parallelism axis the counter is reporting
// on (a 1/1 session is just "running", not "running parallel work").
export function totalInFlight(map: SessionParallelismMap): number {
  let n = 0;
  for (const k of Object.keys(map)) {
    const s = map[k];
    if (!isIntraParallel(s)) continue;
    n += s.in_flight;
  }
  return n;
}

// --- module-level store ---------------------------------------------------
// Single shared map for the whole SPA. The SSE consumer writes via
// applyEvent; components subscribe via subscribe(); useSyncExternalStore
// turns this into reactive reads.

let state: SessionParallelismMap = {};
const listeners = new Set<() => void>();

export function getParallelismMap(): SessionParallelismMap {
  return state;
}

export function applyEvent(payload: SessionParallelismChangedPayload): void {
  const next = applyParallelismEvent(state, payload);
  if (next === state) return;
  state = next;
  for (const l of listeners) l();
}

export function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// resetParallelism is a test seam — replaces state without notifying
// subscribers. Use it in beforeEach / afterEach to seed or wipe state
// without triggering act() warnings from a still-mounted component
// re-rendering during teardown. Tests that need mid-test reactivity
// should call applyEvent instead.
export function resetParallelism(seed: SessionParallelismMap = {}): void {
  state = { ...seed };
}
