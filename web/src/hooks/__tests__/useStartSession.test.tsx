// gm-root.26 item 2 — toast on session start.
//
// Verifies that useStartSession fires a discoverability toast on
// successful POST /api/sessions. The toast is wired centrally on the
// hook so every call site (board drag-to-spawn, NewSessionDialog,
// future surfaces) inherits the behavior automatically.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { useStartSession } from '../useSessions';
import type { Session } from '@/api/sessions';

// Mock the toast module so we can assert on the show() call without
// mounting the viewport / clipping into router-aware rendering.
vi.mock('@/components/ui/ToastContext', () => {
  const show = vi.fn();
  const dismiss = vi.fn();
  return {
    useToast: () => ({ show, dismiss }),
    ToastProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
    __mock: { show, dismiss },
  };
});

import * as toastModule from '@/components/ui/ToastContext';
const mockShow = (toastModule as unknown as { __mock: { show: ReturnType<typeof vi.fn> } }).__mock.show;

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </MemoryRouter>
    );
  };
}

const sampleSession: Session = {
  id: 'sess-abc123',
  assignment_id: 'assn-1',
  agent_id: 'agent-1' as Session['agent_id'],
  status: 'working',
  started_at: '2026-04-29T00:00:00Z',
};

describe('useStartSession — discoverability toast (gm-root.26 item 2)', () => {
  const fetchSpy = vi.fn();

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchSpy);
    mockShow.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    fetchSpy.mockReset();
  });

  it('fires a toast pointing at /sessions on a successful start', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify(sampleSession), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useStartSession(), { wrapper: wrapper() });
    await act(async () => {
      result.current.mutate({ kind: 'bead', bead_id: 'gm-x', agent_type: 'claude' });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(mockShow).toHaveBeenCalledTimes(1);
    const arg = mockShow.mock.calls[0][0];
    expect(arg.message).toContain('sess-abc123');
    expect(arg.message).toContain('/sessions');
    expect(arg.action).toEqual({ to: '/sessions', label: 'Open /sessions' });
  });

  it('does not fire a toast on failure', async () => {
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ error: 'bad' }), {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const { result } = renderHook(() => useStartSession(), { wrapper: wrapper() });
    await act(async () => {
      result.current.mutate({ kind: 'bead', bead_id: 'gm-x', agent_type: 'claude' });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(mockShow).not.toHaveBeenCalled();
  });
});
