// SessionsPage (gm-native.15). Live inventory of every session the
// bound OrchestrationPlane tracks: one row per pane, End button per
// row, "New session" dispatcher. Polls /api/sessions every 5s until
// gm-native.11's SSE feed replaces polling with push-invalidation.
//
// Columns follow the bead's spec: worktree, agent, current assignment
// (bead id + optional title), last event, open-escalation count, End.

import { useMemo, useState } from 'react';
import { Play, StopCircle } from 'lucide-react';
import { useSessions, useEndSession } from '@/hooks/useSessions';
import { NewSessionDialog } from '@/components/sessions/NewSessionDialog';
import type { Session } from '@/api/sessions';

export function SessionsPage() {
  const [dialogOpen, setDialogOpen] = useState(false);
  const { data: sessions = [], isLoading, error } = useSessions();
  const endMut = useEndSession();

  // Sort: non-terminal first (newest started first), then terminal
  // (most recently ended first). Operator attention is on live panes.
  const rows = useMemo(() => {
    return [...sessions].sort((a, b) => {
      const aTerm = isTerminal(a);
      const bTerm = isTerminal(b);
      if (aTerm !== bTerm) return aTerm ? 1 : -1;
      return (b.started_at ?? '').localeCompare(a.started_at ?? '');
    });
  }, [sessions]);

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Sessions</h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Live panes from every registered orchestration plane.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setDialogOpen(true)}
          data-hotkey-target="new-session"
          className="inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          <Play className="h-3.5 w-3.5" aria-hidden />
          New session
        </button>
      </header>

      <div className="flex-1 overflow-auto">
        {error && (
          <div className="m-8 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
            {error.message}
          </div>
        )}
        {isLoading && <EmptyState message="Loading…" />}
        {!isLoading && rows.length === 0 && (
          <EmptyState message="No live sessions. Hit “New session” to dispatch one." />
        )}
        {rows.length > 0 && (
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-neutral-50 text-left text-xs uppercase tracking-wide text-neutral-500 dark:bg-neutral-900 dark:text-neutral-400">
              <tr>
                <Th>Bead</Th>
                <Th>Agent</Th>
                <Th>Status</Th>
                <Th>Worktree</Th>
                <Th>Started</Th>
                <Th></Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <Row key={s.id} s={s} onEnd={() => endMut.mutate({ id: s.id })} ending={endMut.isPending} />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <NewSessionDialog open={dialogOpen} onClose={() => setDialogOpen(false)} />
    </div>
  );
}

function Row({ s, onEnd, ending }: { s: Session; onEnd: () => void; ending: boolean }) {
  const terminal = isTerminal(s);
  const worktree = typeof s.provider_metadata?.worktree === 'string' ? s.provider_metadata.worktree : '';
  const beadID = typeof s.provider_metadata?.bead_id === 'string' ? s.provider_metadata.bead_id : s.assignment_id;
  const agent = typeof s.provider_metadata?.agent_type === 'string' ? s.provider_metadata.agent_type : s.agent_id;
  return (
    <tr className="border-b border-neutral-100 hover:bg-neutral-50 dark:border-neutral-900 dark:hover:bg-neutral-900/50">
      <Td>
        <span className="font-mono text-xs">{beadID}</span>
      </Td>
      <Td>{agent || '—'}</Td>
      <Td>
        <span
          className={
            'rounded-md px-2 py-0.5 text-xs ' +
            (terminal
              ? 'bg-neutral-100 text-neutral-500 dark:bg-neutral-900 dark:text-neutral-500'
              : s.status === 'working'
                ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400'
                : s.status === 'prompting'
                  ? 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400'
                  : 'bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-400')
          }
        >
          {s.status}
        </span>
      </Td>
      <Td>
        <span className="truncate font-mono text-xs text-neutral-500">{worktree}</span>
      </Td>
      <Td>{formatTime(s.started_at)}</Td>
      <Td>
        {!terminal && (
          <button
            type="button"
            onClick={onEnd}
            disabled={ending}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-50 dark:text-red-400 dark:hover:bg-red-950"
          >
            <StopCircle className="h-3.5 w-3.5" aria-hidden />
            End
          </button>
        )}
      </Td>
    </tr>
  );
}

function Th({ children }: { children?: React.ReactNode }) {
  return <th className="px-6 py-2 font-medium">{children}</th>;
}
function Td({ children }: { children?: React.ReactNode }) {
  return <td className="px-6 py-2 align-middle">{children}</td>;
}
function EmptyState({ message }: { message: string }) {
  return (
    <div className="p-12 text-center text-sm text-neutral-500 dark:text-neutral-400">{message}</div>
  );
}

function isTerminal(s: Session): boolean {
  return s.status === 'completed' || s.status === 'failed';
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleTimeString();
  } catch {
    return iso;
  }
}
