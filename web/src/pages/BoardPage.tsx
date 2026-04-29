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
//   /board?bead=X                       → RHP workitem detail (migration shim; clears ?bead=)
//   /board?rhp=workitem:X               → RHP workitem detail tab
//   /board/:epicId                      → Epic kanban + RHP epic detail tab
//   /board/:epicId?rhp=workitem:X       → RHP epic + workitem detail tabs stacked

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { Inbox, LayoutGrid, List, ListChecks, Plus, RotateCcw, Zap } from 'lucide-react';
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import { BoardColumn } from '@/components/board/BoardColumn';
import { BoardListView } from '@/components/board/BoardListView';
import { EpicView } from '@/components/board/EpicView';
import { EpicDetailRegistration } from '@/components/rhp/details/EpicDetail';
import { useRhp } from '@/components/rhp/RhpContext';
import { NewWorkItemDialog } from '@/components/board/NewWorkItemDialog';
import { ScopePicker } from '@/components/board/ScopePicker';
import { SCOPE_ALL, filterByScope, lineageIDs, type ScopeID } from '@/components/board/scope';
import {
  buildEscalationsByItem,
  countEscalationsInScope,
} from '@/components/board/escalationLookup';
import { useEscalations } from '@/hooks/useEscalations';
import { Link } from 'react-router-dom';
import { AlertTriangle } from 'lucide-react';
import { MilestonePicker } from '@/components/board/MilestonePicker';
import {
  MILESTONE_ALL,
  filterByMilestone,
  type MilestoneID,
} from '@/components/board/milestone';
import { resolveRestage, shouldAutoStartSession } from '@/components/board/dragToRestage';
import { resolveReparent } from '@/components/board/dragToReparent';
import { useUpdateWorkItem } from '@/hooks/useWorkItems';
import { useStartSession } from '@/hooks/useSessions';
import { agentsKeys } from '@/hooks/useAgents';
import { useQueryClient } from '@tanstack/react-query';
import type { AgentRef } from '@/types/core.gen';
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

// show_backlog persistence (gm-5ekd). The kanban hides the Backlog
// column by default; ?show_backlog=1 brings it back. Triage now lives
// on /refine (gm-3ofd), so the kanban is for in-flight work. Mirrors
// the power-mode pattern: URL wins, localStorage is the fallback so
// an operator's preference survives reloads.
const SHOW_BACKLOG_PARAM = 'show_backlog';
const SHOW_BACKLOG_STORAGE_KEY = 'gemba.board.show-backlog';

