import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { endSession, listSessions, startSession, type Session } from '../sessions';

const sample: Session = {
  id: 's-1',
  assignment_id: 'gm-foo',
  agent_id: 'gemba/polecats/obsidian',
  status: 'working',
  started_at: '2026-04-24T12:00:00Z',
};

describe('sessions API client', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('listSessions unwraps the {sessions,total} envelope', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ sessions: [sample], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listSessions();
    expect(Array.isArray(items)).toBe(true);
    expect(items).toEqual([sample]);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/sessions');
  });

  it('listSessions propagates the filter as query string', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ sessions: [], total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await listSessions({
      include_terminal: true,
      status: ['working', 'ready'],
      agent_id: 'mike',
    });
    const url = String(fetchSpy.mock.calls[0]?.[0]);
    expect(url).toContain('include_terminal=true');
    expect(url).toContain('status=working%2Cready');
    expect(url).toContain('agent_id=mike');
  });

  it('startSession POSTs with X-GEMBA-Confirm nonce', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sample), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await startSession({ bead_id: 'gm-foo', agent_type: 'claude' }, { nonce: 'fixed-nonce' });
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe('POST');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBe('fixed-nonce');
    expect(JSON.parse(init.body as string)).toEqual({ bead_id: 'gm-foo', agent_type: 'claude' });
  });

  it('endSession DELETEs with the mode query param', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ ...sample, status: 'completed' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await endSession('s-1', 'canceled', { nonce: 'nonce-end' });
    const url = String(fetchSpy.mock.calls[0]?.[0]);
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe('DELETE');
    expect(url).toBe('/api/sessions/s-1?mode=canceled');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBe('nonce-end');
  });

  it('endSession URL-encodes the id', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sample), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await endSession('tmux:gm-1:42', 'canceled', { nonce: 'n' });
    const url = String(fetchSpy.mock.calls[0]?.[0]);
    expect(url).toBe('/api/sessions/tmux%3Agm-1%3A42?mode=canceled');
  });

  it('endSession rejects empty id', async () => {
    await expect(endSession('', 'canceled')).rejects.toThrow(/required/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
