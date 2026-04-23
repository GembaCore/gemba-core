// Board pane (M1.7a / gm-922). Read-only: 5 StateCategory columns, rich
// BeadCard per M1.7d. Drag, filtering, and drill-in land in later beads.

import { RotateCcw } from 'lucide-react';
import { BoardColumn } from '@/components/board/BoardColumn';
import { useBeads } from '@/hooks/useBeads';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

const COLUMN_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Unstarted',
  started: 'Started',
  completed: 'Completed',
  canceled: 'Canceled',
};

function groupByStateCategory(items: WorkItem[]): Record<StateCategory, WorkItem[]> {
  const out: Record<StateCategory, WorkItem[]> = {
    backlog: [],
    unstarted: [],
    started: [],
    completed: [],
    canceled: [],
  };
  for (const it of items) {
    out[it.state_category]?.push(it);
  }
  return out;
}

function SkeletonCard() {
  return (
    <div
      data-testid="board-skeleton-card"
      className={cn(
        'animate-pulse rounded-md border border-neutral-200 bg-white p-3 shadow-sm',
        'dark:border-neutral-800 dark:bg-neutral-900'
      )}
    >
      <div className="mb-2 flex items-center gap-2">
        <div className="h-2 w-2 rounded-full bg-neutral-200 dark:bg-neutral-800" />
        <div className="h-3 w-12 rounded bg-neutral-200 dark:bg-neutral-800" />
        <div className="ml-auto h-3 w-8 rounded bg-neutral-200 dark:bg-neutral-800" />
      </div>
      <div className="mb-1 h-3.5 w-4/5 rounded bg-neutral-200 dark:bg-neutral-800" />
      <div className="h-3.5 w-3/5 rounded bg-neutral-200 dark:bg-neutral-800" />
    </div>
  );
}

function SkeletonBoard() {
  return (
    <div className="flex h-full gap-3 p-4" data-testid="board-loading">
      {STATE_CATEGORIES.map((cat) => (
        <section
          key={cat}
          className="flex h-full min-w-[18rem] flex-1 flex-col rounded-md bg-neutral-50 dark:bg-neutral-950"
        >
          <header className="border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
            <h2 className="text-xs font-semibold uppercase tracking-wide text-neutral-400 dark:text-neutral-600">
              {COLUMN_LABELS[cat]}
            </h2>
          </header>
          <div className="flex-1 space-y-2 p-2">
            <SkeletonCard />
            <SkeletonCard />
          </div>
        </section>
      ))}
    </div>
  );
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div
      data-testid="board-error"
      className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center"
    >
      <p className="text-sm text-rose-600 dark:text-rose-400">Could not load beads.</p>
      <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">{message}</p>
      <button
        type="button"
        onClick={onRetry}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border border-neutral-300 bg-white px-3 py-1.5 text-sm',
          'text-neutral-700 hover:bg-neutral-50',
          'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800'
        )}
      >
        <RotateCcw className="h-3.5 w-3.5" />
        Retry
      </button>
    </div>
  );
}

function EmptyState() {
  return (
    <div
      data-testid="board-empty"
      className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center"
    >
      <p className="text-sm text-neutral-600 dark:text-neutral-300">No beads yet.</p>
      <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">
        Create a bead with <code className="font-mono">bd create</code> or point{' '}
        <code className="font-mono">gemba serve</code> at a populated beads database.
      </p>
    </div>
  );
}

export function BoardPage() {
  const { data, isLoading, isError, error, refetch } = useBeads();

  if (isLoading) return <SkeletonBoard />;
  if (isError) return <ErrorState message={error?.message ?? 'Unknown error.'} onRetry={() => void refetch()} />;
  if (!data || data.length === 0) return <EmptyState />;

  const groups = groupByStateCategory(data);

  return (
    <div data-testid="board" className="flex h-full gap-3 overflow-x-auto p-4">
      {STATE_CATEGORIES.map((cat) => (
        <BoardColumn
          key={cat}
          category={cat}
          label={COLUMN_LABELS[cat]}
          items={groups[cat]}
        />
      ))}
    </div>
  );
}
