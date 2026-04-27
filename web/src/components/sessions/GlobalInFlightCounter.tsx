// GlobalInFlightCounter (gm-root.16.2). SPA-chrome pill summing
// in_flight across every session whose agent type has
// intra_parallel=true. Mounted in the Topbar so the operator gets
// an at-a-glance answer to "how parallel is the system right now?"
// — the second axis the parent epic (gm-root.16) calls out.
//
// Renders nothing when the count is zero. We considered showing
// "0" always, but the empty state is the steady-state for a
// single-stream-only setup, and a permanent zero pill is noise.

import { useSessionParallelismMap } from '@/hooks/useSessionParallelism';
import { totalInFlight } from '@/api/parallelism';
import { Layers } from 'lucide-react';

export interface GlobalInFlightCounterProps {
  testid?: string;
}

export function GlobalInFlightCounter({
  testid = 'global-in-flight',
}: GlobalInFlightCounterProps) {
  const map = useSessionParallelismMap();
  const total = totalInFlight(map);
  if (total <= 0) return null;
  return (
    <span
      data-testid={testid}
      data-total={total}
      title={`${total} parallel beads in flight across sessions`}
      className="inline-flex items-center gap-1 rounded-md border border-sky-200 bg-sky-50 px-2 py-0.5 text-xs font-medium text-sky-700 dark:border-sky-900 dark:bg-sky-950 dark:text-sky-300"
    >
      <Layers className="h-3 w-3" aria-hidden />
      <span className="font-mono tabular-nums">{total}</span>
      <span className="opacity-70">in flight</span>
    </span>
  );
}
