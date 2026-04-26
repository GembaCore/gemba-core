// Board pane (gm-root.6 / ui-spec §4). Default view is Epic-primary
// with swimlanes by parent-epic. Two alternate views ride the same
// URL state and drawer plumbing:
//
//   ?view=workitem  flat WorkItem kanban (M1.7a behaviour, Cmd-Shift-W)
//   ?view=list      flat WorkItem list   (former /backlog surface,
//                   Cmd-Shift-L; gm-e12.19.1)
//
// Presets layer on top via ?preset=<name> (ui-spec §4.11). The
// /backlog route is a permanent redirect to ?view=list&preset=backlog
// so the legacy URL keeps working.
//
// URL is the source of truth:
//   /board                              → Epic kanban, no drawer
//   /board?view=workitem                → flat WorkItem kanban
//   /board?view=list                    → flat WorkItem list
//   /board?view=list&preset=backlog     → list + Backlog preset
//   /board?bead=X                       → any view + WorkItemDrawer open on X
//   /board/:epicId                      → Epic kanban + EpicDrawer auto-open
//   /board/:epicId?bead=X               → Epic drawer + WorkItemDrawer stacked

import { useCallback, useMemo, useState } from 'react';
import {
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { LayoutGrid, List, ListChecks, Plus, RotateCcw } from 'lucide-react';
import { BoardColumn } from '@/components/board/BoardColumn';
import { BoardListView } from '@/components/board/BoardListView';
import { WorkItemDrawer } from '@/components/board/WorkItemDrawer';
import { EpicDrawer } from '@/components/board/EpicDrawer';
import { EpicView } from '@/components/board/EpicView';
import { NewWorkItemDialog } from '@/components/board/NewWorkItemDialog';
import {
  DEFAULT_SWIMLANE_MODE,
  parseSwimlaneMode,
  SWIMLANE_MODES,
  type SwimlaneMode,
} from '@/components/board/swimlaneMode';
import {
  BOARD_PRESETS,
  BOARD_PRESET_DEFAULT_VIEW,
  BOARD_PRESET_LABELS,
  isKnownPreset,
  type BoardPreset,
  type BoardView,
} from '@/lib/boardPresets';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useHotkey } from '@/hotkeys';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

// Spec wording per ui-spec §4.3 (Backlog → Next Up → Staged → In
// Progress → Done → Canceled). The internal core enum keeps its
// camel-but-not-quite names; only the labels match the spec.
const COLUMN_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

function viewFromQuery(p: URLSearchParams, preset: BoardPreset | null): BoardView {
  const v = p.get('view');
  if (v === 'workitem') return 'workitem';
  if (v === 'list') return 'list';
  if (v === 'epic') return 'epic';
  // No explicit ?view=: fall back to the active preset's preferred
  // view so /board?preset=backlog lands on list mode without the
  // operator having to spell it out. Default-default = epic.
  if (preset) return BOARD_PRESET_DEFAULT_VIEW[preset];
  return 'epic';
}

function statesFromQuery(p: URLSearchParams): StateCategory[] {
  const all = p.getAll('state_category');
  return all.filter((s): s is StateCategory =>
    (STATE_CATEGORIES as readonly string[]).includes(s)
  );
}

function kindsFromQuery(p: URLSearchParams): string[] {
  return p.getAll('kind').filter((s) => s.length > 0);
}

