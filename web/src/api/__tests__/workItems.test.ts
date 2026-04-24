import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getWorkItem, listWorkItems, listWorkItemsEnvelope } from '../workItems';
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

describe('listWorkItems / getWorkItem', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // Pins the envelope unwrap (gm-root.1.8). The server emits
  // {items, total}; callers of listWorkItems expect a bare WorkItem[]
  // so BoardPage can iterate it directly. Before the fix, this test
  // exposed the "TypeError: i is not iterable" crash because the
  // bare array at `data` was actually the envelope object.
  it('listWorkItems unwraps the {items,total} envelope to a bare array', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [sampleItem], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listWorkItems();
    expect(Array.isArray(items)).toBe(true);
    expect(items).toEqual([sampleItem]);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/work-items');
  });

  it('listWorkItems tolerates an empty envelope (items missing)', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listWorkItems();
    expect(items).toEqual([]);
  });

  it('listWorkItemsEnvelope returns items and total for count-aware callers', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [sampleItem, sampleItem], total: 2 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const env = await listWorkItemsEnvelope();
    expect(env.total).toBe(2);
    expect(env.items).toHaveLength(2);
  });

  it('getWorkItem URL-encodes the id', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sampleItem), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await getWorkItem('gm/weird id');
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/work-items/gm%2Fweird%20id');
  });

  it('getWorkItem rejects empty id without hitting the network', async () => {
    await expect(getWorkItem('')).rejects.toThrow(/required/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
