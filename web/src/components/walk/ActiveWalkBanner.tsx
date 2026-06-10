// Active-walk banner (gm-uipx.2). Yellow-tinted strip at the top of
// the content area when a walk is active. Per ui-spec §5.4:
// "Gemba walk active · 4 of 11 decided · [resume / end]".
//
// Mounted inside AppShell so it shows on every route during a
// walk — the walk takes over the operator's attention everywhere,
// not just on /walk.

import { Footprints } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { decidedCount, totalDecidableCount, useWalk } from './WalkContext';

export function ActiveWalkBanner(): JSX.Element | null {
  const walk = useWalk();
  const navigate = useNavigate();
  if (!walk.active) return null;
  const done = decidedCount(walk.agenda);
  const total = totalDecidableCount(walk.agenda);
  return (
    <div
      role="status"
      data-testid="walk-banner"
      className="flex items-center gap-3 border-b border-amber-300 bg-amber-100 px-3 py-1.5 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-950/60 dark:text-amber-200"
    >
      <Footprints className="h-3.5 w-3.5" aria-hidden />
      <span className="font-semibold">Gemba walk active</span>
      <span className="text-amber-800 dark:text-amber-300/80">
        · {done} of {total} decided
      </span>
      <div className="ml-auto flex items-center gap-2">
        <button
          type="button"
          data-testid="walk-banner-resume"
          onClick={() => navigate('/walk')}
          className="rounded border border-amber-400 bg-amber-50 px-2 py-0.5 text-[11px] hover:bg-amber-200 dark:border-amber-700 dark:bg-amber-900 dark:hover:bg-amber-800"
        >
          Resume
        </button>
        <button
          type="button"
          data-testid="walk-banner-end"
          onClick={() => walk.end()}
          className="rounded border border-amber-400 bg-amber-50 px-2 py-0.5 text-[11px] hover:bg-amber-200 dark:border-amber-700 dark:bg-amber-900 dark:hover:bg-amber-800"
        >
          End walk
        </button>
      </div>
    </div>
  );
}
