import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useBead, useBeads } from '../useBeads';
import type { WorkItem } from '@/types/core.gen';

const sampleItem: WorkItem = {
  id: 'gm-foo',
  kind: 'task',
  title: 'Sample',
  status: 'open',
  state_category: 'unstarted',
  created_at: '2026-04-22T00:00:00Z',
  updated_at: '2026-04-22T00:00:00Z',
};

function wrapper() {
  // retry:false so failed fetches surface immediately instead of
  // stalling the test runner. Each test owns its own client so state
  // never leaks between cases.
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('useBeads', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // Mock emits the real server wire shape — the {items,total} envelope
  // (gm-peg) — so useBeads exercises the listBeads unwrap path (gm-root.1.8).
  // A bare-array mock would hide the envelope regression.
  it('returns data on success', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [sampleItem], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useBeads(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([sampleItem]);
  });

  it('surfaces ApiError on 503 adaptor_degraded', async () => {
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ error: 'adaptor_degraded', message: 'reconnecting' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useBeads(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.code).toBe('adaptor_degraded');
    expect(result.current.error?.isAdaptorDegraded).toBe(true);
    // adaptor_degraded MUST NOT spam retries.
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});

describe('useBead', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('fetches a single bead by id', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sampleItem), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useBead('gm-foo'), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(sampleItem);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/beads/gm-foo');
  });

  it('is disabled when id is empty', async () => {
    const { result } = renderHook(() => useBead(undefined), { wrapper: wrapper() });
    // Disabled queries stay in pending+fetchStatus=idle; they never fire.
    expect(result.current.fetchStatus).toBe('idle');
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('surfaces 404 as ApiError with isNotFound=true and does not retry', async () => {
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ error: 'session_not_found', message: 'bead gm-x not found' }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useBead('gm-x'), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.isNotFound).toBe(true);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});
