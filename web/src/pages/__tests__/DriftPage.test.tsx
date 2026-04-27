// DriftPage tests (gm-e12.13). Pins the loaded / empty / error
// branches plus the 501 → empty-snapshot behavior of the api/drift
// fetcher. Uses fetch-stubbing rather than module mocking so the
// real apiFetch + getDriftSnapshot path runs end-to-end.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { DriftPage } from '../DriftPage';
import { getDriftSnapshot } from '@/api/drift';
import { ApiError } from '@/api/client';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    );
  };
}

describe('DriftPage', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders rows with correct values when drift is present', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        drift: [
          {
            kind: 'session',
            id: 's-1',
            field: 'status',
            desired: 'working',
            actual: 'paused',
          },
          {
            kind: 'agent',
            id: 'gemba/polecats/obsidian',
            field: 'pack',
            desired: 'planner',
            actual: 'coach',
          },
        ],
        desired_count: 5,
        actual_count: 4,
        fetched_at: '2026-04-26T12:00:00Z',
      })
    );

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <DriftPage />
      </Wrapper>
    );

    expect(await screen.findByTestId('drift-table')).toBeTruthy();

    const sessionRow = screen.getByTestId('drift-row-session-s-1-status');
    expect(sessionRow.textContent).toContain('session');
    expect(sessionRow.textContent).toContain('s-1');
    expect(sessionRow.textContent).toContain('status');
    expect(sessionRow.textContent).toContain('working');
    expect(sessionRow.textContent).toContain('paused');

    const agentRow = screen.getByTestId(
      'drift-row-agent-gemba/polecats/obsidian-pack'
    );
    expect(agentRow.textContent).toContain('planner');
    expect(agentRow.textContent).toContain('coach');

    expect(screen.getByTestId('drift-summary-desired').textContent).toContain('5');
    expect(screen.getByTestId('drift-summary-actual').textContent).toContain('4');
    expect(screen.getByTestId('drift-summary-drift').textContent).toContain('2');
  });

  it('renders the empty state when drift is empty', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        drift: [],
        desired_count: 3,
        actual_count: 3,
        fetched_at: '2026-04-26T12:00:00Z',
      })
    );

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <DriftPage />
      </Wrapper>
    );

    expect(await screen.findByTestId('drift-empty')).toBeTruthy();
    expect(screen.getByTestId('drift-empty').textContent).toMatch(
      /no drift detected/i
    );
    expect(screen.getByTestId('drift-summary-desired').textContent).toContain('3');
    expect(screen.queryByTestId('drift-table')).toBeNull();
  });

  it('renders the error state when the fetcher throws', async () => {
    // 503 with the full envelope → ApiError surfaces through the
    // fetcher (501 is the only swallowed status).
    fetchSpy.mockResolvedValue(
      jsonResponse({ error: 'adaptor_degraded', message: 'down' }, 503)
    );

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <DriftPage />
      </Wrapper>
    );

    const err = await screen.findByTestId('drift-error');
    expect(err.textContent).toMatch(/could not load drift data/i);
    expect(err.textContent).toContain('down');
  });

  it('renders the loading state on first paint', () => {
    fetchSpy.mockImplementation(() => new Promise(() => {}));
    const Wrapper = wrapper();
    render(
      <Wrapper>
        <DriftPage />
      </Wrapper>
    );
    expect(screen.getByTestId('drift-loading')).toBeTruthy();
  });

  it('renders the empty state when /api/drift returns 501 (stubbed endpoint)', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ error: 'not implemented yet; see gm-e2.6' }, 501)
    );

    const Wrapper = wrapper();
    render(
      <Wrapper>
        <DriftPage />
      </Wrapper>
    );

    // Page renders the loaded path with zeroed counts and the empty
    // state — no error banner.
    expect(await screen.findByTestId('drift-empty')).toBeTruthy();
    expect(screen.queryByTestId('drift-error')).toBeNull();
  });
});

describe('getDriftSnapshot', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('maps a 501 response to an empty snapshot', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ error: 'not implemented yet' }, 501)
    );
    const snap = await getDriftSnapshot();
    expect(snap.drift).toEqual([]);
    expect(snap.desired_count).toBe(0);
    expect(snap.actual_count).toBe(0);
  });

  it('rethrows non-501 ApiError', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({ error: 'adaptor_degraded', message: 'down' }, 503)
    );
    await expect(getDriftSnapshot()).rejects.toBeInstanceOf(ApiError);
  });

  it('returns parsed snapshot on 200', async () => {
    fetchSpy.mockResolvedValue(
      jsonResponse({
        drift: [
          { kind: 'session', id: 's-1', field: 'status', desired: 'a', actual: 'b' },
        ],
        desired_count: 1,
        actual_count: 1,
        fetched_at: '2026-04-26T00:00:00Z',
      })
    );
    const snap = await getDriftSnapshot();
    expect(snap.drift).toHaveLength(1);
    expect(snap.drift[0].kind).toBe('session');
  });
});
