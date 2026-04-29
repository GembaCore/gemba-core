// InsightsPage tests (gm-e12.17.1).
//
// Smoke coverage for the three live tiles + the deferred-cost stub +
// the time-window selector. Stubs fetch so the real apiFetch +
// fetchMetricSeries paths run end-to-end against the agreed
// /api/v1/metrics/series contract (gm-e9m0).
//
// We do NOT stand up MSW — the test suite uses vi.stubGlobal('fetch')
// across the codebase (see DriftPage.test.tsx) and adding MSW for one
// page would be inconsistent. The fetch stub here is per-test
// deterministic and matches the contract exactly, so the swap to a
// real backend is mechanical.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';
import { InsightsPage } from '../InsightsPage';
import type { MetricSeries } from '@/api/metrics';

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

// makeSeries builds a deterministic series at the requested bucket
// granularity. Backend handler is being built in parallel — this
// matches the contract exactly so the swap is mechanical.
function makeSeries(
  metric: string,
  bucket: string,
  values: number[],
  labels: Record<string, string> = {},
): MetricSeries {
  return {
    metric,
    bucket,
    labels,
    samples: values.map((v, i) => ({
      t: new Date(2026, 3, 15, i, 0, 0).toISOString(),
      v,
    })),
  };
}

// Routes mock fetch responses by inspecting the URL — keeps each test
// declarative ("when GET session_spawn_total, return X").
function routeFetch(routes: Record<string, MetricSeries | null>): typeof fetch {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    for (const [metric, series] of Object.entries(routes)) {
      if (url.includes(`metric=${encodeURIComponent(metric)}`) || url.includes(`metric=${metric}`)) {
        if (series === null) {
          return jsonResponse({
            metric,
            labels: {},
            samples: [],
            bucket: '1h',
          });
        }
        return jsonResponse(series);
      }
    }
    // Unknown metric → empty series so the test fails loudly through
    // the empty-state assertion rather than via a network error.
    return jsonResponse({
      metric: 'unknown',
      labels: {},
      samples: [],
      bucket: '1h',
    });
  }) as unknown as typeof fetch;
}

