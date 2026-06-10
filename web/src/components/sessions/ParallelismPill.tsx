// ParallelismPill (gm-root.16.2). Tiny pill shown wherever an
// agent/session card lives, surfacing how many beads are in-flight
// on the session vs. its declared cap. Pure component — props
// drive every state.
//
// Four states (per the bead's DoD):
//   - zero:        in_flight=0, intra_parallel=true  → "0/N"
//   - partial:     0 < in_flight < max_parallel       → "K/N"
//   - at-cap:      in_flight === max_parallel         → "N/N" with cap tone
//   - suppressed:  intra_parallel=false               → renders null
//
// We render `null` for suppressed sessions rather than a degenerate
// "1/1" pill — the parent card already conveys "this session is
// running" via its status badge, and adding an extra glyph here
// just adds noise. (Bead leaves this to design review; chose the
// quieter option.)

import { cn } from '@/lib/utils';

export interface ParallelismPillProps {
  inFlight: number;
  maxParallel: number;
  intraParallel: boolean;
  testid?: string;
  // className lets the parent control sizing/positioning without
  // us needing a wrapper div. Pill is span-level by default so
  // it can sit inline with status badges.
  className?: string;
}

export function ParallelismPill({
  inFlight,
  maxParallel,
  intraParallel,
  testid = 'parallelism-pill',
  className,
}: ParallelismPillProps) {
  if (!intraParallel) return null;
  const atCap = maxParallel > 0 && inFlight >= maxParallel;
  const partial = inFlight > 0 && !atCap;
  return (
    <span
      data-testid={testid}
      data-state={atCap ? 'at-cap' : partial ? 'partial' : 'zero'}
      data-in-flight={inFlight}
      data-max-parallel={maxParallel}
      title={`${inFlight} of ${maxParallel} parallel beads in flight`}
      className={cn(
        'inline-flex items-center rounded px-1.5 py-0.5 font-mono text-[10px] tabular-nums',
        atCap
          ? 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300'
          : partial
            ? 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300'
            : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400',
        className
      )}
    >
      {inFlight}/{maxParallel}
    </span>
  );
}
