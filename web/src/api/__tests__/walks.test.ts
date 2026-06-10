import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  addAgendaItem,
  addTurn,
  decideAgendaItem,
  decisionCounts,
  endWalk,
  getWalk,
  isTerminal,
  listRecentWalks,
  patchAgendaItemStatus,
  pauseWalk,
  resumeWalk,
  startWalk,
  walkDuration,
  type Walk,
} from '../walks';

const sampleWalk: Walk = {
  id: 'walk-001',
  workspace: 'ws-x',
  initiated_by: 'operator',
  primary_persona: 'project-manager',
  started_at: '2026-04-27T12:00:00Z',
  status: 'active',
  agenda: [],
  cost: { tokens_in: 0, tokens_out: 0, dollars: 0 },
};

describe('walks API client', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  function jsonResponse(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  it('startWalk POSTs the request body', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(sampleWalk, 201));
    const result = await startWalk({ label: 'morning review' });
    expect(result).toEqual(sampleWalk);
    const url = fetchSpy.mock.calls[0]?.[0] as string;
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(url).toBe('/api/v1/walks:start');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ label: 'morning review' });
  });

  it('startWalk accepts no body', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(sampleWalk, 201));
    await startWalk();
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({});
  });

  it('getWalk URL-encodes the id', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(sampleWalk));
    await getWalk('walk/with/slash');
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/v1/walks/walk%2Fwith%2Fslash');
  });

  it('getWalk rejects empty id without hitting the network', async () => {
    await expect(getWalk('')).rejects.toThrow(/required/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('listRecentWalks omits qs when no params', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse({ walks: [], total: 0 }));
    await listRecentWalks();
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/v1/walks:recent');
  });

  it('listRecentWalks joins multiple statuses', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse({ walks: [], total: 0 }));
    await listRecentWalks({ status: ['active', 'paused'] });
    expect(fetchSpy.mock.calls[0]?.[0]).toBe(
      '/api/v1/walks:recent?status=active%2Cpaused'
    );
  });

  it('listRecentWalks forwards workspace + status together', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse({ walks: [], total: 0 }));
    await listRecentWalks({ workspace: 'ws-x', status: 'completed' });
    const url = fetchSpy.mock.calls[0]?.[0] as string;
    expect(url).toContain('workspace=ws-x');
    expect(url).toContain('status=completed');
  });

  it('addAgendaItem posts topic + source', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(sampleWalk));
    await addAgendaItem('walk-001', {
      topic: 'extra topic',
      source: { kind: 'user' },
    });
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({
      topic: 'extra topic',
      source: { kind: 'user' },
    });
  });

  it('patchAgendaItemStatus uses PATCH + URL-encodes both ids', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(sampleWalk));
    await patchAgendaItemStatus('walk-001', 'item/x', 'active');
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe('PATCH');
    expect(fetchSpy.mock.calls[0]?.[0]).toBe(
      '/api/v1/walks/walk-001/agenda/item%2Fx'
    );
    expect(JSON.parse(init.body as string)).toEqual({ status: 'active' });
  });

  it('addTurn rejects empty walk id without network', async () => {
    await expect(addTurn('', { content: 'hello' })).rejects.toThrow(/required/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('decideAgendaItem POSTs a kind and decided_by', async () => {
    fetchSpy.mockResolvedValueOnce(jsonResponse(sampleWalk));
    await decideAgendaItem('walk-001', 'item-1', {
      kind: 'ratify',
      decided_by: 'operator',
    });
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({
      kind: 'ratify',
      decided_by: 'operator',
    });
  });

  it('pauseWalk + resumeWalk + endWalk hit their lifecycle paths', async () => {
    // Fresh Response per call — Response bodies are single-use streams.
    fetchSpy.mockImplementation(() =>
      Promise.resolve(jsonResponse(sampleWalk))
    );
    await pauseWalk('walk-001');
    await resumeWalk('walk-001');
    await endWalk('walk-001');
    expect(fetchSpy.mock.calls.map((c) => c[0])).toEqual([
      '/api/v1/walks/walk-001/pause',
      '/api/v1/walks/walk-001/resume',
      '/api/v1/walks/walk-001/end',
    ]);
  });
});

describe('walks pure helpers', () => {
  it('isTerminal flips on completed + abandoned', () => {
    expect(isTerminal({ ...sampleWalk, status: 'active' })).toBe(false);
    expect(isTerminal({ ...sampleWalk, status: 'paused' })).toBe(false);
    expect(isTerminal({ ...sampleWalk, status: 'completed' })).toBe(true);
    expect(isTerminal({ ...sampleWalk, status: 'abandoned' })).toBe(true);
  });

  it('decisionCounts excludes deferred from the total', () => {
    const w: Walk = {
      ...sampleWalk,
      agenda: [
        {
          id: 'a',
          topic: 'a',
          source: { kind: 'user' },
          priority: 0,
          status: 'decided',
          added_at: '2026-04-27T12:00:00Z',
        },
        {
          id: 'b',
          topic: 'b',
          source: { kind: 'user' },
          priority: 0,
          status: 'queued',
          added_at: '2026-04-27T12:00:00Z',
        },
        {
          id: 'c',
          topic: 'c',
          source: { kind: 'user' },
          priority: 0,
          status: 'deferred',
          added_at: '2026-04-27T12:00:00Z',
        },
      ],
    };
    expect(decisionCounts(w)).toEqual({ decided: 1, deferred: 1, total: 2 });
  });

  it('walkDuration returns ms between started_at and ended_at when ended', () => {
    const w: Walk = {
      ...sampleWalk,
      started_at: '2026-04-27T12:00:00Z',
      ended_at: '2026-04-27T12:30:00Z',
    };
    expect(walkDuration(w)).toBe(30 * 60 * 1000);
  });

  it('walkDuration falls back to now when not ended', () => {
    const fixedNow = new Date('2026-04-27T12:15:00Z');
    const w: Walk = { ...sampleWalk, started_at: '2026-04-27T12:00:00Z' };
    expect(walkDuration(w, fixedNow)).toBe(15 * 60 * 1000);
  });
});
