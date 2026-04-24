// Board pane (gm-root.6 / ui-spec §4). Default view is Epic-primary
// with swimlanes by parent-epic. The flat WorkItem variant from M1
// remains accessible via ?view=workitem (and Cmd-Shift-W) — that's
// the spec's "alternate" view.
//
// URL is the source of truth:
//   /board                       → Epic view, no drawer
//   /board?view=workitem         → flat WorkItem view (M1.7a behaviour)
//   /board/:epicId               → Epic view + EpicDrawer auto-open
//   /board/:epicId?bead=X        → reserved for future deep-links into
//                                  a child WorkItem from an Epic context

import { useCallback, useMemo, useState } from 'react';
import {
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { LayoutGrid, ListChecks, RotateCcw } from 'lucide-react';
import { BoardColumn } from '@/components/board/BoardColumn';
import { BeadDrawer } from '@/components/board/BeadDrawer';
import { EpicDrawer } from '@/components/board/EpicDrawer';
import { EpicView } from '@/components/board/EpicView';
import { useBeads } from '@/hooks/useBeads';
import { useHotkey } from '@/hotkeys';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

const COLUMN_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Unstarted',
  started: 'Started',
  completed: 'Completed',
  canceled: 'Canceled',
};

type BoardView = 'epic' | 'workitem';

function viewFromQuery(p: URLSearchParams): BoardView {
  return p.get('view') === 'workitem' ? 'workitem' : 'epic';
}

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

export function BoardPage() {
  const { data, isLoading, isError, error, refetch } = useBeads();
  const [params, setParams] = useSearchParams();
  const view = viewFromQuery(params);
  // The route is /board/* so the matched suffix lands under the splat
  // param. bd ids carry slashes ("gemba/gemba/gm-e1"); a :epicId
  // segment would only catch the first chunk.
  const splatParams = useParams();
  const splatRaw = splatParams['*'] ?? '';
  const epicId = splatRaw.length > 0 ? splatRaw : null;
  const navigate = useNavigate();

  // workitem-view drawer is local SPA state; epic-view drawer is
  // URL-routed (/board/:epicId) per spec L116.
  const [openWorkItemId, setOpenWorkItemId] = useState<string | null>(null);

  const setView = useCallback(
    (next: BoardView) => {
      const p = new URLSearchParams(params);
      if (next === 'workitem') p.set('view', 'workitem');
      else p.delete('view');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );
  const toggleView = useCallback(
    () => setView(view === 'epic' ? 'workitem' : 'epic'),
    [setView, view]
  );
  useHotkey('view-toggle-board', toggleView);

  const openEpic = useCallback(
    (id: string) => {
      const search = params.toString();
      navigate({ pathname: `/board/${id}`, search: search ? `?${search}` : '' });
    },
    [navigate, params]
  );
  const closeEpic = useCallback(() => {
    const search = params.toString();
    navigate({ pathname: '/board', search: search ? `?${search}` : '' });
  }, [navigate, params]);

  if (isLoading) return <SkeletonBoard />;
  if (isError)
    return <ErrorState message={error?.message ?? 'Unknown error.'} onRetry={() => void refetch()} />;
  if (!data || data.length === 0) return <EmptyState />;

  return (
    <>
      <ViewToggle view={view} onChange={setView} />
      {view === 'epic' ? (
        <EpicView items={data} onSelectEpic={openEpic} />
      ) : (
        <WorkItemBoard data={data} onSelectWorkItem={setOpenWorkItemId} />
      )}
      <EpicDrawer
        openId={epicId ?? null}
        onClose={closeEpic}
        onOpenChild={(id) => setOpenWorkItemId(id)}
      />
      <BeadDrawer openId={openWorkItemId} onClose={() => setOpenWorkItemId(null)} />
    </>
  );
}

interface ViewToggleProps {
  view: BoardView;
  onChange: (v: BoardView) => void;
}
function ViewToggle({ view, onChange }: ViewToggleProps) {
  return (
    <div
      data-testid="board-view-toggle"
      className="flex items-center justify-end gap-1 border-b border-neutral-200 bg-white/50 px-4 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950/50"
    >
      <ToggleButton
        active={view === 'epic'}
        onClick={() => onChange('epic')}
        label="Epics"
        icon={<LayoutGrid className="h-3 w-3" />}
        testid="view-toggle-epic"
      />
      <ToggleButton
        active={view === 'workitem'}
        onClick={() => onChange('workitem')}
        label="Work items"
        icon={<ListChecks className="h-3 w-3" />}
        testid="view-toggle-workitem"
      />
    </div>
  );
}

interface ToggleButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  icon: React.ReactNode;
  testid: string;
}
function ToggleButton({ active, onClick, label, icon, testid }: ToggleButtonProps) {
  return (
    <button
      type="button"
      data-testid={testid}
      data-active={active}
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1 rounded px-2 py-1 text-xs',
        active
          ? 'bg-neutral-200 text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100'
          : 'text-neutral-500 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-900'
      )}
    >
      {icon}
      {label}
    </button>
  );
}

interface WorkItemBoardProps {
  data: WorkItem[];
  onSelectWorkItem: (id: string | null) => void;
}
function WorkItemBoard({ data, onSelectWorkItem }: WorkItemBoardProps) {
  const groups = useMemo(() => groupByStateCategory(data), [data]);
  return (
    <div data-testid="board-workitem" className="flex h-full gap-3 overflow-x-auto p-4">
      {STATE_CATEGORIES.map((cat) => (
        <BoardColumn
          key={cat}
          category={cat}
          label={COLUMN_LABELS[cat]}
          items={groups[cat]}
          onSelect={onSelectWorkItem}
        />
      ))}
    </div>
  );
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