function readStoredShowBacklog(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(SHOW_BACKLOG_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

function writeStoredShowBacklog(on: boolean): void {
  if (typeof window === 'undefined') return;
  try {
    if (on) window.localStorage.setItem(SHOW_BACKLOG_STORAGE_KEY, '1');
    else window.localStorage.removeItem(SHOW_BACKLOG_STORAGE_KEY);
  } catch {
    /* storage unavailable; URL param still works */
  }
}

// Filter the canonical STATE_CATEGORIES enum down to the columns the
// kanban renders. Backlog is dropped unless show_backlog is on.
function visibleColumns(showBacklog: boolean): readonly StateCategory[] {
  if (showBacklog) return STATE_CATEGORIES;
  return STATE_CATEGORIES.filter((c) => c !== 'backlog');
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
  // show_backlog resolution mirrors power: URL wins; on absence we
  // fall back to the last stored preference so a refresh sticks.
  const showBacklog = useMemo(() => {
    const raw = params.get(SHOW_BACKLOG_PARAM);
    if (raw === '1') return true;
    if (raw === '0') return false;
    return readStoredShowBacklog();
  }, [params]);
  const setShowBacklog = useCallback(
    (next: boolean) => {
      const p = new URLSearchParams(params);
      if (next) p.set(SHOW_BACKLOG_PARAM, '1');
      else p.delete(SHOW_BACKLOG_PARAM);
      writeStoredShowBacklog(next);
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

  // gm-root.22.6: pop the RHP epic detail tab on mount when the path
  // carries an epicId. popDetail handles both deep-links and
  // card-click navigations: same-kind replaces, different-kind stacks.
  // Guard with a ref so the effect runs once per mount; popDetail
  // calls setSearchParams and would otherwise loop on each render.
  const { popDetail } = useRhp();
  const epicPopRanRef = useRef(false);
  useEffect(() => {
    if (epicPopRanRef.current) return;
    epicPopRanRef.current = true;
    if (!epicId) return;
    popDetail({ kind: 'epic', id: epicId });
    // Intentionally a one-shot effect — see ref guard above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // gm-root.22.5: ?bead=X migration shim. On first paint, if a legacy
  // ?bead=X param is present and no ?rhp= param already exists, translate
  // it to ?rhp=workitem:X so the RHP detail tab opens. The ?bead= param
  // is then cleared so the URL stays clean. This runs once per mount.
  const rhpShimRanRef = useRef(false);
  useEffect(() => {
    if (rhpShimRanRef.current) return;
    rhpShimRanRef.current = true;
    const beadParam = rawParams.get('bead');
    if (!beadParam) return;
    const next = new URLSearchParams(rawParams);
    next.delete('bead');
    // Only set ?rhp= if not already present (operator may have navigated
    // directly with a ?rhp= codec deep-link that includes workitem).
    if (!next.get('rhp')) {
      next.set('rhp', `workitem:${beadParam}`);
    }
    setParams(next, { replace: true });
    // Intentionally a one-shot effect — see ref guard above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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

  // Scope (gm-uekk). Replaces the old swimlane-mode dropdown + root-
  // epic banner. URL-owned so deep-links survive reloads.
  // ?scope=<id> narrows the board to that scope's lineage; absent or
  // ?scope=all is the full project view.
  const scope: ScopeID = params.get('scope') ?? SCOPE_ALL;
  const setScope = useCallback(
    (next: ScopeID) => {
      const p = new URLSearchParams(params);
      if (next === SCOPE_ALL) p.delete('scope');
      else p.set('scope', next);
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  // Milestone (gm-l7hy). Independent axis from scope: scope narrows to
  // a single epic's lineage, milestone narrows to a milestone's child
  // epics + their descendants. Both can be active at once and are
  // composed (milestone filter first, then scope).
  const milestone: MilestoneID = params.get('milestone') ?? MILESTONE_ALL;
  const setMilestone = useCallback(
    (next: MilestoneID) => {
      const p = new URLSearchParams(params);
      if (next === MILESTONE_ALL) p.delete('milestone');
      else p.set('milestone', next);
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
  const [newItemOpen, setNewItemOpen] = useState(false);

  // gm-75u + gm-935r: a single DndContext spans both the BoardHeader
  // (so milestone-option rows can be drop targets) and the EpicView
  // (so column cells can be drop targets). The handler routes by the
  // over-id encoding: cell|... → restage; milestone-option|... →
  // re-parent. PointerSensor's 4px threshold prevents accidental drags
  // from a normal click; KeyboardSensor keeps the cards usable without
  // a pointer.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor)
  );
  const updateWorkItem = useUpdateWorkItem();
  const startSession = useStartSession();
  // Read the agents roster lazily from the query cache when auto-start
  // fires; we don't useAgents() here because mounting BoardPage in
  // error/loading states shouldn't trigger an agents fetch.
  const queryClient = useQueryClient();
  // Index by id so the drag handler can read source state without
  // re-walking the list. Keyed off the unfiltered dataset so a
  // milestone-narrowed view still resolves cards correctly when their
  // drag target lives outside the current filter.
  const itemById = useMemo(() => {
    const m = new Map<string, WorkItem>();
    for (const it of data ?? []) m.set(it.id, it);
    return m;
  }, [data]);
  const onDragEnd = useCallback(
    (event: DragEndEvent) => {
      const reparent = resolveReparent({
        activeID: event.active.id,
        overID: event.over?.id,
        itemById,
      });
      if (reparent) {
        updateWorkItem.mutate(reparent);
        return;
      }
      const restage = resolveRestage({
        activeID: event.active.id,
        overID: event.over?.id,
        itemById,
      });
      if (!restage) return;
      updateWorkItem.mutate(restage, {
        onSuccess: (updated) => {
          if (!shouldAutoStartSession(updated)) return;
          const agents = queryClient.getQueryData<AgentRef[]>(agentsKeys.list()) ?? [];
          let agent = 'claude';
          for (const a of agents) {
            if (a.dialect) {
              agent = a.dialect;
              break;
            }
          }
          startSession.mutate({ bead_id: updated.id, agent_type: agent });
        },
      });
    },
    [itemById, updateWorkItem, startSession, queryClient]
  );

  // gm-e11.3: build the per-item escalation lookup once, then derive
  // both the per-card badge counts and the scope-aware banner count
  // from it. These hooks run unconditionally (before the early
  // returns) so the hook order stays stable across paint cycles.
  const escalationsQuery = useEscalations();
  const escalationsByItem = useMemo(
    () => buildEscalationsByItem(escalationsQuery.data),
    [escalationsQuery.data]
  );
  const escalationCounts = useMemo(() => {
    const m = new Map<string, number>();
    for (const [id, list] of escalationsByItem) m.set(id, list.length);
    return m;
  }, [escalationsByItem]);
  // Scope-aware banner: when scope=all, count every targeted open
  // escalation; otherwise restrict to the current scope's lineage.
  const bannerCount = useMemo(() => {
    if (escalationsByItem.size === 0) return 0;
    const ids = scope === SCOPE_ALL ? undefined : lineageIDs(data ?? [], scope);
    return countEscalationsInScope(escalationsByItem, ids);
  }, [escalationsByItem, scope, data]);

  // List layout runs its own filtered query; the kanban-level
  // loading and error gates only apply to the kanban renderers.
  if (layout !== 'list' && isLoading) return <SkeletonBoard />;
  if (layout !== 'list' && isError)
    return <ErrorState message={error?.message ?? 'Unknown error.'} onRetry={() => void refetch()} />;

  // gm-uekk + gm-l7hy: compose milestone → scope filtering. The
  // unfiltered `data` is still passed to the pickers so their
  // dropdowns can enumerate every option regardless of the active
  // selection. Order: milestone narrows first (drops other milestones'
  // subtrees), then scope narrows within that.
  const scopedData = data
    ? filterByScope(filterByMilestone(data, milestone), scope)
    : data;

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <BoardHeader
        layout={layout}
        onChangeLayout={setLayout}
        items={data ?? []}
        scope={scope}
        onChangeScope={setScope}
        milestone={milestone}
        onChangeMilestone={setMilestone}
        onShowMilestone={(id) => popDetail({ kind: 'workitem', id })}
        view={view}
        onChangeView={setView}
        power={power}
        onChangePower={setPower}
        showBacklog={showBacklog}
        onChangeShowBacklog={setShowBacklog}
        onNewWorkItem={() => setNewItemOpen(true)}
      />
      {bannerCount > 0 && <EscalationBanner count={bannerCount} />}
      {layout === 'list' ? (
        <BoardListView
          view={view}
          stateCategories={listStates}
          kinds={listKinds}
          search={listSearch}
          onChangeStateCategories={setListStates}
          onChangeKinds={setListKinds}
          onChangeSearch={setListSearch}
          onSelectWorkItem={(id) => popDetail({ kind: 'workitem', id })}
          power={power}
          scope={scope}
        />
      ) : !scopedData || scopedData.length === 0 ? (
        <EmptyState onCreate={() => setNewItemOpen(true)} />
      ) : layout === 'epic' ? (
        <EpicView
          items={scopedData}
          onSelectEpic={openEpic}
          showBacklog={showBacklog}
          escalationCounts={escalationCounts}
        />
      ) : (
        <WorkItemBoard
          data={scopedData}
          onSelectWorkItem={(id) => { if (id) popDetail({ kind: 'workitem', id }); }}
          showBacklog={showBacklog}
          escalationCounts={escalationCounts}
        />
      )}
      <EpicDetailRegistration />
      <NewWorkItemDialog
        open={newItemOpen}
        onClose={() => setNewItemOpen(false)}
        onCreated={(item) => popDetail({ kind: 'workitem', id: item.id })}
      />
    </DndContext>
  );
}

// BoardHeader holds three controls along one row (gm-uekk):
//   Scope picker (left)   — narrow to a root or child epic
//   View chips (centre)   — Mine / Ready / Done · 7d filter
//   Layout toggles (right) — Epic / Item / List shape
//
// The pre-uekk swimlane-mode dropdown and root-epic banner are gone;
// scope subsumes them.
interface BoardHeaderProps {
  layout: LayoutMode;
  onChangeLayout: (v: LayoutMode) => void;
  items: WorkItem[];
  scope: ScopeID;
  onChangeScope: (s: ScopeID) => void;
  milestone: MilestoneID;
  onChangeMilestone: (m: MilestoneID) => void;
  onShowMilestone: (id: string) => void;
  view: WorkItemView | null;
  onChangeView: (id: string | null) => void;
  power: boolean;
  onChangePower: (next: boolean) => void;
  showBacklog: boolean;
  onChangeShowBacklog: (next: boolean) => void;
  onNewWorkItem: () => void;
}
function BoardHeader({
  layout,
  onChangeLayout,
  items,
  scope,
  onChangeScope,
  milestone,
  onChangeMilestone,
  onShowMilestone,
  view,
  onChangeView,
  power,
  onChangePower,
  showBacklog,
  onChangeShowBacklog,
  onNewWorkItem,
}: BoardHeaderProps) {
  return (
    <div
      data-testid="board-view-toggle"
      className="flex flex-wrap items-center gap-3 border-b border-neutral-200 bg-white/50 px-4 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-950/50"
    >
      <ScopePicker items={items} value={scope} onChange={onChangeScope} />
      <MilestonePicker
        items={items}
        value={milestone}
        onChange={onChangeMilestone}
        onShow={onShowMilestone}
      />
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
        ) : (
          <>
            <div className="mx-1 h-4 w-px bg-neutral-200 dark:bg-neutral-800" />
            <ToggleButton
              active={showBacklog}
              onClick={() => onChangeShowBacklog(!showBacklog)}
              label="Backlog"
              icon={<Inbox className="h-3 w-3" />}
              testid="board-show-backlog-toggle"
            />
          </>
        )}
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

// EscalationBanner (gm-e11.3) — surfaces the count of open
// EscalationRequests within the board's current scope, with a
// click-through to /escalations. Hidden when count is zero (the
// page only mounts this when count > 0).
function EscalationBanner({ count }: { count: number }) {
  return (
    <Link
      to="/escalations"
      data-testid="board-escalation-banner"
      data-escalation-count={count}
      className={cn(
        'flex items-center gap-2 border-b border-rose-200 bg-rose-50 px-4 py-1.5 text-xs',
        'text-rose-800 hover:bg-rose-100',
        'dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200 dark:hover:bg-rose-950/70'
      )}
    >
      <AlertTriangle className="h-3.5 w-3.5" aria-hidden />
      <span>
        {count} open escalation{count === 1 ? '' : 's'} in this scope
      </span>
      <span className="ml-auto font-medium underline-offset-2 hover:underline">View →</span>
    </Link>
  );
}

interface WorkItemBoardProps {
  data: WorkItem[];
  onSelectWorkItem: (id: string | null) => void;
  showBacklog: boolean;
  escalationCounts?: Map<string, number>;
}
function WorkItemBoard({
  data,
  onSelectWorkItem,
  showBacklog,
  escalationCounts,
}: WorkItemBoardProps) {
  const groups = useMemo(() => groupByStateCategory(data), [data]);
  const columns = visibleColumns(showBacklog);
  return (
    <div data-testid="board-workitem" className="flex h-full gap-3 overflow-x-auto p-4">
      {columns.map((cat) => (
        <BoardColumn
          key={cat}
          category={cat}
          label={COLUMN_LABELS[cat]}
          items={groups[cat]}
          onSelect={onSelectWorkItem}
          escalationCounts={escalationCounts}
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
            <h2 className="text-xs font-semibold uppercase tracking-wide text-neutral-600 dark:text-neutral-400">
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
  const navigate = useNavigate();
  // gm-root.17.13: gate the "Plan with the Onboarder" CTA on whether
  // an LLM client is configured. The probe is fire-and-forget — the
  // CTA simply doesn't render until/unless the probe says yes, so a
  // missing or slow probe never blocks the manual-create path.
  const [planAvailable, setPlanAvailable] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void import('@/api/newproject').then(({ probeOnboarder }) => {
      probeOnboarder()
        .then((p) => {
          if (!cancelled) setPlanAvailable(Boolean(p.available));
        })
        .catch(() => {
          /* swallow — the CTA stays hidden on probe failure */
        });
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div
      data-testid="board-empty"
      className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center"
    >
      <p className="text-sm text-neutral-600 dark:text-neutral-300">No beads yet.</p>
      <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">
        Create one below, or use <code className="font-mono">bd create</code> from your terminal.
      </p>
      <div className="mt-1 flex flex-wrap items-center justify-center gap-2">
        <button
          type="button"
          onClick={onCreate}
          className="inline-flex items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          <Plus className="h-3.5 w-3.5" />
          New work item
        </button>
        {planAvailable ? (
          <button
            type="button"
            data-testid="board-empty-plan"
            onClick={() => navigate('/onboard')}
            className="inline-flex items-center gap-1.5 rounded-md border border-emerald-300 bg-emerald-50 px-3 py-1.5 text-sm text-emerald-800 hover:bg-emerald-100 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200 dark:hover:bg-emerald-950/70"
          >
            Plan with the Onboarder →
          </button>
        ) : null}
      </div>
    </div>
  );
}
