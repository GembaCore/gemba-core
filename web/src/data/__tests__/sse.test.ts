// SSE consumer tests (gm-e12.2). Covers the kind→queryKey routing
// table and the EventSource lifecycle. Uses a hand-rolled mock for
// EventSource because jsdom doesn't ship one and we want full control
// over which event names fire.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { routeEvent, startSSE, type EventSourceLike, type GembaEvent } from '../sse';
import {
  getParallelismMap,
  resetParallelism,
  SESSION_PARALLEL_CHANGED,
} from '@/api/parallelism';

class MockEventSource implements EventSourceLike {
  url: string;
  listeners = new Map<string, ((ev: MessageEvent) => void)[]>();
  closed = false;

  constructor(url: string) {
    this.url = url;
  }
  addEventListener(kind: string, listener: (ev: MessageEvent) => void): void {
    const arr = this.listeners.get(kind) ?? [];
    arr.push(listener);
    this.listeners.set(kind, arr);
  }
  close(): void {
    this.closed = true;
  }
  // Test helper.
  fire(kind: string, payload: GembaEvent) {
    const data = JSON.stringify(payload);
    const listeners = this.listeners.get(kind) ?? [];
    for (const l of listeners) {
      l({ data } as MessageEvent);
    }
  }
}

function makeQC() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

describe('routeEvent', () => {
  let qc: QueryClient;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let invalidateSpy: any;

  beforeEach(() => {
    qc = makeQC();
    invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
  });
  afterEach(() => {
    invalidateSpy.mockRestore();
  });

  const baseEvent = {
    id: 'e',
    at: '2026-04-24T12:00:00Z',
    source: { plane: 'orchestration', adaptor_id: 'native' },
  };

  it('routes session.transition to ["sessions"]', () => {
    routeEvent(qc, { ...baseEvent, kind: 'session.transition' } as GembaEvent);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['sessions'] });
  });

  it('routes session.state_reported to ["sessions"]', () => {
    routeEvent(qc, { ...baseEvent, kind: 'session.state_reported' } as GembaEvent);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['sessions'] });
  });

  it('routes escalation.opened and escalation.resolved to ["escalations"]', () => {
    routeEvent(qc, { ...baseEvent, kind: 'escalation.opened' } as GembaEvent);
    routeEvent(qc, { ...baseEvent, kind: 'escalation.resolved' } as GembaEvent);
    const escalationCalls = invalidateSpy.mock.calls.filter(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (c: any) => Array.isArray(c[0]?.queryKey) && c[0].queryKey[0] === 'escalations'
    );
    expect(escalationCalls.length).toBe(2);
  });

  it('routes workitem.* prefix to ["beads"]', () => {
    routeEvent(qc, { ...baseEvent, kind: 'workitem.updated' } as GembaEvent);
    routeEvent(qc, { ...baseEvent, kind: 'workitem.closed' } as GembaEvent);
    const beadsCalls = invalidateSpy.mock.calls.filter(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (c: any) => Array.isArray(c[0]?.queryKey) && c[0].queryKey[0] === 'beads'
    );
    expect(beadsCalls.length).toBe(2);
  });

  it('routes reservation.* and workspace.* to ["sessions"]', () => {
    routeEvent(qc, { ...baseEvent, kind: 'reservation.claimed' } as GembaEvent);
    routeEvent(qc, { ...baseEvent, kind: 'workspace.acquired' } as GembaEvent);
    const sessionCalls = invalidateSpy.mock.calls.filter(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (c: any) => Array.isArray(c[0]?.queryKey) && c[0].queryKey[0] === 'sessions'
    );
    expect(sessionCalls.length).toBe(2);
  });

  it('ignores unknown kinds (additive-extensible contract)', () => {
    routeEvent(qc, { ...baseEvent, kind: 'epic.future_kind' } as GembaEvent);
    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  describe('session.parallel.changed', () => {
    afterEach(() => {
      resetParallelism();
    });

    it('routes payload into the parallelism store (no invalidate)', () => {
      routeEvent(qc, {
        ...baseEvent,
        kind: SESSION_PARALLEL_CHANGED,
        session_id: 'sess-A',
        payload: {
          session_id: 'sess-A',
          agent_id: 'claude',
          in_flight: 2,
          max_parallel: 3,
          intra_parallel: true,
        },
      } as GembaEvent);
      expect(invalidateSpy).not.toHaveBeenCalled();
      expect(getParallelismMap()['sess-A']).toEqual({
        session_id: 'sess-A',
        agent_id: 'claude',
        in_flight: 2,
        max_parallel: 3,
        intra_parallel: true,
      });
    });

    it('drops a malformed event (missing in_flight / max_parallel)', () => {
      routeEvent(qc, {
        ...baseEvent,
        kind: SESSION_PARALLEL_CHANGED,
        session_id: 'sess-bad',
        payload: { session_id: 'sess-bad', agent_id: 'claude', intra_parallel: true },
      } as GembaEvent);
      expect(getParallelismMap()['sess-bad']).toBeUndefined();
    });

    it('falls back to GembaEvent.session_id when payload omits it', () => {
      routeEvent(qc, {
        ...baseEvent,
        kind: SESSION_PARALLEL_CHANGED,
        session_id: 'sess-B',
        payload: {
          agent_id: 'claude',
          in_flight: 1,
          max_parallel: 2,
          intra_parallel: true,
        },
      } as GembaEvent);
      expect(getParallelismMap()['sess-B']?.in_flight).toBe(1);
    });

    it('subsequent events overwrite earlier per-session state', () => {
      const fire = (in_flight: number) =>
        routeEvent(qc, {
          ...baseEvent,
          kind: SESSION_PARALLEL_CHANGED,
          session_id: 'sess-C',
          payload: {
            session_id: 'sess-C',
            agent_id: 'claude',
            in_flight,
            max_parallel: 3,
            intra_parallel: true,
          },
        } as GembaEvent);
      fire(1);
      fire(2);
      fire(3);
      expect(getParallelismMap()['sess-C']?.in_flight).toBe(3);
    });
  });
});

