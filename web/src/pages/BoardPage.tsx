// Board pane (gm-root.6 / ui-spec §4). Default view is Epic-primary
// with swimlanes by parent-epic. The flat WorkItem variant from M1
// remains accessible via ?view=workitem (and Cmd-Shift-W) — that's
// the spec's "alternate" view.
//
// URL is the source of truth:
//   /board                       → Epic view, no drawer
//   /board?view=workitem         → flat WorkItem view (M1.7a behaviour)
//   /board?bead=X                → any view + WorkItemDrawer open on X
//                                  (drawer deep link — gm-e12.5)
//   /board/:epicId               → Epic view + EpicDrawer auto-open
//   /board/:epicId?bead=X        → Epic drawer + WorkItemDrawer stacked
//                                  (child drill-down, deep-linkable)

import { useCallback, useMemo } from 'react';
import {
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { LayoutGrid, ListChecks, RotateCcw } from 'lucide-react';
import { BoardColumn } from '@/components/board/BoardColumn';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { EpicDrawer } from '@/components/board/EpicDrawer';
import { EpicView } from '@/components/board/EpicView';
import {
  DEFAULT_SWIMLANE_MODE,
  parseSwimlaneMode,
  SWIMLANE_MODES,
  type SwimlaneMode,
} from '@/components/board/swimlaneMode';
import { useWorkItems } from '@/hooks/useWorkItems';
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
  const { data, isLoading, isError, error, refetch } = useWorkItems();
  const [params, setParams] = useSearchParams();
  const view = viewFromQuery(params);
  // The route is /board/* so the matched suffix lands under the splat
  // param. bd ids carry slashes ("gemba/gemba/gm-e1"); a :epicId
  // segment would only catch the first chunk.
  const splatParams = useParams();
  const splatRaw = splatParams['*'] ?? '';
  const epicId = splatRaw.length > 0 ? splatRaw : null;
  const navigate = useNavigate();

  // Both drawers are URL-routed now (gm-e12.5 DoD: "Opens from grid,
  // board, palette, deep link"). Epic id lives in the path; work-item
  // id lives in ?bead=X so an Epic + WorkItem drawer can both be open
  // (drill-down from an Epic card → child) and the pair is deep-linkable.
  const openWorkItemId = params.get('bead');
  const setOpenWorkItemId = useCallback(
    (id: string | null) => {
      const p = new URLSearchParams(params);
      if (id) p.set('bead', id);
      else p.delete('bead');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  const setView = useCallback(
    (next: BoardView) => {
      const p = new URLSearchParams(params);
      if (next === 'workitem') p.set('view', 'workitem');
      else p.delete('view');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  // Swimlane partition (ui-spec §4.4). URL is the source of truth so
  // the operator's selection survives reloads + deep-links and a future
  // workspace-switcher can clobber it without a stale-state hazard.
  const swimlane = parseSwimlaneMode(params.get('swimlane'));
  const setSwimlane = useCallback(
    (next: SwimlaneMode) => {
      const p = new URLSearchParams(params);
      if (next === DEFAULT_SWIMLANE_MODE) p.delete('swimlane');
      else p.set('swimlane', next);
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
      <BoardHeader
        view={view}
        onChangeView={setView}
        swimlane={swimlane}
        onChangeSwimlane={setSwimlane}
      />
      {view === 'epic' ? (
        <EpicView items={data} onSelectEpic={openEpic} mode={swimlane} />
      ) : (
        <WorkItemBoard data={data} onSelectWorkItem={setOpenWorkItemId} />
      )}
      <EpicDrawer
        openId={epicId ?? null}
        onClose={closeEpic}
        onOpenChild={(id) => setOpenWorkItemId(id)}
      />
      <WorkItemDrawer openId={openWorkItemId} onClose={() => setOpenWorkItemId(null)} />
    </>
  );
}

// BoardHeader holds the view picker — both the cards-as-X toggle
// (Epics / Work items) and the swimlane partition selector
// (by-parent-epic / by-label / none). Swimlane control is hidden in
// the WorkItem view because that variant isn't swimlaned.
interface BoardHeaderProps {
  view: BoardView;
  onChangeView: (v: BoardView) => void;
  swimlane: SwimlaneMode;
  onChangeSwimlane: (s: SwimlaneMode) => void;
}
function BoardHeader({ view, onChangeView, swimlane, onChangeSwimlane }: BoardHeaderProps) {
  return (
    <div
      data-testid="board-view-toggle"
      className="flex items-center gap-3 border-b border-neutral-200 bg-white/50 px-4 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950/50"
    >
      {view === 'epic' ? (
        <SwimlaneSwitcher value={swimlane} onChange={onChangeSwimlane} />
      ) : null}
      <div className="ml-auto flex items-center gap-1">
        <ToggleButton
          active={view === 'epic'}
          onClick={() => onChangeView('epic')}
          label="Epic"
          icon={<LayoutGrid className="h-3 w-3" />}
          testid="view-toggle-epic"
        />
        <ToggleButton
          active={view === 'workitem'}
          onClick={() => onChangeView('workitem')}
          label="Item"
          icon={<ListChecks className="h-3 w-3" />}
          testid="view-toggle-workitem"
        />
      </div>
    </div>
  );
}

const SWIMLANE_LABELS: Record<SwimlaneMode, string> = {
  'by-parent-epic': 'Parent epic',
  'by-label': 'Label',
  'none': 'None',
};

interface SwimlaneSwitcherProps {
  value: SwimlaneMode;
  onChange: (v: SwimlaneMode) => void;
}
function SwimlaneSwitcher({ value, onChange }: SwimlaneSwitcherProps) {
  return (
    <label className="inline-flex items-center gap-1" data-testid="swimlane-switcher">
      <span className="text-neutral-500">Swimlane</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value as SwimlaneMode)}
        className="rounded border border-neutral-300 bg-white px-1.5 py-0.5 text-xs font-mono dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
      >
        {SWIMLANE_MODES.map((m) => (
          <option key={m} value={m}>
            {SWIMLANE_LABELS[m]}
          </option>
        ))}
      </select>
    </label>
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
