import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getBead, listBeads, listBeadsEnvelope } from '../beads';
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

describe('listBeads / getBead', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  // Pins the envelope unwrap (gm-root.1.8). The server emits
  // {items, total}; callers of listBeads expect a bare WorkItem[]
  // so BoardPage can iterate it directly. Before the fix, this test
  // exposed the "TypeError: i is not iterable" crash because the
  // bare array at `data` was actually the envelope object.
  it('listBeads unwraps the {items,total} envelope to a bare array', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [sampleItem], total: 1 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listBeads();
    expect(Array.isArray(items)).toBe(true);
    expect(items).toEqual([sampleItem]);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/beads');
  });

  it('listBeads tolerates an empty envelope (items missing)', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ total: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listBeads();
    expect(items).toEqual([]);
  });

  it('listBeadsEnvelope returns items and total for count-aware callers', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [sampleItem, sampleItem], total: 2 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const env = await listBeadsEnvelope();
    expect(env.total).toBe(2);
    expect(env.items).toHaveLength(2);
  });

  it('getBead URL-encodes the id', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sampleItem), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    await getBead('gm/weird id');
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/beads/gm%2Fweird%20id');
  });

  it('getBead rejects empty id without hitting the network', async () => {
    await expect(getBead('')).rejects.toThrow(/required/);
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
