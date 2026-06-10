// MilestoneChildrenPanel (gm-98sq) — child-epic list + add affordance
// inside the WorkItemDrawer when the open bead is a milestone.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { MilestoneChildrenPanel } from '../MilestoneChildrenPanel';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';

const COMMON = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function milestone(id: string, title = 'M1 Beta'): WorkItem {
  return { ...COMMON, id, title, kind: KIND_MILESTONE, relationships: [] };
}

function epic(id: string, title: string, parentId?: string): WorkItem {
  return {
    ...COMMON,
    id,
    title,
    kind: 'epic',
    relationships: parentId
      ? [{ kind: 'parent_child', from: parentId, to: id }]
      : [],
  };
}

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function mockJSON(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('MilestoneChildrenPanel (gm-98sq)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('lists child epics under the milestone and offers orphan epics for adding', () => {
    const m = milestone('gm-m1');
    const items: WorkItem[] = [
      m,
      epic('gm-e1', 'Child epic one', 'gm-m1'),
      epic('gm-e2', 'Child epic two', 'gm-m1'),
      epic('gm-e3', 'Orphan epic'),
      // Non-epic items must not appear in either list.
      { ...COMMON, id: 'gm-t1', title: 'A task', kind: 'task', relationships: [] },
    ];

    render(<MilestoneChildrenPanel milestone={m} allItems={items} />, {
      wrapper: wrapper(),
    });

    // Both children appear in the children list.
    expect(screen.getByTestId('milestone-child-gm-e1')).toBeTruthy();
    expect(screen.getByTestId('milestone-child-gm-e2')).toBeTruthy();
    // Orphan epic is the only candidate offered for add.
    expect(screen.getByTestId('milestone-add-gm-e3')).toBeTruthy();
    expect(screen.queryByTestId('milestone-add-gm-e1')).toBeNull();
    // Task does not appear anywhere.
    expect(screen.queryByTestId('milestone-child-gm-t1')).toBeNull();
    expect(screen.queryByTestId('milestone-add-gm-t1')).toBeNull();
  });

  it('clicking Remove fires a PATCH with parent_id="" for that epic', async () => {
    const m = milestone('gm-m1');
    const child = epic('gm-e1', 'Child epic one', 'gm-m1');
    fetchSpy.mockResolvedValue(mockJSON({ ...child, relationships: [] }));

    render(<MilestoneChildrenPanel milestone={m} allItems={[m, child]} />, {
      wrapper: wrapper(),
    });

    fireEvent.click(screen.getByTestId('milestone-child-remove-gm-e1'));

    await waitFor(() =>
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
        ),
      ).toBe(true),
    );
    const patchCall = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
    )!;
    const [url, init] = patchCall;
    expect(url).toBe('/api/work-items/gm-e1');
    const body = JSON.parse(init.body as string);
    expect(body.parent_id).toBe('');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-GEMBA-Confirm']).toBeTruthy();
  });

  it('clicking Add fires a PATCH with parent_id=<milestoneId> for the orphan epic', async () => {
    const m = milestone('gm-m1');
    const orphan = epic('gm-e3', 'Orphan epic');
    fetchSpy.mockResolvedValue(
      mockJSON({
        ...orphan,
        relationships: [{ kind: 'parent_child', from: m.id, to: orphan.id }],
      }),
    );

    render(<MilestoneChildrenPanel milestone={m} allItems={[m, orphan]} />, {
      wrapper: wrapper(),
    });

    fireEvent.click(screen.getByTestId('milestone-add-button-gm-e3'));

    await waitFor(() =>
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
        ),
      ).toBe(true),
    );
    const patchCall = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
    )!;
    const [url, init] = patchCall;
    expect(url).toBe('/api/work-items/gm-e3');
    const body = JSON.parse(init.body as string);
    expect(body.parent_id).toBe('gm-m1');
  });

  it('shows the empty children copy when no epic is parented under the milestone', () => {
    const m = milestone('gm-m1');
    render(<MilestoneChildrenPanel milestone={m} allItems={[m]} />, {
      wrapper: wrapper(),
    });
    expect(screen.getByTestId('milestone-children-empty')).toBeTruthy();
  });

  it('filter input narrows the add-list', () => {
    const m = milestone('gm-m1');
    const items: WorkItem[] = [
      m,
      epic('gm-e3', 'Apple cart'),
      epic('gm-e4', 'Banana split'),
    ];
    render(<MilestoneChildrenPanel milestone={m} allItems={items} />, {
      wrapper: wrapper(),
    });
    expect(screen.getByTestId('milestone-add-gm-e3')).toBeTruthy();
    expect(screen.getByTestId('milestone-add-gm-e4')).toBeTruthy();

    fireEvent.change(screen.getByTestId('milestone-add-filter'), {
      target: { value: 'banana' },
    });
    expect(screen.queryByTestId('milestone-add-gm-e3')).toBeNull();
    expect(screen.getByTestId('milestone-add-gm-e4')).toBeTruthy();
  });
});
