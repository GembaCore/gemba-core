import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { History } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useRhp } from './RhpContext';
import { useRhpPinnedContent } from './RhpPinnedContent';
import { getBeadsHistory, type BeadsHistoryEvent } from '@/api/beadsHistory';
import { useCapabilities } from '@/capabilities';
import { relativeTime } from '@/components/board/relativeTime';
import { cn } from '@/lib/utils';

export function BeadsHistoryBody() {
  const { beadsOnly } = useCapabilities();
  const { popDetail } = useRhp();
  const { data, isLoading, error } = useQuery({
    queryKey: ['beads-history'],
    queryFn: getBeadsHistory,
    enabled: beadsOnly,
    refetchInterval: beadsOnly ? 3000 : false,
  });

  if (!beadsOnly) {
    return (
      <div className="p-4 text-sm text-neutral-500" data-testid="rhp-beads-history-unavailable">
        Beads history is available in Beads-only mode.
      </div>
    );
  }

  const entries = data?.entries ?? [];
  return (
    <div className="min-h-full px-4 py-4" data-testid="rhp-beads-history-body">
      <header className="mb-4">
        <h2 className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">
          Beads history
        </h2>
        <p className="mt-1 text-xs text-neutral-500">
          Informational manifest of Beads changes from this Beads-only session.
        </p>
      </header>
      {data?.error ? (
        <div className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-100">
          {data.error}
        </div>
      ) : null}
      {data?.malformed ? (
        <div className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-100">
          {data.malformed} malformed manifest line{data.malformed === 1 ? '' : 's'} skipped.
        </div>
      ) : null}
      {isLoading ? (
        <Muted>Loading history...</Muted>
      ) : error ? (
        <Muted>Could not load Beads history.</Muted>
      ) : entries.length === 0 ? (
        <Muted>Beads history begins when you create, edit, or move a card.</Muted>
      ) : (
        <ol className="space-y-2">
          {entries.map((entry) => (
            <li key={entry.event_id}>
              <button
                type="button"
                data-testid="beads-history-entry"
                onClick={() => popDetail({ kind: 'workitem', id: entry.entity.id })}
                className={cn(
                  'w-full rounded-md border border-neutral-200 bg-white px-3 py-2 text-left',
                  'hover:bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950 dark:hover:bg-neutral-900'
                )}
              >
                <div className="flex items-start justify-between gap-2">
                  <span className="text-xs font-medium text-neutral-900 dark:text-neutral-100">
                    {plainEnglish(entry)}
                  </span>
                  <span className="shrink-0 text-[10px] text-neutral-500">
                    {relativeTime(entry.occurred_at)}
                  </span>
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-neutral-500">
                  <span className="rounded bg-neutral-100 px-1.5 py-0.5 dark:bg-neutral-800">
                    {entry.action}
                  </span>
                  <span className="font-mono">{entry.entity.id}</span>
                </div>
              </button>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

export function BeadsHistoryTab() {
  const { beadsOnly } = useCapabilities();
  const { registerPinnedTab } = useRhp();
  const { register } = useRhpPinnedContent();

  useEffect(() => {
    if (!beadsOnly) return;
    return registerPinnedTab({
      id: 'beads-history',
      icon: History,
      label: 'Beads history',
    });
  }, [beadsOnly, registerPinnedTab]);

  useEffect(() => {
    if (!beadsOnly) return;
    return register('beads-history', () => <BeadsHistoryBody />);
  }, [beadsOnly, register]);

  return null;
}

function plainEnglish(entry: BeadsHistoryEvent): string {
  if (entry.summary) return entry.summary;
  const title = entry.entity.title || entry.entity.id;
  if (entry.action.endsWith('.created')) return `Created ${entry.entity.type} "${title}".`;
  if (entry.action === 'work_item.state_changed') return `Moved "${title}".`;
  if (entry.action.endsWith('.edited')) return `Edited "${title}".`;
  return `${entry.action} on "${title}".`;
}

function Muted({ children }: { children: ReactNode }) {
  return <div className="text-xs text-neutral-500">{children}</div>;
}