describe('startSSE', () => {
  it('opens the EventSource at the default /events URL', () => {
    const qc = makeQC();
    let constructed: MockEventSource | null = null;
    const factory = (url: string) => {
      constructed = new MockEventSource(url);
      return constructed;
    };
    startSSE(qc, { factory });
    expect(constructed).not.toBeNull();
    expect((constructed as MockEventSource | null)?.url).toBe('/events');
  });

  it('invalidates ["sessions"] when a session.transition event arrives', () => {
    const qc = makeQC();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    let mock: MockEventSource | null = null;
    const factory = (url: string) => {
      mock = new MockEventSource(url);
      return mock;
    };
    startSSE(qc, { factory });
    (mock as unknown as MockEventSource).fire('session.transition', {
      id: 'e1',
      kind: 'session.transition',
      at: '2026-04-24T12:00:00Z',
      source: { plane: 'orchestration', adaptor_id: 'native' },
    });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['sessions'] });
  });

  it('cleanup closes the underlying EventSource', () => {
    const qc = makeQC();
    let mock: MockEventSource | null = null;
    const factory = (url: string) => {
      mock = new MockEventSource(url);
      return mock;
    };
    const stop = startSSE(qc, { factory });
    expect((mock as unknown as MockEventSource).closed).toBe(false);
    stop();
    expect((mock as unknown as MockEventSource).closed).toBe(true);
  });

  it('routes catch-all message events for unknown kinds', () => {
    // Future Kind arrives via the default 'message' channel because
    // we don't register a typed listener for it. The handler still
    // parses it and the kind→route table ignores unknowns.
    const qc = makeQC();
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');
    let mock: MockEventSource | null = null;
    const factory = (url: string) => {
      mock = new MockEventSource(url);
      return mock;
    };
    startSSE(qc, { factory });
    (mock as unknown as MockEventSource).fire('message', {
      id: 'e2',
      kind: 'epic.ui_state_changed',
      at: '2026-04-24T12:00:00Z',
      source: { plane: 'workplane', adaptor_id: 'bd' },
    });
    expect(invalidateSpy).not.toHaveBeenCalled();
  });
});
