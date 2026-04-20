import { createContext } from 'react';
import type { Manifests } from './types';

// CapabilitiesContext lives in a .ts file (no JSX) so that
// eslint-plugin-react-refresh can keep the companion .tsx files
// component-only for fast-refresh. Consumers should import the hook
// and provider from '@/capabilities' rather than reaching for the raw
// context directly.

export type CapabilitiesStatus = 'loading' | 'ready' | 'error';

export interface CapabilitiesContextValue {
  manifests: Manifests;
  status: CapabilitiesStatus;
  error?: string;
  refetch: () => void;
}

export const EMPTY_MANIFESTS: Manifests = { work: null, orchestration: null };

export const CapabilitiesContext = createContext<CapabilitiesContextValue>({
  manifests: EMPTY_MANIFESTS,
  status: 'loading',
  refetch: () => {
    /* no-op until provider mounts */
  },
});