function groupByStateCategory(items: WorkItem[]): Record<StateCategory, WorkItem[]> {
  const out: Record<StateCategory, WorkItem[]> = {
    backlog: [],
    unstarted: [],
    staged: [],
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
  const preset = isKnownPreset(params.get('preset'));
  const view = viewFromQuery(params, preset);
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
      // Drop ?view= when the choice matches the current default
      // (epic, or whatever the active preset prefers) so the URL
      // stays clean for the common case.
      const defaultView = preset ? BOARD_PRESET_DEFAULT_VIEW[preset] : 'epic';
      if (next === defaultView) p.delete('view');
      else p.set('view', next);
      setParams(p, { replace: true });
    },
    [params, setParams, preset]
  );

  const setPreset = useCallback(
    (next: BoardPreset | null) => {
      const p = new URLSearchParams(params);
      if (next === null) p.delete('preset');
      else p.set('preset', next);
      // Switching presets clears any explicit view override and
      // explicit chip selections so the new preset's defaults take
      // hold cleanly. The bead drawer (?bead=) is preserved.
      p.delete('view');
      p.delete('state_category');
      p.delete('kind');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  // List-mode chip + search state lives entirely in the URL — no
  // localStorage layer (the old BacklogPage's hash+storage hybrid is
  // gone). Search uses replace:true so typing doesn't pollute history.
  const listStates = useMemo(() => statesFromQuery(params), [params]);
  const listKinds = useMemo(() => kindsFromQuery(params), [params]);
  const listSearch = params.get('q') ?? '';
  const setListStates = useCallback(
    (next: StateCategory[]) => {
      const p = new URLSearchParams(params);
      p.delete('state_category');
      for (const s of next) p.append('state_category', s);
      setParams(p, { replace: true });
    },
    [params, setParams]
  );
  const setListKinds = useCallback(
    (next: string[]) => {
      const p = new URLSearchParams(params);
      p.delete('kind');
      for (const k of next) p.append('kind', k);
      setParams(p, { replace: true });
    },
    [params, setParams]
  );
  const setListSearch = useCallback(
    (next: string) => {
      const p = new URLSearchParams(params);
      if (next) p.set('q', next);
      else p.delete('q');
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
  // Cmd-W toggles the kanban granularity (epic ↔ workitem). It does
  // not pivot through list — the list/kanban swap is its own hotkey
  // (Cmd-Shift-L) so the two axes stay independent.
  const toggleView = useCallback(
    () => setView(view === 'epic' ? 'workitem' : 'epic'),
    [setView, view]
  );
  useHotkey('view-toggle-board', toggleView);

  const toggleListMode = useCallback(() => {
    if (view === 'list') {
      // Returning from list → kanban: prefer epic (the global
      // default) unless the active preset prefers workitem.
      setView(preset ? BOARD_PRESET_DEFAULT_VIEW[preset] : 'epic');
    } else {
      setView('list');
    }
  }, [setView, view, preset]);
  useHotkey('view-toggle-list', toggleListMode);

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

  const [newItemOpen, setNewItemOpen] = useState(false);

  // List view runs its own filtered query; the kanban-level loading
  // and error gates only apply to the kanban renderers.
  if (view !== 'list' && isLoading) return <SkeletonBoard />;
  if (view !== 'list' && isError)
    return <ErrorState message={error?.message ?? 'Unknown error.'} onRetry={() => void refetch()} />;

  return (
    <>
      <BoardHeader
        view={view}
        onChangeView={setView}
        swimlane={swimlane}
        onChangeSwimlane={setSwimlane}
        preset={preset}
        onChangePreset={setPreset}
        onNewWorkItem={() => setNewItemOpen(true)}
      />
      {view === 'list' ? (
        <BoardListView
          preset={preset}
          stateCategories={listStates}
          kinds={listKinds}
          search={listSearch}
          onChangeStateCategories={setListStates}
          onChangeKinds={setListKinds}
          onChangeSearch={setListSearch}
          onSelectWorkItem={setOpenWorkItemId}
        />
      ) : !data || data.length === 0 ? (
        <EmptyState onCreate={() => setNewItemOpen(true)} />
      ) : view === 'epic' ? (
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
      <NewWorkItemDialog
        open={newItemOpen}
        onClose={() => setNewItemOpen(false)}
        onCreated={(item) => setOpenWorkItemId(item.id)}
      />
    </>
  );
}

// BoardHeader holds the view picker — three view buttons (Epic /
// Item / List), the swimlane partition selector (kanban-only), and
// the preset chip rail (ui-spec §4.11).
interface BoardHeaderProps {
  view: BoardView;
  onChangeView: (v: BoardView) => void;
  swimlane: SwimlaneMode;
  onChangeSwimlane: (s: SwimlaneMode) => void;
  preset: BoardPreset | null;
  onChangePreset: (p: BoardPreset | null) => void;
  onNewWorkItem: () => void;
}
function BoardHeader({
  view,
  onChangeView,
  swimlane,
  onChangeSwimlane,
  preset,
  onChangePreset,
  onNewWorkItem,
}: BoardHeaderProps) {
  return (
    <div
      data-testid="board-view-toggle"
      className="flex flex-wrap items-center gap-3 border-b border-neutral-200 bg-white/50 px-4 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950/50"
    >
      {view === 'epic' ? (
        <SwimlaneSwitcher value={swimlane} onChange={onChangeSwimlane} />
      ) : null}
      <PresetSwitcher value={preset} onChange={onChangePreset} />
      <div className="ml-auto flex items-center gap-1">
        <button
          type="button"
          data-testid="board-new-workitem"
          onClick={onNewWorkItem}
          className="inline-flex items-center gap-1 rounded bg-neutral-900 px-2 py-1 text-xs text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          <Plus className="h-3 w-3" />
          New
        </button>
        <div className="mx-1 h-4 w-px bg-neutral-200 dark:bg-neutral-800" />
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
        <ToggleButton
          active={view === 'list'}
          onClick={() => onChangeView('list')}
          label="List"
          icon={<List className="h-3 w-3" />}
          testid="view-toggle-list"
        />
      </div>
    </div>
  );
}

interface PresetSwitcherProps {
  value: BoardPreset | null;
  onChange: (p: BoardPreset | null) => void;
}
function PresetSwitcher({ value, onChange }: PresetSwitcherProps) {
  return (
    <div className="flex items-center gap-1" data-testid="board-preset-switcher">
      <span className="text-neutral-500">Preset</span>
      {BOARD_PRESETS.map((p) => (
        <button
          key={p}
          type="button"
          data-testid={`board-preset-${p}`}
          data-active={value === p || undefined}
          onClick={() => onChange(value === p ? null : p)}
          className={cn(
            'rounded border px-2 py-0.5 text-xs transition-colors',
            value === p
              ? 'border-sky-700 bg-sky-700 text-white'
              : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
          )}
        >
          {BOARD_PRESET_LABELS[p]}
        </button>
      ))}
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

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div
      data-testid="board-empty"
      className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center"
    >
      <p className="text-sm text-neutral-600 dark:text-neutral-300">No beads yet.</p>
      <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">
        Create one below, or use <code className="font-mono">bd create</code> from your terminal.
      </p>
      <button
        type="button"
        onClick={onCreate}
        className="mt-1 inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
      >
        <Plus className="h-3.5 w-3.5" />
        New work item
      </button>
    </div>
  );
}
