import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  listEscalations,
  respondEscalation,
  type EscalationRequest,
} from '../escalations';

const sample: EscalationRequest = {
  id: 'esc-1',
  source: 'permission_prompt',
  urgency: 'blocking',
  state: 'open',
  title: 'Bash command',
  prompt: 'May I run rm -rf node_modules?',
  assignment_id: 'gm-foo',
  created_at: '2026-04-24T12:00:00Z',
};

describe('escalations API client', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('listEscalations omits the session_id query when undefined', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ escalations: [sample], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listEscalations();
    expect(items).toEqual([sample]);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/escalations');
  });

  it('listEscalations propagates session_id as a URL-encoded query', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ escalations: [], total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await listEscalations('tmux:gm-1:42');
    expect(fetchSpy.mock.calls[0]?.[0]).toBe(
      '/api/escalations?session_id=tmux%3Agm-1%3A42'
    );
  });

  it('respondEscalation POSTs the body with X-GEMBA-Confirm', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ ...sample, state: 'resolved' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await respondEscalation(
      'esc-1',
      { kind: 'approve' },
      { nonce: 'fixed-nonce' }
    );
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ kind: 'approve' });
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBe('fixed-nonce');
  });

  it('respondEscalation forwards a custom value with kind=modify', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ ...sample, state: 'resolved' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await respondEscalation(
      'esc-1',
      { kind: 'modify', value: 'try a smaller batch' },
      { nonce: 'n' }
    );
    const init = fetchSpy.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual({
      kind: 'modify',
      value: 'try a smaller batch',
    });
  });

  it('respondEscalation rejects empty id without hitting the network', async () => {
    await expect(respondEscalation('', { kind: 'approve' })).rejects.toThrow(/required/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('respondEscalation URL-encodes the id', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sample), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await respondEscalation('esc/with/slash', { kind: 'deny' }, { nonce: 'n' });
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/escalations/esc%2Fwith%2Fslash/respond');
  });
});
