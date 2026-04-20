import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CapabilitiesProvider, useCapabilities } from '@/capabilities';

// CapabilitiesProvider is the production reader of /api/capabilities.
// These tests cover the three responses we guarantee a safe fallback
// for: success, 501 (not-implemented), and transport error. A silent
// regression on the 501 path would defeat the point of the
// capability-negotiation layer during early deployments where the
// backend has the endpoint stubbed.

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// Probe harness: mount a consumer that captures the live value via a
// ref every render, then flush react-query resolution and return the
// last observed value. Avoids renderHook so we don't need
// @testing-library/react.
interface Observed {
  status: string;
  manifests: unknown;
  error?: string;
}

let container: HTMLDivElement | null = null;
let root: Root | null = null;
const originalFetch = global.fetch;

function mockFetch(response: Response) {
  global.fetch = vi.fn().mockResolvedValue(response) as unknown as typeof fetch;
}

beforeEach(() => {
  global.fetch = originalFetch;
});

afterEach(() => {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
  global.fetch = originalFetch;
});

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0, gcTime: 0 } },
  });
}

async function observe(): Promise<Observed> {
  const observed: Observed = { status: 'loading', manifests: null };
  function Probe() {
    const v = useCapabilities();
    observed.status = v.status;
    observed.manifests = v.manifests;
    observed.error = v.error;
    return null;
  }

  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  const client = makeClient();

  act(() => {
    root!.render(
      <QueryClientProvider client={client}>
        <CapabilitiesProvider>
          <Probe />
        </CapabilitiesProvider>
      </QueryClientProvider>
    );
  });

  // Wait for react-query to settle. We don't have waitFor without
  // @testing-library; one microtask flush + one macrotask is enough
  // when the underlying fetch resolves synchronously.
  for (let i = 0; i < 20; i++) {
    if (observed.status !== 'loading') break;
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }

  return observed;
}

describe('CapabilitiesProvider', () => {
  it('loads manifests from /api/capabilities', async () => {
    mockFetch(
      jsonResponse({
        work: {
          adaptor_name: 'beads',
          adaptor_version: '0.1',
          protocol_version: '1',
          transport: 'jsonl',
          state_map: { open: 'unstarted' },
          sprint_native: true,
          token_budget_enforced: false,
          evidence_synthesis_required: false,
        },
        orchestration: null,
      })
    );

    const v = await observe();
    expect(v.status).toBe('ready');
    const m = v.manifests as {
      work: { adaptor_name: string; sprint_native: boolean } | null;
      orchestration: unknown;
    };
    expect(m.work?.adaptor_name).toBe('beads');
    expect(m.work?.sprint_native).toBe(true);
    expect(m.orchestration).toBeNull();
  });

  it('falls back to empty manifests when the endpoint returns 501', async () => {
    mockFetch(new Response('', { status: 501 }));

    const v = await observe();
    expect(v.status).toBe('ready');
    expect(v.manifests).toEqual({ work: null, orchestration: null });
    expect(v.error).toBeUndefined();
  });

  it('falls back to empty manifests when the endpoint returns 404', async () => {
    mockFetch(new Response('', { status: 404 }));

    const v = await observe();
    expect(v.status).toBe('ready');
    expect(v.manifests).toEqual({ work: null, orchestration: null });
  });

  it('surfaces a non-OK response as an error status', async () => {
    mockFetch(new Response('', { status: 500 }));

    const v = await observe();
    expect(v.status).toBe('error');
    expect(v.manifests).toEqual({ work: null, orchestration: null });
    expect(v.error).toContain('/api/capabilities');
  });
});
