// Per-session parallelism state (gm-root.16.2 / Workstream B).
//
// The backend (gm-root.16.1) emits `session.parallel.changed` events
// carrying the in-flight count for one session at a time. The SPA
// keeps a session_id → SessionParallelismState map updated off those
// events; pills + the global counter both read from that map.
//
// State lives in a tiny module-level store driven by useSyncExternalStore,
// not react-query. Why: the cache is purely SPA-internal (the backend
// has no list endpoint to invalidate), and the alternative — a
// queryFn-less useQuery — both warns at runtime and forces every test
// to wrap in QueryClientProvider. The store keeps the hook leaf-level
// and friction-free for callers like AgentContextCard whose tests
// don't touch react-query today.

export interface SessionParallelismState {
  // session_id (Session.id) the state describes.
  session_id: string;
  // agent_id of the session's agent type (e.g. "claude").
  agent_id: string;
  // Number of beads currently in-flight on this session. 0 when idle.
  in_flight: number;
  // Hard cap declared on the agent type. 0 when intra_parallel=false.
  max_parallel: number;
  // Whether the agent type supports multiple concurrent beads in one
  // session. False → pill is suppressed in the UI.
  intra_parallel: boolean;
}

// SessionParallelismChangedPayload is the shape carried in
// GembaEvent.payload for `session.parallel.changed`. The session_id
// at the GembaEvent envelope level is authoritative; payload echoes
// it for self-contained fixtures.
export interface SessionParallelismChangedPayload {
  session_id: string;
  agent_id: string;
  in_flight: number;
  max_parallel: number;
  intra_parallel: boolean;
}

// EVENT_KIND is the canonical event Kind name. Matches WS-A's
// emission. Exported so the SSE consumer and any test fixture
// reference one constant.
export const SESSION_PARALLEL_CHANGED = 'session.parallel.changed';

// SessionParallelismMap is the cached shape — keyed by session_id.
export type SessionParallelismMap = Record<string, SessionParallelismState>;

// applyParallelismEvent folds one event payload into the existing
// map and returns the next map. Pure — the store calls this; tests
// can also exercise the fold directly. Drops events whose session_id
// is missing (defensive against a malformed payload).
export function applyParallelismEvent(
  prev: SessionParallelismMap,
  payload: SessionParallelismChangedPayload
): SessionParallelismMap {
  if (!payload.session_id) return prev;
  return { ...prev, [payload.session_id]: { ...payload } };
}

// totalInFlight sums in_flight across the map. Used by the global
// counter. Excludes sessions where intra_parallel=false because
// those don't participate in the parallelism axis the counter is
// reporting on.
export function totalInFlight(map: SessionParallelismMap): number {
  let n = 0;
  for (const k of Object.keys(map)) {
    const s = map[k];
    if (!s.intra_parallel) continue;
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
