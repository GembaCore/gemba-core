// useSessionParallelism (gm-root.16.2). Reactive read of the
// session_id → parallelism state map maintained by the SSE consumer.
// Backed by useSyncExternalStore against a module-level store
// (web/src/api/parallelism.ts) so tests don't need to wrap callers
// in QueryClientProvider just to render a pill.

import { useSyncExternalStore } from 'react';
import {
  getParallelismMap,
  subscribe,
  type SessionParallelismMap,
  type SessionParallelismState,
} from '@/api/parallelism';

export function useSessionParallelismMap(): SessionParallelismMap {
  return useSyncExternalStore(subscribe, getParallelismMap, getParallelismMap);
}

export function useSessionParallelism(sessionId: string): SessionParallelismState | null {
  const map = useSessionParallelismMap();
  return map[sessionId] ?? null;
}
