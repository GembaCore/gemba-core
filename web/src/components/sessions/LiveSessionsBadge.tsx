// LiveSessionsBadge (gm-root.26 item 2). Topbar pill showing the
// number of non-terminal sessions, click-through to /sessions. The
// session runs in a separate pane window so first-time users may not
// know /sessions exists; the badge is a permanent breadcrumb whenever
// at least one session is live.
//
// Hidden when count <= 0. Driven by useSessions (which is invalidated
// by SSE on session.transition / state_reported per gm-e12.2), so the
// count refreshes in real time without dedicated wiring.

import { Link } from 'react-router-dom';
import { Activity } from 'lucide-react';
import { useSessions } from '@/hooks/useSessions';
import type { Session } from '@/api/sessions';
import type { SessionStatus } from '@/types/core.gen';

const TERMINAL_STATUSES: ReadonlySet<SessionStatus> = new Set([
  'completed',
  'failed',
]);

export function isLive(s: Session): boolean {
  return !TERMINAL_STATUSES.has(s.status);
}

export interface LiveSessionsBadgeProps {
  testid?: string;
}

export function LiveSessionsBadge({
  testid = 'live-sessions-badge',
}: LiveSessionsBadgeProps) {
  // Tolerate include_terminal default (false) on the wire; we still
  // filter locally so the count stays correct if a future server
  // tweak surfaces terminal entries in the default list.
  const { data } = useSessions();
  const count = (data ?? []).filter(isLive).length;
  if (count <= 0) return null;
  return (
    <Link
      to="/sessions"
      data-testid={testid}
      data-count={count}
      aria-label={`${count} live session${count === 1 ? '' : 's'}`}
      title="Click to view /sessions"
      className="inline-flex items-center gap-1 rounded-md border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700 hover:bg-emerald-100 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300 dark:hover:bg-emerald-900"
    >
      <Activity className="h-3 w-3" aria-hidden />
      <span className="font-mono tabular-nums">{count}</span>
      <span className="opacity-70">live</span>
    </Link>
  );
}
