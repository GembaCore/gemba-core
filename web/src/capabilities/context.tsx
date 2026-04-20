import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  CapabilitiesContext,
  EMPTY_MANIFESTS,
  type CapabilitiesContextValue,
  type CapabilitiesStatus,
} from './context-value';
import type { Manifests } from './types';

// CapabilitiesProvider surfaces the currently negotiated manifests to
// the whole tree (gm-e11.4). Consumers MUST NOT read the raw network
// shape — always go through useCapabilities so that a missing backend
// degrades to the minimal default (hide everything optional) instead
// of throwing in render.
//
// The minimal default (both halves null) is the shape the UI uses
// before the first successful fetch, and also when /api/capabilities
// is not yet implemented on the backend. Every capability gate must
// treat null as "off" so controls are hidden by default, not disabled.

async function fetchManifests(): Promise<Manifests> {
  const r = await fetch('/api/capabilities');
  if (r.status === 404 || r.status === 501) {
    // Endpoint not yet implemented or the deployment has no adaptor
    // configured. Fall through to the minimal default rather than
    // surfacing an error banner — the AdaptorBanner already handles
    // the "degraded" case; capability negotiation is a secondary
    // signal and must not add noise here.
    return EMPTY_MANIFESTS;
  }
  if (!r.ok) {
    throw new Error(`/api/capabilities: ${r.status}`);
  }
  const body = (await r.json()) as Partial<Manifests>;
  return {
    work: body.work ?? null,
    orchestration: body.orchestration ?? null,
  };
}

export function CapabilitiesProvider({ children }: { children: ReactNode }) {
  const { data, status, error, refetch } = useQuery({
    queryKey: ['capabilities'],
    queryFn: fetchManifests,
    // Capability manifests are stable for the life of an adaptor
    // binding; the server re-emits them on reconnect. A 60s stale
    // window keeps the UI coherent without flooding a freshly-
    // started backend.
    staleTime: 60_000,
  });

  const resolvedStatus: CapabilitiesStatus =
    status === 'pending' ? 'loading' : status === 'success' ? 'ready' : 'error';

  const value: CapabilitiesContextValue = {
    manifests: data ?? EMPTY_MANIFESTS,
    status: resolvedStatus,
    error: error instanceof Error ? error.message : undefined,
    refetch: () => {
      void refetch();
    },
  };

  return <CapabilitiesContext.Provider value={value}>{children}</CapabilitiesContext.Provider>;
}

// CapabilitiesTestProvider takes manifests directly, bypassing the
// network. Keeps unit tests hermetic without needing MSW.
export function CapabilitiesTestProvider({
  manifests,
  status = 'ready',
  children,
}: {
  manifests: Manifests;
  status?: CapabilitiesStatus;
  children: ReactNode;
}) {
  const value: CapabilitiesContextValue = {
    manifests,
    status,
    refetch: () => {
      /* no-op in tests */
    },
  };
  return <CapabilitiesContext.Provider value={value}>{children}</CapabilitiesContext.Provider>;
}
