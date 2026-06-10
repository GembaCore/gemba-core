// Walk bottom bar (gm-i65). Per ui-spec §5.4:
//   "N decisions · M beads touched · $X spent · {duration}"
// Reads everything from the active walk in WalkContext.

import { useEffect, useState } from 'react';
import { walkDuration } from '@/api/walks';
import { useWalk } from './WalkContext';

export function BottomBar(): JSX.Element {
  const walk = useWalk();

  // Live duration tick — re-render every second while the walk is
  // active so the bar shows real-time elapsed. Paused / terminal
  // walks freeze at the last-known duration.
  const [, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!walk.walk || walk.walk.status !== 'active') return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [walk.walk?.id, walk.walk?.status]); // eslint-disable-line react-hooks/exhaustive-deps

  const w = walk.walk;
  const decisionsCount = w?.decisions?.length ?? 0;
  const beadsTouched = w?.beads_touched?.length ?? 0;
  const dollars = w?.cost?.dollars ?? 0;
  const duration = w ? walkDuration(w) : 0;

  return (
    <footer
      data-testid="walk-bottom-bar"
      className="flex shrink-0 items-center gap-4 border-t border-neutral-200 bg-neutral-50 px-4 py-1.5 text-[11px] text-neutral-600 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-300"
    >
      <Stat label="Decisions" value={decisionsCount} testid="walk-bar-decisions" />
      <Sep />
      <Stat label="Beads touched" value={beadsTouched} testid="walk-bar-beads" />
      <Sep />
      <Stat
        label="Spent"
        value={`$${dollars.toFixed(2)}`}
        testid="walk-bar-cost"
      />
      <Sep />
      <Stat label="Duration" value={fmtDuration(duration)} testid="walk-bar-duration" />
    </footer>
  );
}

function Stat({
  label,
  value,
  testid,
}: {
  label: string;
  value: string | number;
  testid: string;
}): JSX.Element {
  return (
    <span data-testid={testid} className="inline-flex items-baseline gap-1.5">
      <span className="text-neutral-500">{label}</span>
      <span className="font-semibold text-neutral-800 dark:text-neutral-200">{value}</span>
    </span>
  );
}

function Sep(): JSX.Element {
  return <span aria-hidden className="text-neutral-300 dark:text-neutral-700">·</span>;
}

// fmtDuration returns h/m/s display. <60s shows seconds; <60m shows
// "Xm Ys"; otherwise "Xh Ym". Pure for tests.
export function fmtDuration(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  if (totalSec < 60) return `${totalSec}s`;
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  const mm = m % 60;
  return `${h}h ${mm}m`;
}