describe('InsightsPage', () => {
  // Pin Date.now() without using fake timers — fake timers also halt
  // React Query's retry/poll setTimeouts, which makes findBy* queries
  // hang. We only need wall-clock determinism, not full timer control.
  const FIXED_NOW = Date.UTC(2026, 3, 29, 12, 0, 0);
  let dateNowSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    dateNowSpy = vi.spyOn(Date, 'now').mockReturnValue(FIXED_NOW);
  });

  afterEach(() => {
    dateNowSpy.mockRestore();
    vi.unstubAllGlobals();
  });

  it('renders the four tiles plus the cost stub on the happy path', async () => {
    const fetchSpy = routeFetch({
      session_spawn_total: makeSeries('session_spawn_total', '3h', [0, 2, 5, 1]),
      escalation_open_count: makeSeries('escalation_open_count', '3h', [3, 4, 4, 2]),
      workitem_state_duration: makeSeries('workitem_state_duration', '3h', [120, 300, 60, 0], { state: 'prompting' }),
    });
    vi.stubGlobal('fetch', fetchSpy);

    const Wrapper = wrapper();
    await act(async () => {
      render(
        <Wrapper>
          <InsightsPage />
        </Wrapper>,
      );
    });

    // Page chrome.
    expect(screen.getByTestId('insights-page')).toBeTruthy();
    expect(screen.getByTestId('insights-grid')).toBeTruthy();
    expect(screen.getByTestId('insights-window-selector')).toBeTruthy();

    // All four tiles present (the fourth is the deferred-cost stub).
    expect(await screen.findByTestId('insights-tile-spawn-rate')).toBeTruthy();
    expect(await screen.findByTestId('insights-tile-escalation-backlog')).toBeTruthy();
    expect(await screen.findByTestId('insights-tile-stuck-minutes')).toBeTruthy();
    expect(screen.getByTestId('insights-tile-cost-deferred')).toBeTruthy();
    // Cost stub renders its placeholder body, not a chart.
    expect(screen.getByTestId('insights-tile-cost-deferred-stub')).toBeTruthy();
    expect(screen.getByTestId('insights-tile-cost-deferred-stub').textContent).toMatch(
      /awaiting gm-e11\.2/i,
    );

    // Live tiles emerged from loading into rendered state — the
    // empty-state testid only appears when there are zero samples.
    expect(screen.queryByTestId('insights-tile-spawn-rate-empty')).toBeNull();
    expect(screen.queryByTestId('insights-tile-escalation-backlog-empty')).toBeNull();
    expect(screen.queryByTestId('insights-tile-stuck-minutes-empty')).toBeNull();

    // The fetch URL surface includes the bucket and the contract
    // params — proves we're hitting /api/v1/metrics/series with the
    // shape gm-e9m0 will serve.
    const calls = (fetchSpy as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    const urls = calls.map((c) => String(c[0]));
    expect(urls.some((u) => u.includes('/api/v1/metrics/series'))).toBe(true);
    expect(urls.some((u) => u.includes('metric=session_spawn_total'))).toBe(true);
    expect(urls.some((u) => u.includes('bucket=3h'))).toBe(true); // 14d default
    expect(urls.some((u) => u.includes('since='))).toBe(true);
    expect(urls.some((u) => u.includes('until='))).toBe(true);
    // labels= only appears for the stuck-session tile.
    expect(urls.some((u) => u.includes('labels=state_category%3Astarted'))).toBe(true);
  });

  it('renders the per-tile empty state when a series has no samples', async () => {
    const fetchSpy = routeFetch({
      session_spawn_total: null, // empty
      escalation_open_count: makeSeries('escalation_open_count', '3h', [1, 2, 3]),
      workitem_state_duration: null, // empty
    });
    vi.stubGlobal('fetch', fetchSpy);

    const Wrapper = wrapper();
    await act(async () => {
      render(
        <Wrapper>
          <InsightsPage />
        </Wrapper>,
      );
    });

    expect(await screen.findByTestId('insights-tile-spawn-rate-empty')).toBeTruthy();
    expect(screen.getByTestId('insights-tile-spawn-rate-empty').textContent).toMatch(
      /no sessions spawned/i,
    );
    expect(await screen.findByTestId('insights-tile-stuck-minutes-empty')).toBeTruthy();

    // The non-empty tile is *not* in empty state.
    expect(screen.queryByTestId('insights-tile-escalation-backlog-empty')).toBeNull();
  });

  it('refetches with new bucket/window when the window selector changes', async () => {
    const fetchSpy = routeFetch({
      session_spawn_total: makeSeries('session_spawn_total', '15m', [0, 1]),
      escalation_open_count: makeSeries('escalation_open_count', '15m', [0, 1]),
      workitem_state_duration: makeSeries('workitem_state_duration', '15m', [0, 1]),
    });
    vi.stubGlobal('fetch', fetchSpy);

    const Wrapper = wrapper();
    await act(async () => {
      render(
        <Wrapper>
          <InsightsPage />
        </Wrapper>,
      );
    });

    // Initial render — default 14d → bucket=3h.
    const calls = (fetchSpy as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    const initialBuckets = calls.map((c) => String(c[0])).filter((u) => u.includes('bucket='));
    expect(initialBuckets.some((u) => u.includes('bucket=3h'))).toBe(true);

    // Click the 24h preset.
    const btn24h = screen.getByTestId('insights-window-24h');
    await act(async () => {
      btn24h.click();
    });

    // After change, fetch should have been called with bucket=15m.
    const afterUrls = calls.map((c) => String(c[0]));
    expect(afterUrls.some((u) => u.includes('bucket=15m'))).toBe(true);
    // The clicked button is now aria-checked.
    expect(btn24h.getAttribute('aria-checked')).toBe('true');
  });

  it('falls back to per-tile empty state when the backend returns 503', async () => {
    // 503 with adaptor_degraded code is the contract's "telemetry not
    // configured yet" signal — fetchMetricSeries swallows it and
    // returns an empty series, which renders as the empty-state tile.
    const fetchSpy = vi.fn(async () =>
      jsonResponse({ error: 'adaptor_degraded', message: 'metrics not wired' }, 503),
    ) as unknown as typeof fetch;
    vi.stubGlobal('fetch', fetchSpy);

    const Wrapper = wrapper();
    await act(async () => {
      render(
        <Wrapper>
          <InsightsPage />
        </Wrapper>,
      );
    });

    expect(await screen.findByTestId('insights-tile-spawn-rate-empty')).toBeTruthy();
    expect(await screen.findByTestId('insights-tile-escalation-backlog-empty')).toBeTruthy();
    expect(await screen.findByTestId('insights-tile-stuck-minutes-empty')).toBeTruthy();
    // No error banner — degraded backend collapses to the empty state.
    expect(screen.queryByTestId('insights-tile-spawn-rate-error')).toBeNull();
  });
});
