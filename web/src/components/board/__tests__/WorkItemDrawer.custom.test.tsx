// Custom-field editor tests (gm-root.13). Covers the type → editor
// mapping and the round-trip through PATCH /api/work-items/{id} where
// the drawer writes a single custom key via a spread of the original
// custom map (so adaptors that replace-rather-than-merge don't drop
// the other keys).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { WorkItemDrawer } from '../WorkItemDrawer';
import { CapabilitiesProvider } from '@/capabilities';
import type { CapabilitiesResponse } from '@/capabilities';
import type { WorkItem } from '@/types/core.gen';

function capsWithFieldTypes(
  fieldExtensions: Array<{ name: string; type: string }>
): CapabilitiesResponse {
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
      field_extensions: fieldExtensions,
    },
    orchestration_plane: null,
  };
}

// state_category must be non-started for canEdit('custom') to return
// true (canEdit.ts blanket-disables edits while a bead is 'started').
const baseFixture: WorkItem = {
  id: 'gm-cf',
  kind: 'task',
  title: 'Custom-field fixture',
  status: 'open',
  state_category: 'unstarted',
  created_at: '2026-04-24T00:00:00Z',
  updated_at: '2026-04-24T00:00:00Z',
  custom: {
    'gt:role': 'polecat',
    'gt:count': 3,
    'gt:active': true,
    'gt:notes': '# heading',
    'gt:config': { nested: 'value' },
  },
};

function wrapper(caps: CapabilitiesResponse): (p: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <CapabilitiesProvider initial={caps}>{children}</CapabilitiesProvider>
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

async function openExtensionsTab(
  fetchSpy: ReturnType<typeof vi.fn>,
  caps: CapabilitiesResponse
) {
  fetchSpy.mockResolvedValueOnce(mockJSON(baseFixture));
  render(<WorkItemDrawer openId="gm-cf" onClose={() => {}} />, {
    wrapper: wrapper(caps),
  });
  await waitFor(() => expect(screen.getByTestId('drawer-tab-extensions')).toBeTruthy());
  act(() => {
    screen.getByTestId('drawer-tab-extensions').click();
  });
  await waitFor(() => expect(screen.getByTestId('section-custom')).toBeTruthy());
}

function clickEdit(key: string) {
  act(() => {
    screen.getByTestId(`custom-${key}-edit`).click();
  });
}

function findPatch(fetchSpy: ReturnType<typeof vi.fn>): [string, RequestInit] | null {
  const call = fetchSpy.mock.calls.find(
    ([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
  );
  return (call as [string, RequestInit]) ?? null;
}

describe('WorkItemDrawer custom-field editor', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders a text input for string-typed fields', async () => {
    await openExtensionsTab(
      fetchSpy,
      capsWithFieldTypes([{ name: 'gt:role', type: 'string' }])
    );
    clickEdit('gt:role');
    const input = screen.getByTestId('custom-gt:role-input') as HTMLInputElement;
    expect(input.tagName).toBe('INPUT');
    expect(input.type).toBe('text');
    expect(input.value).toBe('polecat');
  });

  it('renders a number input for number-typed fields', async () => {
    await openExtensionsTab(
      fetchSpy,
      capsWithFieldTypes([{ name: 'gt:count', type: 'number' }])
    );
    clickEdit('gt:count');
    const input = screen.getByTestId('custom-gt:count-input') as HTMLInputElement;
    expect(input.type).toBe('number');
    expect(input.value).toBe('3');
  });

  it('renders a checkbox for boolean-typed fields', async () => {
    await openExtensionsTab(
      fetchSpy,
      capsWithFieldTypes([{ name: 'gt:active', type: 'boolean' }])
    );
    clickEdit('gt:active');
    const input = screen.getByTestId('custom-gt:active-input') as HTMLInputElement;
    expect(input.type).toBe('checkbox');
    expect(input.checked).toBe(true);
  });

  it('renders a textarea for markdown-typed fields', async () => {
    await openExtensionsTab(
      fetchSpy,
      capsWithFieldTypes([{ name: 'gt:notes', type: 'markdown' }])
    );
    clickEdit('gt:notes');
    const input = screen.getByTestId('custom-gt:notes-input') as HTMLTextAreaElement;
    expect(input.tagName).toBe('TEXTAREA');
    expect(input.value).toBe('# heading');
  });

  it('falls back to a JSON textarea for unknown / object values', async () => {
    // No manifest declaration → kind inferred from value. Object value
    // yields JSON kind.
    await openExtensionsTab(fetchSpy, capsWithFieldTypes([]));
    clickEdit('gt:config');
    const input = screen.getByTestId('custom-gt:config-input') as HTMLTextAreaElement;
    expect(input.tagName).toBe('TEXTAREA');
    expect(JSON.parse(input.value)).toEqual({ nested: 'value' });
  });

  it('round-trips: edits a string + saves → PATCH carries full custom map with new value', async () => {
    await openExtensionsTab(
      fetchSpy,
      capsWithFieldTypes([{ name: 'gt:role', type: 'string' }])
    );
    clickEdit('gt:role');
    const input = screen.getByTestId('custom-gt:role-input') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'scout' } });

    fetchSpy.mockResolvedValueOnce(
      mockJSON({ ...baseFixture, custom: { ...baseFixture.custom, 'gt:role': 'scout' } })
    );

    act(() => {
      screen.getByTestId('custom-gt:role-save').click();
    });

    await waitFor(() => expect(findPatch(fetchSpy)).not.toBeNull());
    const [url, init] = findPatch(fetchSpy)!;
    expect(url).toMatch(/\/work-items\/gm-cf$/);
    const body = JSON.parse(init.body as string);
    expect(body.custom).toEqual({
      ...baseFixture.custom,
      'gt:role': 'scout',
    });
  });

  it('saving unchanged value is a no-op (no PATCH fires)', async () => {
    await openExtensionsTab(
      fetchSpy,
      capsWithFieldTypes([{ name: 'gt:role', type: 'string' }])
    );
    clickEdit('gt:role');
    // Save without changing the draft.
    act(() => {
      screen.getByTestId('custom-gt:role-save').click();
    });
    // Wait a tick so any PATCH would have fired.
    await new Promise((r) => setTimeout(r, 0));
    expect(findPatch(fetchSpy)).toBeNull();
  });

  it('rejects invalid JSON with an inline error', async () => {
    await openExtensionsTab(fetchSpy, capsWithFieldTypes([]));
    clickEdit('gt:config');
    const input = screen.getByTestId('custom-gt:config-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: '{ not json' } });
    act(() => {
      screen.getByTestId('custom-gt:config-save').click();
    });
    expect(screen.getByTestId('custom-gt:config-error')).toBeTruthy();
    // No PATCH.
    expect(findPatch(fetchSpy)).toBeNull();
  });
});
