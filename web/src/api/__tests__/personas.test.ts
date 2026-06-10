// Tests for /api/v1/personas typed client (gm-e11.8.7).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listPersonas, type PersonaSummary } from '../personas';

const sample: PersonaSummary = {
  id: 'project-manager',
  name: 'Project Manager',
  role: 'PM',
  variety: 'coach',
  scope: { kind: 'project' },
  description: 'PM persona for tests.',
  skills: ['epic_order', 'escalation_handoff'],
};

describe('personas API client', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('listPersonas hits /api/v1/personas and unwraps the envelope', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ personas: [sample], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listPersonas();
    expect(items).toEqual([sample]);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/v1/personas');
  });

  it('listPersonas tolerates a missing personas key', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listPersonas();
    expect(items).toEqual([]);
  });

  it('listPersonas surfaces a 503 as an ApiError', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ error: 'adaptor_not_configured', message: 'no registry' }),
        { status: 503, headers: { 'Content-Type': 'application/json' } }
      )
    );
    await expect(listPersonas()).rejects.toThrow();
  });
});
