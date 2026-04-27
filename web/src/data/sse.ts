// SPA SSE consumer for /events (gm-e12.2). Maps GembaEvent kinds to
// react-query invalidations so the SPA reflects adaptor mutations
// without per-hook polling. EventSource auto-reconnects on connection
// drop (browser default), so disconnect handling is free.
//
// Mapping rationale: we keep the kind→queryKey table short and
// targeted. Invalidating too broadly (e.g. every event invalidates
// ['beads']) wastes network and re-renders; invalidating too narrowly
// risks missing cross-cutting refresh (a session.transition that
// closes a bead also moves the WorkItem). The current table is tuned
// to the gm-native.15/.16 surfaces that just shipped — extend it as
// new event Kinds land.

import type { QueryClient } from '@tanstack/react-query';
import {
  applyEvent as applyParallelismToStore,
  SESSION_PARALLEL_CHANGED,
  type SessionParallelismChangedPayload,
} from '@/api/parallelism';

// GembaEvent mirrors internal/events/schema.go. Kept in this module
// rather than @/types so the SSE consumer is self-contained; when
// codegen grows the event types swap this for the generated import.
export interface GembaEvent {
  id: string;
  kind: string;
  at: string;
  source: { plane: string; adaptor_id: string };
  assignment_id?: string;
  session_id?: string;
  work_item_id?: string;
  agent_id?: string;
  epic_id?: string;
  sprint_id?: string;
  trace_id?: string;
  payload?: Record<string, unknown>;
}

// EventSourceLike is the subset of the EventSource API the consumer
// uses. Tests inject a mock; production passes the browser global.
export interface EventSourceLike {
  addEventListener: (kind: string, listener: (ev: MessageEvent) => void) => void;
  close: () => void;
}

// EventSourceFactory constructs an EventSource for a given URL. The
// factory shape lets tests stub the constructor without monkey-patching
// the global. Default uses the browser's built-in EventSource.
export type EventSourceFactory = (url: string) => EventSourceLike;

const defaultFactory: EventSourceFactory = (url) => new EventSource(url);

// Mapping from event Kind prefix → react-query keys to invalidate.
// Each entry's keys array runs through queryClient.invalidateQueries.
// Ordered by specificity: more-specific Kinds checked first so a
// future 'workitem.created.batch' wouldn't accidentally match the
// generic 'workitem.*' rule.
const KIND_ROUTES: Array<{ matches: (kind: string) => boolean; keys: readonly unknown[][] }> = [
  {
    matches: (k) => k === 'session.transition' || k === 'session.state_reported',
    keys: [['sessions']],
  },
  {
    matches: (k) => k === 'escalation.opened' || k === 'escalation.resolved',
    keys: [['escalations']],
  },
  {
    matches: (k) => k.startsWith('workitem.'),
    keys: [['beads']],
  },
  {
    matches: (k) => k.startsWith('reservation.') || k.startsWith('workspace.'),
    // Workspace acquisition affects the session view (worktree column)
    // and reservations show up in the assignment lifecycle.
    keys: [['sessions']],
  },
];

// startSSE opens the /events stream and routes incoming events through
// queryClient.invalidateQueries. Returns a cleanup that closes the
// connection. Idempotent — safe to call multiple times; each call
// opens a separate connection.
export interface StartSSEOptions {
  /** Override the URL. Defaults to '/events'. */
  url?: string;
  /** Override the EventSource constructor. Tests inject a mock. */
  factory?: EventSourceFactory;
}

export function startSSE(qc: QueryClient, opts: StartSSEOptions = {}): () => void {
  const url = opts.url ?? '/events';
  const factory = opts.factory ?? defaultFactory;
  const es = factory(url);

  // Listen for every Kind we care about. The /events handler stamps
  // the canonical Kind on the SSE `event:` field so we can register
  // typed listeners instead of parsing the body to dispatch.
  // We also register a generic 'message' listener as a fallback for
  // events whose Kind we don't yet have a typed listener for — this
  // supports the gm-hjx additive-extensible contract: future Kinds
  // arrive as default-typed messages and we route on the parsed Kind.
  const handle = (ev: MessageEvent) => {
    let parsed: GembaEvent;
    try {
      parsed = JSON.parse(ev.data) as GembaEvent;
    } catch {
      return;
    }
    routeEvent(qc, parsed);
  };

  // Register one listener per known Kind that the server stamps as
  // the SSE event-name. The browser's EventSource only fires the
  // typed listener for matching event names; the catch-all 'message'
  // handler fires only for default-typed frames (no event: line).
  // Since our server always stamps an event:, message catches future
  // Kinds we don't yet listen for.
  const knownKinds = [
    'session.transition',
    'session.state_reported',
    SESSION_PARALLEL_CHANGED,
    'escalation.opened',
    'escalation.resolved',
    'workitem.created',
    'workitem.updated',
    'workitem.closed',
    'workitem.evidence_attached',
    'reservation.claimed',
    'reservation.released',
    'workspace.acquired',
    'workspace.released',
  ];
  for (const k of knownKinds) {
    es.addEventListener(k, handle);
  }
  es.addEventListener('message', handle);

  return () => es.close();
}

// routeEvent dispatches a parsed GembaEvent to react-query
// invalidations. Exported for direct unit-test access.
export function routeEvent(qc: QueryClient, ev: GembaEvent): void {
  // session.parallel.changed is special: rather than invalidate a
  // backend-served query (no list endpoint exists for the per-session
  // parallelism map), fold the payload into a SPA-internal store
  // (api/parallelism) that pills + the global counter subscribe to.
  // gm-root.16.2.
  if (ev.kind === SESSION_PARALLEL_CHANGED) {
    const payload = parsedParallelismPayload(ev);
    if (payload) applyParallelismToStore(payload);
    return;
  }
  for (const route of KIND_ROUTES) {
    if (!route.matches(ev.kind)) continue;
    for (const key of route.keys) {
      qc.invalidateQueries({ queryKey: key });
    }
  }
}

// parsedParallelismPayload validates that an event's payload matches
// the wire shape from parallelChanged() in
// internal/adapter/native/parallel.go before we fold it in. Defensive
// against a malformed event body — better to drop than to render
// "NaN/undefined" in a pill.
function parsedParallelismPayload(
  ev: GembaEvent
): SessionParallelismChangedPayload | null {
  const p = ev.payload;
  if (!p || typeof p !== 'object') return null;
  const session_id =
    typeof p.session_id === 'string' && p.session_id !== ''
      ? p.session_id
      : ev.session_id;
  if (!session_id) return null;
  const agent_type =
    typeof p.agent_type === 'string' ? p.agent_type : (ev.agent_id ?? '');
  const in_flight = typeof p.in_flight === 'number' ? p.in_flight : NaN;
  const max_parallel = typeof p.max_parallel === 'number' ? p.max_parallel : NaN;
  const delta = typeof p.delta === 'number' ? p.delta : 0;
  if (!Number.isFinite(in_flight) || !Number.isFinite(max_parallel)) return null;
  return { session_id, agent_type, in_flight, max_parallel, delta };
}
