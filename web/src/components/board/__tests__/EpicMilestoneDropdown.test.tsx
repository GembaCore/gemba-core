// EpicMilestoneDropdown (gm-o2k9) — edit path c. Verifies the dropdown
// reflects the epic's current milestone and emits parent_id mutations
// for set / clear.

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { EpicMilestoneDropdown } from '../EpicMilestoneDropdown';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';

const COMMON = {
  status: 'open',
  state_category: 'unstarted' as const,
  created_at: '',
  updated_at: '',
};

function milestone(id: string, title: string): WorkItem {
  return { ...COMMON, id, title, kind: KIND_MILESTONE, relationships: [] };
}

function epic(id: string, opts: { parent?: string } = {}): WorkItem {
  const relationships = opts.parent
    ? [{ kind: 'parent_child' as const, from: opts.parent, to: id }]
    : [];
  return { ...COMMON, id, title: 'My epic', kind: 'epic', relationships };
}

function mount(epicItem: WorkItem, items: WorkItem[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const ui: ReactNode = (
    <QueryClientProvider client={client}>
      <EpicMilestoneDropdown epic={epicItem} items={items} />
    </QueryClientProvider>
  );
  return render(ui);
}

describe('EpicMilestoneDropdown (gm-o2k9)', () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('shows the current milestone title when the epic is parented', () => {
    const m1 = milestone('m-1', 'M1 Beta');
    const e = epic('e-1', { parent: 'm-1' });
    // The epic's milestone is found via parent_child lookup; the items
    // list must include both the epic and the milestone.
    mount(e, [e, m1]);
    const trigger = screen.getByTestId('epic-milestone-trigger');
    expect(trigger.textContent).toContain('M1 Beta');
  });

  it('shows "None" when the epic is unparented', () => {
    const e = epic('e-1');
    mount(e, [e, milestone('m-1', 'M1 Beta')]);
    const trigger = screen.getByTestId('epic-milestone-trigger');
    expect(trigger.textContent).toContain('None');
  });

  it('selecting a milestone PATCHes parent_id to that id', async () => {
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ ...epic('e-1') }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const m1 = milestone('m-1', 'M1 Beta');
    const e = epic('e-1');
    mount(e, [e, m1]);
    fireEvent.click(screen.getByTestId('epic-milestone-trigger'));
    fireEvent.click(screen.getByTestId('epic-milestone-option-m-1'));

    await waitFor(() =>
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
        ),
      ).toBe(true),
    );
    const patch = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
    )!;
    const body = JSON.parse(patch[1].body as string);
    expect(body).toEqual({ parent_id: 'm-1' });
  });

  it('selecting "None" PATCHes parent_id="" (clear)', async () => {
    fetchSpy.mockResolvedValue(
      new Response(JSON.stringify({ ...epic('e-1') }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const m1 = milestone('m-1', 'M1 Beta');
    const e = epic('e-1', { parent: 'm-1' });
    mount(e, [e, m1]);
    fireEvent.click(screen.getByTestId('epic-milestone-trigger'));
    fireEvent.click(screen.getByTestId('epic-milestone-option-__none__'));

    await waitFor(() =>
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
        ),
      ).toBe(true),
    );
    const patch = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
    )!;
    const body = JSON.parse(patch[1].body as string);
    expect(body).toEqual({ parent_id: '' });
  });

  it('selecting the already-active milestone is a no-op', async () => {
    const m1 = milestone('m-1', 'M1 Beta');
    const e = epic('e-1', { parent: 'm-1' });
    mount(e, [e, m1]);
    fireEvent.click(screen.getByTestId('epic-milestone-trigger'));
    fireEvent.click(screen.getByTestId('epic-milestone-option-m-1'));
    await Promise.resolve();
    const patches = fetchSpy.mock.calls.filter(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH',
    );
    expect(patches).toHaveLength(0);
  });

  it('lists "None" plus every milestone in the project', () => {
    const items = [
      epic('e-1'),
      milestone('m-late', 'M5 Late'),
      milestone('m-early', 'M1 Early'),
    ];
    mount(items[0] as WorkItem, items);
    fireEvent.click(screen.getByTestId('epic-milestone-trigger'));
    const opts = Array.from(
      screen.getByTestId('epic-milestone-options').querySelectorAll('[role="option"]'),
    ).map((el) => el.getAttribute('data-testid'));
    expect(opts).toEqual([
      'epic-milestone-option-__none__',
      'epic-milestone-option-m-early',
      'epic-milestone-option-m-late',
    ]);
  });
});
