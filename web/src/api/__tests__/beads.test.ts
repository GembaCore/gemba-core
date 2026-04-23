import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getBead, listBeads } from '../beads';
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

  it('listBeads hits GET /api/beads', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify([sampleItem]), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const items = await listBeads();
    expect(items).toEqual([sampleItem]);
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/beads');
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
