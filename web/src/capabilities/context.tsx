import { useMemo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { CapabilitiesContext, type CapabilityState } from './context-internal';
import type { CapabilitiesResponse } from './types';

// The capability manifests change rarely — once at connect, and again on
// adaptor restart. We poll anyway (at a slow cadence) because there is no
// SSE hub yet; when gm-e4.3 lands, this can become a keyed invalidation.
const POLL_MS = 30_000;

async function fetchCapabilities(): Promise<CapabilitiesResponse> {
  const r = await fetch('/api/capabilities');
  if (!r.ok) {
    throw new Error(`/api/capabilities: ${r.status}`);
  }
  return r.json();
}

export interface CapabilitiesProviderProps {
  children: ReactNode;
  // initial lets tests and Storybook seed a manifest without network. When
  // set, the fetching query is skipped.
  initial?: CapabilitiesResponse;
}

export function CapabilitiesProvider({ children, initial }: CapabilitiesProviderProps) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['capabilities'],
    queryFn: fetchCapabilities,
    refetchInterval: POLL_MS,
    refetchIntervalInBackground: false,
    staleTime: POLL_MS / 2,
    enabled: initial === undefined,
  });

  const value = useMemo<CapabilityState>(() => {
    const resolved = initial ?? data;
    return {
      workPlane: resolved?.work_plane ?? null,
      orchestrationPlane: resolved?.orchestration_plane ?? null,
      loading: initial === undefined && isLoading,
      error: (error as Error) ?? null,
    };
  }, [initial, data, isLoading, error]);

  return <CapabilitiesContext.Provider value={value}>{children}</CapabilitiesContext.Provider>;
}
