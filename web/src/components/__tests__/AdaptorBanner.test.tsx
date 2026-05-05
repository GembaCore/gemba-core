// AdaptorBanner reactive tests (gm-root.7). The banner must not poll or
// subscribe on mount. It reacts to apiFetch's local
// gemba:adaptor-operation-failed event, performs one fresh heartbeat,
// and renders only when that heartbeat also reports/fails degraded.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { AdaptorBanner } from '../AdaptorBanner';
import {
  ADAPTOR_OPERATION_FAILED_EVENT,
  type AdaptorOperationFailedDetail,
} from '@/api/client';

function wrapper(): (props: { children: ReactNode }) => JSX.Element {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function operationFailed(detail: Partial<AdaptorOperationFailedDetail> = {}) {
  window.dispatchEvent(
    new CustomEvent(ADAPTOR_OPERATION_FAILED_EVENT, {
      detail: {
        status: 503,
        code: 'adaptor_degraded',
        message: 'operation failed',
        url: '/api/work-items',
        ...detail,
      },
    })
  );
}

describe('AdaptorBanner', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('renders nothing and does not probe on mount', () => {
    render(<AdaptorBanner />, { wrapper: wrapper() });
    expect(screen.queryByTestId('adaptor-banner')).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('stays hidden when an operation fails but the heartbeat is healthy', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ adaptors: [{ name: 'beads', plane: 'work', healthy: true }] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );

    render(<AdaptorBanner />, { wrapper: wrapper() });
    act(() => operationFailed());

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/adaptors?refresh=1',
      expect.objectContaining({
        headers: expect.objectContaining({ Accept: 'application/json' }),
      })
    );
    expect(screen.queryByTestId('adaptor-banner')).toBeNull();
  });

  it('renders the banner when an operation fails and the heartbeat reports degraded', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          adaptors: [
            { name: 'beads', plane: 'work', healthy: false, reason: 'Dolt unreachable' },
            { name: 'native', plane: 'orchestration', healthy: true },
          ],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      )
    );

    render(<AdaptorBanner />, { wrapper: wrapper() });
    act(() => operationFailed());

    await waitFor(() => expect(screen.getByTestId('adaptor-banner')).toBeTruthy());
    expect(screen.getByText('beads')).toBeTruthy();
    expect(screen.getByText(/Dolt unreachable/)).toBeTruthy();
    expect(screen.queryByText('native')).toBeNull();
  });

  it('renders a heartbeat failure when the confirmatory health check cannot run', async () => {
    fetchSpy.mockRejectedValueOnce(new TypeError('Failed to fetch'));

    render(<AdaptorBanner />, { wrapper: wrapper() });
    act(() => operationFailed({ code: 'adaptor_not_configured' }));

    await waitFor(() => expect(screen.getByTestId('adaptor-banner')).toBeTruthy());
    expect(screen.getByText('health check')).toBeTruthy();
    expect(screen.getByText(/heartbeat failed after adaptor_not_configured/)).toBeTruthy();
  });

  it('removes the reactive listener when unmounted', () => {
    const { unmount } = render(<AdaptorBanner />, { wrapper: wrapper() });
    unmount();
    act(() => operationFailed());
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
