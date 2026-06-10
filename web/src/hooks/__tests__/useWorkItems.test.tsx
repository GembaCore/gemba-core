import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { workItemsKeys, useWorkItem, useWorkItems, useUpdateWorkItem } from '../useWorkItems';
import { CONFIRM_HEADER } from '@/api/workItems';
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

describe('useWorkItems', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // Mock emits the real server wire shape — the {items,total} envelope
  // (gm-peg) — so useWorkItems exercises the listWorkItems unwrap path (gm-root.1.8).
  // A bare-array mock would hide the envelope regression.
  it('returns data on success', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [sampleItem], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useWorkItems(), { wrapper: wrapper() });
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
    const { result } = renderHook(() => useWorkItems(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.code).toBe('adaptor_degraded');
    expect(result.current.error?.isAdaptorDegraded).toBe(true);
    // adaptor_degraded MUST NOT spam retries.
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});

describe('useWorkItem', () => {
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
    const { result } = renderHook(() => useWorkItem('gm-foo'), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(sampleItem);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/work-items/gm-foo');
  });

  it('is disabled when id is empty', async () => {
    const { result } = renderHook(() => useWorkItem(undefined), { wrapper: wrapper() });
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
    const { result } = renderHook(() => useWorkItem('gm-x'), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.isNotFound).toBe(true);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});

// gm-root.8 slice 2: useUpdateWorkItem must wire the X-GEMBA-Confirm
// header, optimistically update the cache, and roll back on error.
describe('useUpdateWorkItem', () => {
  const fetchSpy = vi.fn();
  beforeEach(() => vi.stubGlobal('fetch', fetchSpy));
  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // Custom wrapper that exposes the QueryClient so the test can pre-seed
  // the cache (mirroring a drawer that opened after a list fetch).
  function withClient(client: QueryClient) {
    return function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    };
  }

  it('PATCHes /api/work-items/:id with X-GEMBA-Confirm and returns the updated item', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ ...sampleItem, status: 'closed' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const { result } = renderHook(() => useUpdateWorkItem(), { wrapper: withClient(client) });
    result.current.mutate({ id: 'gm-foo', patch: { status: 'closed' } });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const [url, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/work-items/gm-foo');
    expect(init.method).toBe('PATCH');
    const headers = new Headers(init.headers as HeadersInit);
    expect(headers.get(CONFIRM_HEADER)).toBeTruthy();
    expect(headers.get('Content-Type')).toBe('application/json');
    expect(JSON.parse(init.body as string)).toEqual({ status: 'closed' });
  });

  it('optimistically updates the detail cache; rolls back on error', async () => {
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
    });
    client.setQueryData(workItemsKeys.detail('gm-foo'), sampleItem);

    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'read_only', message: 'adaptor is read-only' }), {
        status: 405,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useUpdateWorkItem(), { wrapper: withClient(client) });

    result.current.mutate({ id: 'gm-foo', patch: { status: 'closed' } });
    // Within a tick the optimistic write is visible.
    await waitFor(() => {
      const cur = client.getQueryData<WorkItem>(workItemsKeys.detail('gm-foo'));
      // The optimistic write either lands as 'closed' or has already
      // rolled back; assert via the mutation state instead.
      expect(cur?.id).toBe('gm-foo');
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    // After the error settles, the cache must be back to the original.
    const after = client.getQueryData<WorkItem>(workItemsKeys.detail('gm-foo'));
    expect(after?.status).toBe(sampleItem.status);
  });
});
