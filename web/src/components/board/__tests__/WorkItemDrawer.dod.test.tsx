// Structured DoD editor tests (gm-root.12). Covers add / remove /
// reorder of acceptance criteria plus the round-trip through the
// PATCH /api/work-items/{id} mutation.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { WorkItemDrawer } from '../WorkItemDrawer';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';

function caps(format?: string): CapabilitiesResponse {
  return {
    work_plane: {
      adaptor_name: 'fake',
      adaptor_version: '0.1.0',
      protocol_version: '0.1.0',
      transport: 'api',
      state_map: { open: 'unstarted' },
      sprint_native: false,
      token_budget_enforced: false,
      evidence_synthesis_required: false,
      description_format: format,
    },
    orchestration_plane: null,
  };
}

// An editable fixture — state_category is a non-started bucket so
// canEdit('dod') returns true.
const baseFixture: WorkItem = {
  id: 'gm-dod',
  kind: 'task',
  title: 'DoD fixture',
  status: 'open',
  state_category: 'unstarted',
  created_at: '2026-04-24T00:00:00Z',
  updated_at: '2026-04-24T00:00:00Z',
  dod: {
    acceptance_criteria: ['one', 'two', 'three'],
    notes: 'base notes',
    version: '1',
  },
};

function wrapper(
  capsResp: CapabilitiesResponse = caps()
): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={capsResp}>{children}</CapabilitiesProvider>
      </QueryClientProvider>
    );
  };
}

function mockJSON(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

async function openEditor(fetchSpy: ReturnType<typeof vi.fn>, item: WorkItem = baseFixture) {
  fetchSpy.mockResolvedValueOnce(mockJSON(item));
  render(<WorkItemDrawer openId={item.id} onClose={() => {}} />, { wrapper: wrapper() });
  await waitFor(() => expect(screen.getByTestId('section-dod')).toBeTruthy());
  act(() => {
    screen.getByTestId('work-item-dod-edit').click();
  });
  return screen.getByTestId('work-item-dod-editing');
}

describe('WorkItemDrawer DoD editor', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders one row per existing criterion', async () => {
    const panel = await openEditor(fetchSpy);
    const inputs = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    expect(inputs.map((i) => i.value)).toEqual(['one', 'two', 'three']);
  });

  it('adds a row via the "Add criterion" button', async () => {
    const panel = await openEditor(fetchSpy);
    act(() => {
      within(panel).getByTestId('work-item-dod-add-criterion').click();
    });
    const inputs = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    expect(inputs.map((i) => i.value)).toEqual(['one', 'two', 'three', '']);
  });

  it('removes a row via the trash button', async () => {
    const panel = await openEditor(fetchSpy);
    act(() => {
      within(panel).getByTestId('work-item-dod-criterion-1-remove').click();
    });
    const inputs = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    expect(inputs.map((i) => i.value)).toEqual(['one', 'three']);
  });

  it('reorders via up/down arrows and disables them at the ends', async () => {
    const panel = await openEditor(fetchSpy);

    // First row's "up" and last row's "down" are disabled at the ends.
    expect(
      (within(panel).getByTestId('work-item-dod-criterion-0-up') as HTMLButtonElement).disabled
    ).toBe(true);
    expect(
      (within(panel).getByTestId('work-item-dod-criterion-2-down') as HTMLButtonElement).disabled
    ).toBe(true);

    // Move "two" down → [one, three, two]
    act(() => {
      within(panel).getByTestId('work-item-dod-criterion-1-down').click();
    });
    let inputs = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    expect(inputs.map((i) => i.value)).toEqual(['one', 'three', 'two']);

    // Move "two" (now at index 2) up twice → [two, one, three]
    act(() => {
      within(panel).getByTestId('work-item-dod-criterion-2-up').click();
    });
    act(() => {
      within(panel).getByTestId('work-item-dod-criterion-1-up').click();
    });
    inputs = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    expect(inputs.map((i) => i.value)).toEqual(['two', 'one', 'three']);
  });

  it('round-trips: edit criterion + save sends a PATCH with the new list', async () => {
    const panel = await openEditor(fetchSpy);

    // Rename "two" → "TWO-renamed" and append a new row with text.
    const inputs = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    fireEvent.change(inputs[1], { target: { value: 'TWO-renamed' } });

    act(() => {
      within(panel).getByTestId('work-item-dod-add-criterion').click();
    });
    const after = within(panel).getAllByPlaceholderText('criterion') as HTMLInputElement[];
    fireEvent.change(after[3], { target: { value: 'four' } });

    // Mock the PATCH response (happens on Save).
    fetchSpy.mockResolvedValueOnce(
      mockJSON({
        ...baseFixture,
        dod: {
          acceptance_criteria: ['one', 'TWO-renamed', 'three', 'four'],
          notes: 'base notes',
          version: '1',
        },
      })
    );

    act(() => {
      within(panel).getByTestId('work-item-dod-save').click();
    });

    // onSettled invalidation triggers additional GETs, so find the
    // PATCH explicitly rather than counting calls.
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
        )
      ).toBe(true);
    });
    const patchCall = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
    ) as [string, RequestInit];
    expect(patchCall[0]).toMatch(/\/work-items\/gm-dod$/);
    const body = JSON.parse(patchCall[1].body as string);
    expect(body.dod).toEqual({
      acceptance_criteria: ['one', 'TWO-renamed', 'three', 'four'],
      notes: 'base notes',
      version: '1',
    });
  });

  it('clears the DoD (sends null) when save fires with empty criteria/notes/version', async () => {
    // Fixture without any DoD — the editor should treat save with
    // empty fields as "clear" and send { dod: null }.
    const empty: WorkItem = { ...baseFixture, dod: undefined };
    const panel = await openEditor(fetchSpy, empty);

    // Seed-row gets an empty input; leave it untouched. Save.
    fetchSpy.mockResolvedValueOnce(mockJSON({ ...empty }));
    act(() => {
      within(panel).getByTestId('work-item-dod-save').click();
    });
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
        )
      ).toBe(true);
    });
    const patchCall = fetchSpy.mock.calls.find(
      ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
    ) as [string, RequestInit];
    const body = JSON.parse(patchCall[1].body as string);
    expect(body.dod).toBeNull();
  });

  it('renders notes via the markdown renderer when the adaptor declares it', async () => {
    fetchSpy.mockResolvedValueOnce(
      mockJSON({
        ...baseFixture,
        dod: {
          acceptance_criteria: ['stub'],
          notes: '# DoD heading\n\n- a\n- b',
        },
      })
    );
    render(<WorkItemDrawer openId="gm-dod" onClose={() => {}} />, {
      wrapper: wrapper(caps('markdown')),
    });
    // Lazy markdown chunk resolves; the renderer component for notes
    // is the same one used for description, so the data-testid is
    // "description-markdown".
    const md = await screen.findAllByTestId('description-markdown');
    // There may be two (description + dod notes); at least one should
    // contain our DoD heading.
    expect(md.some((el) => el.querySelector('h1')?.textContent === 'DoD heading')).toBe(true);
  });
});
