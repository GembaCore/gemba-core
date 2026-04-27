// Board pane (gm-root.6 / ui-spec §4). Default layout is Epic-primary
// kanban with swimlanes by parent-epic. Three layouts share the same
// URL state and drawer plumbing:
//
//   ?layout=epic      Epic kanban with swimlanes (default)
//   ?layout=workitem  flat WorkItem kanban (Cmd-W toggle)
//   ?layout=list      flat WorkItem list   (former /backlog surface,
//                     Cmd-Shift-L; gm-e12.19.1)
//
// Named views layer on top via ?view=<name> (gm-uipx.18). The legacy
// ?preset= and ?view=epic|workitem|list shapes are migrated on
// first paint by migrateLegacyParams; existing bookmarks resolve.
//
// URL is the source of truth:
//   /board                              → Epic kanban, no drawer
//   /board?layout=workitem              → flat WorkItem kanban
//   /board?layout=list                  → flat WorkItem list
//   /board?layout=list&view=backlog     → list + Backlog named view
//   /board?bead=X                       → any layout + WorkItemDrawer on X
//   /board/:epicId                      → Epic kanban + EpicDrawer auto-open
//   /board/:epicId?bead=X               → Epic drawer + WorkItemDrawer stacked

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { LayoutGrid, List, ListChecks, Plus, RotateCcw, Zap } from 'lucide-react';
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
  findView,
  LAYOUT_PARAM,
  LEGACY_FROM_LAYOUT,
  migrateLegacyParams,
  VIEW_PARAM,
  WORK_ITEM_VIEWS,
  type LegacyBoardView,
  type WorkItemView,
} from '@/lib/workItemViews';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useHotkey } from '@/hotkeys';
import { STATE_CATEGORIES, type StateCategory, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

// LayoutMode aliases the LegacyBoardView union — Board's three
// kanban+list layouts. Renaming the type-local alias keeps the page
// vocabulary current ("layout") while the cross-package type stays
// pinned to the LegacyBoardView name for back-compat with future
// callers.
type LayoutMode = LegacyBoardView;

// Power-mode persistence (gm-uipx.17). The URL is the source of
// truth (?power=1) so a deep-link or bookmark lands directly in
// power mode; the localStorage entry is a fallback used when the
// operator opens /board?layout=list with no explicit ?power= param,
// so their last-used preference sticks across sessions.
const POWER_PARAM = 'power';
const POWER_STORAGE_KEY = 'gemba.board.power';

function readStoredPower(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(POWER_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

function writeStoredPower(on: boolean): void {
  if (typeof window === 'undefined') return;
  try {
    if (on) window.localStorage.setItem(POWER_STORAGE_KEY, '1');
    else window.localStorage.removeItem(POWER_STORAGE_KEY);
  } catch {
    /* storage unavailable (private mode); URL param still works */
  }
}

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

function layoutFromQuery(p: URLSearchParams, view: WorkItemView | null): LayoutMode {
  const v = p.get(LAYOUT_PARAM);
  if (v === 'workitem' || v === 'list' || v === 'epic') return v;
  // No explicit ?layout=: fall back to the active named view's
  // preferred default so /board?view=backlog lands on list mode
  // without the operator having to spell it out. Default-default = epic.
  if (view) return LEGACY_FROM_LAYOUT[view.defaultLayout];
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
  const [rawParams, setParams] = useSearchParams();

  // First-paint migration: rewrite legacy URL shapes (?preset=X,
  // ?view=epic|workitem|list) into the canonical (?view=<named>,
  // ?layout=<mode>) so the rest of this render reads from the
  // unified vocabulary. The migrated params drive the page even
  // before setParams flushes the new URL — that way a deep-link
  // to a legacy URL renders the right page on first paint
  // instead of flashing the wrong layout.
  const params = useMemo(() => {
    const next = new URLSearchParams(rawParams);
    return migrateLegacyParams(next) ? next : rawParams;
  }, [rawParams]);
  useEffect(() => {
    if (params !== rawParams) {
      setParams(params, { replace: true });
    }
  }, [params, rawParams, setParams]);

  const view = findView(params.get(VIEW_PARAM));
  const layout = layoutFromQuery(params, view);

  // Power-mode resolution. Explicit ?power=1 / ?power=0 in the URL
  // wins; absent that, fall back to the last-used localStorage value
  // so an operator's preference survives a refresh. Anything else
  // (e.g. ?power=foo) is treated as off.
  const power = useMemo(() => {
    const raw = params.get(POWER_PARAM);
    if (raw === '1') return true;
    if (raw === '0') return false;
    return readStoredPower();
  }, [params]);
  const setPower = useCallback(
    (next: boolean) => {
      const p = new URLSearchParams(params);
      if (next) p.set(POWER_PARAM, '1');
      else p.delete(POWER_PARAM);
      writeStoredPower(next);
      setParams(p, { replace: true });
    },
    [params, setParams]
  );
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

  const setLayout = useCallback(
    (next: LayoutMode) => {
      const p = new URLSearchParams(params);
      // Drop ?layout= when the choice matches the current default
      // (epic, or whatever the active named view prefers) so the
      // URL stays clean for the common case.
      const defaultLayout = view
        ? LEGACY_FROM_LAYOUT[view.defaultLayout]
        : ('epic' as LayoutMode);
      if (next === defaultLayout) p.delete(LAYOUT_PARAM);
      else p.set(LAYOUT_PARAM, next);
      setParams(p, { replace: true });
    },
    [params, setParams, view]
  );

  const setView = useCallback(
    (nextId: string | null) => {
      const p = new URLSearchParams(params);
      if (nextId === null) p.delete(VIEW_PARAM);
      else p.set(VIEW_PARAM, nextId);
      // Switching named views clears explicit chip selections so the
      // new view's defaults take hold cleanly. The explicit
      // ?layout= is preserved — an operator who deep-linked or
      // toggled into a specific layout (e.g. list+power for spreadsheet
      // workflow) shouldn't be yanked out into the view's preferred
      // kanban every time they switch chips. The bead drawer
      // (?bead=) is preserved.
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
  const toggleLayout = useCallback(
    () => setLayout(layout === 'epic' ? 'workitem' : 'epic'),
    [setLayout, layout]
  );
  useHotkey('view-toggle-board', toggleLayout);

  const toggleListMode = useCallback(() => {
    if (layout === 'list') {
      // Returning from list → kanban: prefer epic (the global
      // default) unless the active named view prefers workitem.
      setLayout(view ? LEGACY_FROM_LAYOUT[view.defaultLayout] : 'epic');
    } else {
      setLayout('list');
    }
  }, [setLayout, layout, view]);
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

  // List layout runs its own filtered query; the kanban-level
  // loading and error gates only apply to the kanban renderers.
  if (layout !== 'list' && isLoading) return <SkeletonBoard />;
  if (layout !== 'list' && isError)
    return <ErrorState message={error?.message ?? 'Unknown error.'} onRetry={() => void refetch()} />;

  return (
    <>
      <BoardHeader
        layout={layout}
        onChangeLayout={setLayout}
        swimlane={swimlane}
        onChangeSwimlane={setSwimlane}
        view={view}
        onChangeView={setView}
        power={power}
        onChangePower={setPower}
        onNewWorkItem={() => setNewItemOpen(true)}
      />
      {layout === 'list' ? (
        <BoardListView
          view={view}
          stateCategories={listStates}
          kinds={listKinds}
          search={listSearch}
          onChangeStateCategories={setListStates}
          onChangeKinds={setListKinds}
          onChangeSearch={setListSearch}
          onSelectWorkItem={setOpenWorkItemId}
          power={power}
        />
      ) : !data || data.length === 0 ? (
        <EmptyState onCreate={() => setNewItemOpen(true)} />
      ) : layout === 'epic' ? (
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

// BoardHeader holds the layout picker — three layout buttons (Epic
// / Item / List), the swimlane partition selector (epic-only), and
// the named-view chip rail (gm-uipx.18).
interface BoardHeaderProps {
  layout: LayoutMode;
  onChangeLayout: (v: LayoutMode) => void;
  swimlane: SwimlaneMode;
  onChangeSwimlane: (s: SwimlaneMode) => void;
  view: WorkItemView | null;
  onChangeView: (id: string | null) => void;
  power: boolean;
  onChangePower: (next: boolean) => void;
  onNewWorkItem: () => void;
}
function BoardHeader({
  layout,
  onChangeLayout,
  swimlane,
  onChangeSwimlane,
  view,
  onChangeView,
  power,
  onChangePower,
  onNewWorkItem,
}: BoardHeaderProps) {
  return (
    <div
      data-testid="board-view-toggle"
      className="flex flex-wrap items-center gap-3 border-b border-neutral-200 bg-white/50 px-4 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950/50"
    >
      {layout === 'epic' ? (
        <SwimlaneSwitcher value={swimlane} onChange={onChangeSwimlane} />
      ) : null}
      <ViewSwitcher value={view} onChange={onChangeView} />
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
          active={layout === 'epic'}
          onClick={() => onChangeLayout('epic')}
          label="Epic"
          icon={<LayoutGrid className="h-3 w-3" />}
          testid="view-toggle-epic"
        />
        <ToggleButton
          active={layout === 'workitem'}
          onClick={() => onChangeLayout('workitem')}
          label="Item"
          icon={<ListChecks className="h-3 w-3" />}
          testid="view-toggle-workitem"
        />
        <ToggleButton
          active={layout === 'list'}
          onClick={() => onChangeLayout('list')}
          label="List"
          icon={<List className="h-3 w-3" />}
          testid="view-toggle-list"
        />
        {layout === 'list' ? (
          <>
            <div className="mx-1 h-4 w-px bg-neutral-200 dark:bg-neutral-800" />
            <ToggleButton
              active={power}
              onClick={() => onChangePower(!power)}
              label="Power"
              icon={<Zap className="h-3 w-3" />}
              testid="board-power-toggle"
            />
          </>
        ) : null}
      </div>
    </div>
  );
}

// ViewSwitcher renders one chip per registry entry (gm-uipx.18).
// Replaces the old PresetSwitcher; the testids keep the
// `board-preset-*` prefix (with the canonical view ids) so existing
// e2e selectors that don't carry a strong vocabulary contract on the
// chip name itself stay green.
interface ViewSwitcherProps {
  value: WorkItemView | null;
  onChange: (id: string | null) => void;
}
function ViewSwitcher({ value, onChange }: ViewSwitcherProps) {
  return (
    <div className="flex items-center gap-1" data-testid="board-preset-switcher">
      <span className="text-neutral-500">View</span>
      {WORK_ITEM_VIEWS.map((v) => (
        <button
          key={v.id}
          type="button"
          data-testid={`board-preset-${v.id}`}
          data-active={value?.id === v.id || undefined}
          onClick={() => onChange(value?.id === v.id ? null : v.id)}
          className={cn(
            'rounded border px-2 py-0.5 text-xs transition-colors',
            value?.id === v.id
              ? 'border-sky-700 bg-sky-700 text-white'
              : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800'
          )}
        >
          {v.label}
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
