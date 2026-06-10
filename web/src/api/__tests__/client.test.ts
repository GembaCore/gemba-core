import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ADAPTOR_OPERATION_FAILED_EVENT,
  ApiError,
  apiFetch,
  API_BASE,
  type AdaptorOperationFailedDetail,
} from '../client';

describe('apiFetch', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('decodes a 2xx JSON body', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const body = await apiFetch<{ ok: boolean }>('/work-items');
    expect(body).toEqual({ ok: true });
    expect(fetchSpy).toHaveBeenCalledWith(
      `${API_BASE}/work-items`,
      expect.objectContaining({
        headers: expect.objectContaining({ Accept: 'application/json' }),
      })
    );
  });

  it('throws ApiError with decoded envelope on 404', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'session_not_found', message: 'bead gm-x not found' }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await expect(apiFetch('/work-items/gm-x')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      code: 'session_not_found',
      message: 'bead gm-x not found',
    });
  });

  it('flags 503 adaptor_degraded', async () => {
    const events: AdaptorOperationFailedDetail[] = [];
    const onFailure = (ev: Event) =>
      events.push((ev as CustomEvent<AdaptorOperationFailedDetail>).detail);
    window.addEventListener(ADAPTOR_OPERATION_FAILED_EVENT, onFailure);
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'adaptor_degraded', message: 'reconnecting' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    try {
      try {
        await apiFetch('/work-items');
        expect.fail('expected ApiError');
      } catch (e) {
        expect(e).toBeInstanceOf(ApiError);
        const err = e as ApiError;
        expect(err.isAdaptorDegraded).toBe(true);
        expect(err.isNotFound).toBe(false);
      }
      expect(events).toMatchObject([
        {
          status: 503,
          code: 'adaptor_degraded',
          message: 'reconnecting',
          url: `${API_BASE}/work-items`,
        },
      ]);
    } finally {
      window.removeEventListener(ADAPTOR_OPERATION_FAILED_EVENT, onFailure);
    }
  });

  it('flags 503 adaptor_not_configured as degraded', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'adaptor_not_configured', message: 'no WorkPlane' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    try {
      await apiFetch('/work-items');
      expect.fail('expected ApiError');
    } catch (e) {
      expect((e as ApiError).isAdaptorDegraded).toBe(true);
    }
  });

  it('synthesizes bad_envelope when the error body is not JSON', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response('<html>bad gateway</html>', {
        status: 502,
        statusText: 'Bad Gateway',
      })
    );
    await expect(apiFetch('/work-items')).rejects.toMatchObject({
      status: 502,
      code: 'bad_envelope',
    });
  });

  it('synthesizes a network error when fetch rejects', async () => {
    fetchSpy.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    await expect(apiFetch('/work-items')).rejects.toMatchObject({
      status: 0,
      code: 'network',
    });
  });

  it('rejects paths that do not start with /', async () => {
    await expect(apiFetch('beads')).rejects.toThrow(/must start with/);
  });
});
