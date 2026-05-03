import { useEffect, useMemo, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { CapabilitiesContext, type CapabilityState } from './context-internal';
import type { CapabilitiesResponse } from './types';
import { observeInstanceId } from '../transport/instanceId';

// Capabilities are startup-immutable: configured once during gemba
// serve boot, never mutate during the process lifetime. We fetch once
// and never refetch (gm-6m60). The InstanceIdGuard handles the only
// real invalidation case (server restarted with new config) by full-
// reloading the SPA when a later response shows a different
// instance_id. Earlier comments in this file referenced a future
// "gm-e4.3 keyed invalidation" approach — that's been superseded;
// capabilities don't need a live channel.

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
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    enabled: initial === undefined,
  });

  // Stash the boot id from the first manifest we see. Subsequent
  // observers (the AdaptorBanner SSE) will trigger a full reload if
  // the server restarts with a different id.
  useEffect(() => {
    const id = (initial ?? data)?.instance_id;
    if (id) observeInstanceId(id);
  }, [initial, data]);

  const value = useMemo<CapabilityState>(() => {
    const resolved = initial ?? data;
    return {
      workPlane: resolved?.work_plane ?? null,
      orchestrationPlane: resolved?.orchestration_plane ?? null,
      runtimeMode: resolved?.runtime_mode ?? (resolved?.beads_only ? 'beads_only' : 'full'),
      beadsOnly: Boolean(resolved?.beads_only || resolved?.runtime_mode === 'beads_only'),
      beadsReadOnly: Boolean(resolved?.beads_read_only || resolved?.work_plane?.read_only),
      beadsSource: resolved?.beads_source ?? null,
      beadsHistoryPath: resolved?.beads_history_path ?? '',
      loading: initial === undefined && isLoading,
      error: (error as Error) ?? null,
    };
  }, [initial, data, isLoading, error]);

  return <CapabilitiesContext.Provider value={value}>{children}</CapabilitiesContext.Provider>;
}
