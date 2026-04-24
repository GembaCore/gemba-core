// AdaptorBanner SSE tests (gm-root.7). We stub EventSource so the test
// environment (jsdom, no network) drives onmessage / onerror
// deterministically, then assert on the rendered banner.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { AdaptorBanner } from '../AdaptorBanner';

// MockEventSource mirrors just enough of the DOM EventSource API for
// the component: readyState isn't used directly, so we only track
// handlers and expose helpers to fire frames.
class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }
  close() {
    this.closed = true;
  }
  emit(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent);
  }
  emitRaw(data: string) {
    this.onmessage?.({ data } as MessageEvent);
  }
  fail() {
    this.onerror?.();
  }
}

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe('AdaptorBanner', () => {
  beforeEach(() => {
    MockEventSource.instances = [];
    vi.stubGlobal('EventSource', MockEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders nothing until the stream delivers a frame', () => {
    render(<AdaptorBanner />, { wrapper: wrapper() });
    expect(screen.queryByTestId('adaptor-banner')).toBeNull();
    // One EventSource opened and still open.
    expect(MockEventSource.instances).toHaveLength(1);
    expect(MockEventSource.instances[0].url).toBe('/api/adaptors/stream');
  });

  it('renders the banner when the stream reports a degraded adaptor', async () => {
    render(<AdaptorBanner />, { wrapper: wrapper() });
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit({
        adaptors: [
          { name: 'beads', plane: 'work', healthy: false, reason: 'Dolt unreachable' },
          { name: 'gastown', plane: 'orchestration', healthy: true },
        ],
      });
    });

    await waitFor(() => expect(screen.getByTestId('adaptor-banner')).toBeTruthy());
    expect(screen.getByText('beads')).toBeTruthy();
    expect(screen.getByText(/Dolt unreachable/)).toBeTruthy();
    // The healthy adaptor must not leak into the banner.
    expect(screen.queryByText('gastown')).toBeNull();
  });

  it('disappears again once every adaptor recovers', async () => {
    render(<AdaptorBanner />, { wrapper: wrapper() });
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit({
        adaptors: [{ name: 'beads', plane: 'work', healthy: false, reason: 'bad' }],
      });
    });
    await waitFor(() => expect(screen.getByTestId('adaptor-banner')).toBeTruthy());

    act(() => {
      es.emit({ adaptors: [{ name: 'beads', plane: 'work', healthy: true }] });
    });
    await waitFor(() => expect(screen.queryByTestId('adaptor-banner')).toBeNull());
  });

  it('ignores malformed frames (kept alive by the next valid one)', async () => {
    render(<AdaptorBanner />, { wrapper: wrapper() });
    const es = MockEventSource.instances[0];

    act(() => {
      es.emitRaw('not-json');
    });
    expect(screen.queryByTestId('adaptor-banner')).toBeNull();

    act(() => {
      es.emit({ adaptors: [{ name: 'x', plane: 'work', healthy: false, reason: 'r' }] });
    });
    await waitFor(() => expect(screen.getByTestId('adaptor-banner')).toBeTruthy());
  });

  it('falls back to /api/adaptors on stream error when no frame has arrived', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ adaptors: [{ name: 'beads', plane: 'work', healthy: false, reason: 'q' }] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );
    vi.stubGlobal('fetch', fetchSpy);
    try {
      render(<AdaptorBanner />, { wrapper: wrapper() });
      const es = MockEventSource.instances[0];
      act(() => {
        es.fail();
      });
      await waitFor(() => expect(screen.getByTestId('adaptor-banner')).toBeTruthy());
      // The fallback fetch must have been the snapshot endpoint.
      expect(fetchSpy).toHaveBeenCalledWith('/api/adaptors');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('closes the stream when unmounted', () => {
    const { unmount } = render(<AdaptorBanner />, { wrapper: wrapper() });
    const es = MockEventSource.instances[0];
    unmount();
    expect(es.closed).toBe(true);
  });
});
