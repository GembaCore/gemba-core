import { createContext, useContext } from 'react';
import type { OrchestrationManifest, WorkPlaneManifest } from './types';

// Split out of context.tsx so the Provider (component) and the
// useCapabilities hook live in separate files — eslint's react-refresh
// rule rejects mixed exports, and the hook is the natural thing to
// share imperatively.

export interface CapabilityState {
  workPlane: WorkPlaneManifest | null;
  orchestrationPlane: OrchestrationManifest | null;
  // loading: true on the very first fetch only; subsequent background
  // refetches keep the last known manifest visible so the UI doesn't
  // flicker between "hidden" and "shown" during a re-poll.
  loading: boolean;
  error: Error | null;
}

export const CapabilitiesContext = createContext<CapabilityState | null>(null);

export function useCapabilities(): CapabilityState {
  const ctx = useContext(CapabilitiesContext);
  if (!ctx) {
    // A missing provider is a programmer error, not a runtime degradation —
    // throw so the mistake surfaces at the component under test rather
    // than silently rendering an empty manifest (which would make every
    // Capability gate evaluate false and hide the whole UI).
    throw new Error('useCapabilities: no CapabilitiesProvider in tree');
  }
  return ctx;
}
