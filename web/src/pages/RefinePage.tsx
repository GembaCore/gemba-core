// /refine — dedicated triage + refinement surface (gm-3ofd).
//
// Hardwired filter: state_category=backlog. Default sort: priority desc,
// then age desc (older items bubble up inside a priority band).
// Power features (bulk-action surface, column presets, JSONL import)
// are always on — refinement is a dense triage activity by definition.
//
// Reuses WorkItemGrid; refine-specific columns + actions land in
// follow-up children (see docs/design/refine.md).

import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import {
  BookOpenCheck,
  CheckCircle2,
  FileText,
  FolderPlus,
  GitBranch,
  LayoutGrid,
  MessagesSquare,
  Plus,
  Save,
  Search,
  TableProperties,
} from 'lucide-react';
import { WorkItemGrid, type BulkAction } from '@/components/grid/WorkItemGrid';
import { BulkEditDialog } from '@/components/grid/BulkEditDialog';
import { EpicPickerDialog } from '@/components/refine/EpicPickerDialog';
import { useFilteredWorkItems, useUpdateWorkItem, useWorkItems } from '@/hooks/useWorkItems';
import type { WorkItemPatch } from '@/api/workItems';
import { useRhp } from '@/components/rhp/RhpContext';
import { useSearchParams } from 'react-router-dom';
import type { WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';
import { BeadsCascadeView } from '@/components/board/BeadsCascadeView';
import { EpicView } from '@/components/board/EpicView';
import { DndContext } from '@dnd-kit/core';
import {
  useCreateSpecKitFeature,
  useInitializeSpecKitScaffold,
  useSaveSpecKitFile,
  useSpecKitFile,
  useSpecKitSyncDraft,
  useSpecKitWorkspace,
  useSyncSpecKitFeature,
} from '@/hooks/useSpecKit';
import type {
  SpecKitFeature,
  SpecKitSyncPlan,
  SpecKitTask,
  SpecKitWorkspace,
  SpecKitWorkspaceFile,
} from '@/api/specKit';
import { encodeInteractionTarget } from '@/interactions/types';
import { draftItemsToJSONL, parseDraftJSONL } from '@/api/bootstrapDrafts';
import { useApplyBootstrapDraft } from '@/hooks/useBootstrapDrafts';

const REFINE_PRESETS_STORAGE_KEY = 'gemba.refine.column-presets';
const DEFER_LABEL_PREFIX = 'defer-until:';
const REFINE_VIEW_PARAM = 'view';
type RefineView = 'table' | 'hierarchy' | 'swimlanes' | 'bootstrap';

function refineViewFromParams(params: URLSearchParams): RefineView {
  const raw = params.get(REFINE_VIEW_PARAM);
  if (raw === 'hierarchy' || raw === 'swimlanes' || raw === 'bootstrap') return raw;
  if (raw === 'spec-kit') return 'bootstrap';
  return 'table';
}

// /refine surfaces the refine-specific columns (gm-51i2). The grid hides
// these globally so the Board's list mode stays lean; this override
// flips them back on for /refine only.
const REFINE_VISIBILITY = {
  age: true,
  suggested_epic: true,
  blockers: true,
  dispatch_status: true,
};

// Backlog rows sort by priority (lower number = higher) then by age
// (older = updated_at ascending so it sinks to the top after the
// reverse below). Stable sort.
function refineDefaultSort(rows: WorkItem[]): WorkItem[] {
  const out = rows.slice();
  out.sort((a, b) => {
    const pa = a.priority ?? 99;
    const pb = b.priority ?? 99;
    if (pa !== pb) return pa - pb;
    // older first within priority band
    const ta = Date.parse(a.created_at) || 0;
    const tb = Date.parse(b.created_at) || 0;
    return ta - tb;
  });
  return out;
}

// stripDeferUntil removes any existing defer-until:* labels — only the
// prefix-with-colon form, never a substring match — so we never leak a
// stale defer marker when the operator picks a new date.
function stripDeferUntil(labels: string[] | undefined): string[] {
  return (labels ?? []).filter((l) => !l.startsWith(DEFER_LABEL_PREFIX));
}

export function RefinePage() {
  const [params, setParams] = useSearchParams();
  const view = refineViewFromParams(params);
  const search = params.get('q') ?? '';
  const setView = useCallback(
    (next: RefineView) => {
      const p = new URLSearchParams(params);
      if (next === 'table') p.delete(REFINE_VIEW_PARAM);
      else p.set(REFINE_VIEW_PARAM, next);
      setParams(p, { replace: true });
    },
    [params, setParams]
  );
  const onChangeSearch = useCallback(
    (next: string) => {
      const p = new URLSearchParams(params);
      if (next) p.set('q', next);
      else p.delete('q');
      setParams(p, { replace: true });
    },
    [params, setParams]
  );

  const { popDetail } = useRhp();

  const {
    data = [],
    isLoading,
    error,
  } = useFilteredWorkItems({
    state_category: ['backlog'],
  });
  const allItems = useWorkItems();
  const specKit = useSpecKitWorkspace(view === 'bootstrap');

  const updateWorkItem = useUpdateWorkItem();
  const syncSpecKit = useSyncSpecKitFeature();
  const [bulkEdit, setBulkEdit] = useState<{ ids: string[] } | null>(null);
  const [epicPick, setEpicPick] = useState<{ ids: string[] } | null>(null);
  const [deferTarget, setDeferTarget] = useState<{ ids: string[] } | null>(null);
  const [dismissTarget, setDismissTarget] = useState<{ ids: string[] } | null>(null);

  const applyBulkPatch = useCallback(
    (ids: string[], patch: WorkItemPatch) => {
      for (const id of ids) {
        updateWorkItem.mutate({ id, patch });
      }
    },
    [updateWorkItem]
  );

  // applyDefer writes a defer-until:<iso> label per id, stripping any
  // prior defer-until:* marker so the row carries exactly one. An empty
  // iso is a no-op (rows are already backlog; nothing to record).
  const applyDefer = useCallback(
    (ids: string[], iso: string) => {
      if (!iso) return;
      for (const id of ids) {
        const row = data.find((r) => r.id === id);
        if (!row) continue;
        const next = [...stripDeferUntil(row.labels), `${DEFER_LABEL_PREFIX}${iso}`];
        updateWorkItem.mutate({ id, patch: { labels: next } });
      }
    },
    [data, updateWorkItem]
  );

  // applyDismiss flips state_category → canceled. The optional reason is
  // captured but not currently sent — WorkItemPatch has no notes-append
  // field, and the bead's spec says ship the state-change without it
  // rather than invent a backend feature. TODO(gm-mw5n): pipe reason
  // through once an append-notes patch path lands.
  const applyDismiss = useCallback(
    (ids: string[], _reason: string) => {
      for (const id of ids) {
        updateWorkItem.mutate({ id, patch: { state_category: 'canceled' } });
      }
    },
    [updateWorkItem]
  );

  const handleBulkAction = useCallback((action: BulkAction, ids: string[]) => {
    if (action === 'edit') {
      setBulkEdit({ ids });
    } else if (action === 'defer') {
      setDeferTarget({ ids });
    } else if (action === 'dismiss' || action === 'delete') {
      // Re-purpose 'delete' as a dismiss alias — /refine never wants
      // to actually drop data, only to cancel with a recorded reason.
      setDismissTarget({ ids });
    } else if (action === 'drop-into-epic') {
      setEpicPick({ ids });
    }
  }, []);

  const rows = useMemo(() => {
    let r = refineDefaultSort(data);
    const needle = search.trim().toLowerCase();
    if (needle) r = r.filter((it) => it.title.toLowerCase().includes(needle));
    return r;
  }, [data, search]);
  const planningRows = useMemo(() => {
    let r = refineDefaultSort(allItems.data ?? []);
    const needle = search.trim().toLowerCase();
    if (needle) {
      r = r.filter(
        (it) =>
          it.title.toLowerCase().includes(needle) ||
          it.id.toLowerCase().includes(needle) ||
          it.kind.toLowerCase().includes(needle)
      );
    }
    return r;
  }, [allItems.data, search]);
  const specKitFeatures = useMemo(() => {
    const features = specKit.data?.features ?? [];
    const needle = search.trim().toLowerCase();
    if (!needle) return features;
    return features.filter(
      (feature) =>
        feature.title.toLowerCase().includes(needle) ||
        feature.id.toLowerCase().includes(needle) ||
        feature.directory.toLowerCase().includes(needle)
    );
  }, [specKit.data?.features, search]);
  const visibleCount =
    view === 'table'
      ? rows.length
      : view === 'bootstrap'
        ? specKitFeatures.length
        : planningRows.length;

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="refine-page">
      <div
        className="flex flex-wrap items-center gap-3 border-b border-neutral-200 px-6 py-3 text-xs dark:border-neutral-800"
        data-testid="refine-toolbar"
      >
        <h1 className="text-sm font-semibold tracking-tight">Refine</h1>
        <span className="text-neutral-500" data-testid="refine-row-count">
          {visibleCount} item
          {visibleCount === 1 ? '' : 's'}
        </span>
        <div className="flex items-center gap-1">
          <RefineViewButton
            active={view === 'table'}
            onClick={() => setView('table')}
            label="Table"
            icon={<TableProperties className="h-3.5 w-3.5" />}
            testid="refine-view-table"
          />
          <RefineViewButton
            active={view === 'hierarchy'}
            onClick={() => setView('hierarchy')}
            label="Hierarchy"
            icon={<GitBranch className="h-3.5 w-3.5" />}
            testid="refine-view-hierarchy"
          />
          <RefineViewButton
            active={view === 'swimlanes'}
            onClick={() => setView('swimlanes')}
            label="Swimlanes"
            icon={<LayoutGrid className="h-3.5 w-3.5" />}
            testid="refine-view-swimlanes"
          />
          <RefineViewButton
            active={view === 'bootstrap'}
            onClick={() => setView('bootstrap')}
            label="Bootstrap"
            icon={<BookOpenCheck className="h-3.5 w-3.5" />}
            testid="refine-view-bootstrap"
          />
        </div>
        <div className="ml-auto flex items-center gap-2">
          <label className="relative">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-neutral-500" />
            <input
              data-testid="refine-search"
              type="search"
              placeholder="Search title…"
              value={search}
              onChange={(e) => onChangeSearch(e.target.value)}
              className={cn(
                'h-7 w-56 rounded-md border border-neutral-300 bg-white pl-7 pr-2 text-xs',
                'dark:border-neutral-700 dark:bg-neutral-900'
              )}
            />
          </label>
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-auto">
        {view === 'bootstrap' ? (
          <SpecKitPanel
            features={specKitFeatures}
            configured={specKit.data?.configured ?? false}
            workspace={specKit.data}
            loading={specKit.isLoading}
            error={specKit.error?.message}
            syncingId={syncSpecKit.variables?.id}
            syncing={syncSpecKit.isPending}
            syncError={syncSpecKit.error?.message}
            onSync={(id, plan, allowDeletes, items) =>
              syncSpecKit.mutate({ id, planHash: plan.hash, allowDeletes, items })
            }
          />
        ) : view === 'table' && isLoading ? (
          <RefineSkeleton />
        ) : view === 'table' && error ? (
          <RefineError message={error.message} />
        ) : view === 'table' && rows.length === 0 ? (
          <RefineEmpty hasSearch={!!search.trim()} />
        ) : view === 'table' ? (
          <WorkItemGrid
            rows={rows}
            onSelect={(id) => popDetail({ kind: 'workitem', id })}
            onBulkAction={handleBulkAction}
            presets={{ storageKey: REFINE_PRESETS_STORAGE_KEY }}
            visibilityOverride={REFINE_VISIBILITY}
          />
        ) : allItems.isLoading ? (
          <RefineSkeleton />
        ) : allItems.error ? (
          <RefineError message={allItems.error.message} />
        ) : planningRows.length === 0 ? (
          <RefineEmpty hasSearch={!!search.trim()} />
        ) : view === 'hierarchy' ? (
          <BeadsCascadeView
            items={planningRows}
            orderKey="modified"
            onSelect={(item) => {
              if (item.kind === 'epic') popDetail({ kind: 'epic', id: item.id });
              else popDetail({ kind: 'workitem', id: item.id });
            }}
          />
        ) : (
          <DndContext>
            <EpicView
              items={planningRows}
              onSelectEpic={(id) => popDetail({ kind: 'epic', id })}
              showBacklog
              orderKey="modified"
            />
          </DndContext>
        )}
      </div>

      {bulkEdit && (
        <BulkEditDialog
          open
          ids={bulkEdit.ids}
          onClose={() => setBulkEdit(null)}
          onApply={(patch) => {
            applyBulkPatch(bulkEdit.ids, patch);
            setBulkEdit(null);
          }}
        />
      )}

      {epicPick && (
        <EpicPickerDialog
          open
          ids={epicPick.ids}
          onClose={() => setEpicPick(null)}
          onPick={(epicId) => {
            applyBulkPatch(epicPick.ids, { parent_id: epicId });
            setEpicPick(null);
          }}
        />
      )}

      {deferTarget && (
        <DeferDialog
          ids={deferTarget.ids}
          onClose={() => setDeferTarget(null)}
          onApply={(iso) => {
            applyDefer(deferTarget.ids, iso);
            setDeferTarget(null);
          }}
        />
      )}

      {dismissTarget && (
        <DismissDialog
          ids={dismissTarget.ids}
          onClose={() => setDismissTarget(null)}
          onApply={(reason) => {
            applyDismiss(dismissTarget.ids, reason);
            setDismissTarget(null);
          }}
        />
      )}
    </div>
  );
}

interface RefineViewButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  icon: ReactNode;
  testid: string;
}

function RefineViewButton({ active, onClick, label, icon, testid }: RefineViewButtonProps) {
  return (
    <button
      type="button"
      data-testid={testid}
      data-active={active}
      onClick={onClick}
      className={cn(
        'inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs',
        active
          ? 'border-sky-700 bg-sky-700 text-white dark:border-sky-500 dark:bg-sky-600'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800'
      )}
    >
      {icon}
      {label}
    </button>
  );
}

function RefineSkeleton() {
  return (
    <div className="p-6 text-xs text-neutral-500" data-testid="refine-loading">
      Loading backlog…
    </div>
  );
}

function RefineError({ message }: { message: string }) {
  return (
    <div className="p-6 text-xs text-rose-700 dark:text-rose-300" data-testid="refine-error">
      {message}
    </div>
  );
}

function RefineEmpty({ hasSearch }: { hasSearch: boolean }) {
  return (
    <div
      className="flex h-full items-center justify-center p-6 text-xs text-neutral-500"
      data-testid="refine-empty"
    >
      {hasSearch
        ? 'No backlog items match the search.'
        : 'Nothing in the backlog. New items land here on filing.'}
    </div>
  );
}

interface SpecKitPanelProps {
  features: SpecKitFeature[];
  configured: boolean;
  workspace?: SpecKitWorkspace;
  loading: boolean;
  error?: string;
  syncingId?: string;
  syncing: boolean;
  syncError?: string;
  onSync: (
    id: string,
    plan: SpecKitSyncPlan,
    allowDeletes: boolean,
    items?: WorkItem[]
  ) => void;
}

function SpecKitPanel({
  features,
  configured,
  workspace,
  loading,
  error,
  syncingId,
  syncing,
  syncError,
  onSync,
}: SpecKitPanelProps) {
  const [selectedID, setSelectedID] = useState<string>('');
  const [allowDeletes, setAllowDeletes] = useState(false);
  const [selectedDraftID, setSelectedDraftID] = useState<string>('');
  const [draftItems, setDraftItems] = useState<WorkItem[]>([]);
  const [draftSource, setDraftSource] = useState<'spec-kit' | 'jsonl'>('spec-kit');
  const [draftError, setDraftError] = useState<string>('');
  const [surface, setSurface] = useState<'draft' | 'files'>('draft');
  const [selectedFilePath, setSelectedFilePath] = useState<string>('');
  const [fileText, setFileText] = useState('');
  const [fileDirty, setFileDirty] = useState(false);
  const [newFeatureTitle, setNewFeatureTitle] = useState('');
  const { popDetail } = useRhp();
  const applyDraft = useApplyBootstrapDraft();
  const initializeScaffold = useInitializeSpecKitScaffold();
  const createFeature = useCreateSpecKitFeature();
  const saveFile = useSaveSpecKitFile();
  useEffect(() => {
    if (features.length === 0) {
      setSelectedID('');
      return;
    }
    if (!features.some((feature) => feature.id === selectedID)) {
      setSelectedID(features[0].id);
    }
  }, [features, selectedID]);
  const selected = features.find((feature) => feature.id === selectedID) ?? features[0];
  const draft = useSpecKitSyncDraft(selected?.id, !!selected);
  const plan = draft.data?.plan;
  useEffect(() => {
    setAllowDeletes(false);
  }, [selected?.id, plan?.hash]);
  useEffect(() => {
    setDraftItems(draft.data?.items ?? []);
    setSelectedDraftID(draft.data?.items?.[0]?.id ?? '');
    setDraftSource('spec-kit');
    setDraftError('');
  }, [draft.data?.items, draft.data?.plan?.hash]);
  const files = workspace?.files ?? [];
  const selectedFiles = useMemo(() => {
    if (!selected) return files;
    const featureFiles = files.filter((file) => file.feature_id === selected.id);
    const supportFiles = files.filter((file) => !file.feature_id);
    return [...featureFiles, ...supportFiles];
  }, [files, selected]);
  useEffect(() => {
    if (selectedFiles.length === 0) {
      setSelectedFilePath('');
      return;
    }
    if (!selectedFiles.some((file) => file.path === selectedFilePath)) {
      setSelectedFilePath(selectedFiles[0].path);
    }
  }, [selectedFiles, selectedFilePath]);
  const selectedFile = selectedFiles.find((file) => file.path === selectedFilePath);
  const fileQuery = useSpecKitFile(selectedFilePath, surface === 'files' && !!selectedFilePath);
  useEffect(() => {
    if (!selected && files.length > 0) {
      setSurface('files');
    }
  }, [files.length, selected]);
  useEffect(() => {
    if (fileQuery.data?.path === selectedFilePath) {
      setFileText(fileQuery.data.content);
      setFileDirty(false);
    }
  }, [fileQuery.data?.content, fileQuery.data?.path, selectedFilePath]);
  const pendingDeletes = plan?.counts.delete ?? 0;
  const canSync =
    !!selected &&
    (!!plan?.hash || draftSource === 'jsonl') &&
    draftItems.length > 0 &&
    !draft.isLoading &&
    !draft.error &&
    (!pendingDeletes || allowDeletes);
  const selectedDraft = draftItems.find((item) => item.id === selectedDraftID) ?? draftItems[0];
  const updateDraftItem = (id: string, patch: Partial<WorkItem>) => {
    setDraftItems((items) => items.map((item) => (item.id === id ? { ...item, ...patch } : item)));
  };
  const applyCurrentDraft = () => {
    if (draftSource === 'jsonl') {
      applyDraft.mutate({ items: draftItems, targetDatabase: 'active' });
    } else if (plan && selected) {
      onSync(selected.id, plan, allowDeletes, draftItems);
    }
  };
  const exportDraft = () => {
    const blob = new Blob([draftItemsToJSONL(draftItems)], { type: 'application/x-ndjson' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${selected?.id ?? 'bootstrap-draft'}-beads.jsonl`;
    a.click();
    URL.revokeObjectURL(url);
  };
  const uploadDraft = async (file: File | null) => {
    if (!file) return;
    try {
      const items = parseDraftJSONL(await file.text());
      setDraftItems(items);
      setSelectedDraftID(items[0]?.id ?? '');
      setDraftSource('jsonl');
      setDraftError('');
    } catch (err) {
      setDraftError((err as Error).message);
    }
  };
  const createNewFeature = () => {
    const title = newFeatureTitle.trim();
    if (!title || createFeature.isPending) return;
    createFeature.mutate(
      { title },
      {
        onSuccess: (feature) => {
          setNewFeatureTitle('');
          setSelectedID(feature.id);
          setSurface('files');
        },
      }
    );
  };
  const saveCurrentFile = () => {
    if (!selectedFilePath || saveFile.isPending) return;
    saveFile.mutate(
      { path: selectedFilePath, content: fileText },
      {
        onSuccess: (file) => {
          setFileText(file.content);
          setFileDirty(false);
        },
      }
    );
  };

  if (loading) return <RefineSkeleton />;
  if (error) return <RefineError message={error} />;
  if (!configured) {
    return (
      <div className="h-full min-h-0" data-testid="spec-kit-panel">
        <section
          className="flex h-full min-h-0 items-center justify-center p-6"
          data-testid="spec-kit-empty"
        >
          <div className="max-w-md text-xs text-neutral-600 dark:text-neutral-300">
            <div className="text-[11px] font-semibold uppercase text-neutral-500">
              Bootstrap pack · Spec Kit
            </div>
            <h2 className="mt-2 text-base font-semibold tracking-tight text-neutral-950 dark:text-neutral-100">
              No Spec Kit workspace yet
            </h2>
            <p className="mt-2 leading-5">
              Initialize the project scaffolding, then create a spec set or drop existing Spec Kit files into the project directory.
            </p>
            <button
              type="button"
              data-testid="spec-kit-initialize"
              disabled={initializeScaffold.isPending}
              onClick={() => initializeScaffold.mutate()}
              className="mt-4 inline-flex h-8 items-center gap-2 rounded-md bg-sky-700 px-3 text-xs font-medium text-white hover:bg-sky-800 disabled:cursor-wait disabled:opacity-60 dark:bg-sky-600 dark:hover:bg-sky-500"
            >
              <FolderPlus className="h-3.5 w-3.5" />
              Initialize Spec Kit
            </button>
            {initializeScaffold.error && (
              <p className="mt-3 text-rose-700 dark:text-rose-300">
                {initializeScaffold.error.message}
              </p>
            )}
          </div>
        </section>
      </div>
    );
  }

  return (
    <div
      className="flex h-full min-h-0 flex-col"
      data-testid="spec-kit-panel"
    >
      <SpecKitFeatureStrip
        features={features}
        selectedID={selected?.id ?? ''}
        onSelect={setSelectedID}
        newFeatureTitle={newFeatureTitle}
        onNewFeatureTitle={setNewFeatureTitle}
        onCreateFeature={createNewFeature}
        creatingFeature={createFeature.isPending}
        createError={createFeature.error?.message}
        canCreateFeature
      />
      {selected ? (
        <section className="min-h-0 flex-1 overflow-auto p-5" data-testid="spec-kit-detail">
          <header className="flex flex-wrap items-start justify-between gap-3 border-b border-neutral-200 pb-4 dark:border-neutral-800">
            <div>
              <div className="text-[11px] font-semibold uppercase text-neutral-500">
                Bootstrap pack · Spec Kit
              </div>
              <h2 className="mt-1 text-base font-semibold tracking-tight">{selected.title}</h2>
              <div className="mt-1 flex flex-wrap gap-2 text-xs text-neutral-500">
                <span>{selected.directory}</span>
                {selected.spec_path && <span>{selected.spec_path}</span>}
                {selected.tasks_path && <span>{selected.tasks_path}</span>}
              </div>
            </div>
            <div className="flex h-8 rounded-md border border-neutral-300 p-0.5 dark:border-neutral-700">
              <button
                type="button"
                data-testid="spec-kit-tab-draft"
                data-active={surface === 'draft'}
                onClick={() => setSurface('draft')}
                className={cn(
                  'inline-flex items-center gap-1 rounded px-2 text-xs font-medium text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-900',
                  surface === 'draft' && 'bg-neutral-100 text-neutral-950 dark:bg-neutral-800 dark:text-neutral-100'
                )}
              >
                <CheckCircle2 className="h-3.5 w-3.5" />
                Draft Beads
              </button>
              <button
                type="button"
                data-testid="spec-kit-tab-files"
                data-active={surface === 'files'}
                onClick={() => setSurface('files')}
                className={cn(
                  'inline-flex items-center gap-1 rounded px-2 text-xs font-medium text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-900',
                  surface === 'files' && 'bg-neutral-100 text-neutral-950 dark:bg-neutral-800 dark:text-neutral-100'
                )}
              >
                <FileText className="h-3.5 w-3.5" />
                Spec Files
              </button>
            </div>
            <button
              type="button"
              data-testid="spec-kit-sync"
              disabled={!canSync || (syncing && syncingId === selected.id) || applyDraft.isPending}
              onClick={applyCurrentDraft}
              className="inline-flex h-8 items-center gap-2 rounded-md bg-sky-700 px-3 text-xs font-medium text-white hover:bg-sky-800 disabled:cursor-wait disabled:opacity-60 dark:bg-sky-600 dark:hover:bg-sky-500"
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              {syncing && syncingId === selected.id || applyDraft.isPending ? 'Committing' : 'Ratify to active DB'}
            </button>
            <button
              type="button"
              data-testid="bootstrap-export-jsonl"
              disabled={draftItems.length === 0}
              onClick={exportDraft}
              className="inline-flex h-8 items-center rounded-md border border-neutral-300 px-3 text-xs font-medium text-neutral-700 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-900"
            >
              Export JSONL
            </button>
            <label className="inline-flex h-8 cursor-pointer items-center rounded-md border border-neutral-300 px-3 text-xs font-medium text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-900">
              Upload JSONL
              <input
                data-testid="bootstrap-upload-jsonl"
                type="file"
                accept=".jsonl,.ndjson,application/x-ndjson,application/jsonl,text/plain"
                className="sr-only"
                onChange={(e) => void uploadDraft(e.currentTarget.files?.[0] ?? null)}
              />
            </label>
            <button
              type="button"
              data-testid="spec-kit-open-coach"
              onClick={() =>
                popDetail({
                  kind: 'interaction',
                  id: encodeInteractionTarget({
                    type: 'bootstrap',
                    id: selected.id,
                    title: selected.title,
                    breadcrumb: [{ type: 'project', id: 'bootstrap', label: 'Bootstrap' }],
                  }),
                })
              }
              className="inline-flex h-8 items-center gap-2 rounded-md border border-neutral-300 px-3 text-xs font-medium text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-900"
            >
              <MessagesSquare className="h-3.5 w-3.5" />
              Coach
            </button>
          </header>
          {syncError && (
            <div
              className="mt-3 text-xs text-rose-700 dark:text-rose-300"
              data-testid="spec-kit-sync-error"
            >
              {syncError}
            </div>
          )}
          {(draftError || applyDraft.error?.message) && (
            <div className="mt-3 text-xs text-rose-700 dark:text-rose-300">
            {draftError || applyDraft.error?.message}
            </div>
          )}
          {surface === 'files' ? (
            <SpecKitFileEditor
              files={selectedFiles}
              selectedPath={selectedFilePath}
              selectedFile={selectedFile}
              content={fileText}
              dirty={fileDirty}
              loading={fileQuery.isLoading}
              error={fileQuery.error?.message || saveFile.error?.message}
              saving={saveFile.isPending}
              onSelectFile={setSelectedFilePath}
              onChange={(next) => {
                setFileText(next);
                setFileDirty(true);
              }}
              onSave={saveCurrentFile}
            />
          ) : (
            <>
              <SpecKitChangePlan
                loading={draft.isLoading}
                error={draft.error?.message}
                changes={plan?.changes ?? []}
                counts={plan?.counts}
                hash={plan?.hash}
                jsonl={plan?.jsonl}
                warnings={draft.data?.warnings ?? plan?.warnings}
                allowDeletes={allowDeletes}
                onAllowDeletes={setAllowDeletes}
              />
              <section className="mt-5" data-testid="spec-kit-draft-review">
                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                  <h3 className="text-xs font-semibold uppercase text-neutral-500">
                    Staged Beads Draft
                  </h3>
                  <span className="text-xs text-neutral-500">
                    {draftItems.length} draft items · {draftSource === 'jsonl' ? 'JSONL upload' : 'Spec Kit'}
                  </span>
                </div>
                <div className="grid min-h-[420px] gap-4 lg:grid-cols-[minmax(360px,1fr)_minmax(280px,380px)]">
                  <div className="min-h-[360px] overflow-hidden rounded-md border border-neutral-200 dark:border-neutral-800">
                    {draft.isLoading ? (
                      <div className="p-4 text-xs text-neutral-500">Building staged beads…</div>
                    ) : draft.error ? (
                      <div className="p-4 text-xs text-rose-700 dark:text-rose-300">
                        {draft.error.message}
                      </div>
                    ) : (
                      <BeadsCascadeView
                        items={draftItems}
                        orderKey="modified"
                        onSelect={(item) => setSelectedDraftID(item.id)}
                      />
                    )}
                  </div>
                  <SpecKitDraftItemEditor
                    item={selectedDraft}
                    onChange={(patch) => selectedDraft && updateDraftItem(selectedDraft.id, patch)}
                  />
                </div>
              </section>
              <div className="mt-5 grid gap-5 lg:grid-cols-[minmax(220px,360px)_1fr]">
                <SpecKitStoryList feature={selected} />
                <SpecKitTaskList tasks={selected.tasks ?? []} />
              </div>
            </>
          )}
        </section>
      ) : files.length > 0 ? (
        <section className="min-h-0 flex-1 overflow-auto p-5" data-testid="spec-kit-detail">
          <header className="border-b border-neutral-200 pb-4 dark:border-neutral-800">
            <div className="text-[11px] font-semibold uppercase text-neutral-500">
              Bootstrap pack · Spec Kit
            </div>
            <h2 className="mt-1 text-base font-semibold tracking-tight">Workspace Files</h2>
            <div className="mt-1 text-xs text-neutral-500">
              Create a feature spec set from the toolbar when you are ready to generate draft Beads.
            </div>
          </header>
          <SpecKitFileEditor
            files={selectedFiles}
            selectedPath={selectedFilePath}
            selectedFile={selectedFile}
            content={fileText}
            dirty={fileDirty}
            loading={fileQuery.isLoading}
            error={fileQuery.error?.message || saveFile.error?.message}
            saving={saveFile.isPending}
            onSelectFile={setSelectedFilePath}
            onChange={(next) => {
              setFileText(next);
              setFileDirty(true);
            }}
            onSave={saveCurrentFile}
          />
        </section>
      ) : (
        <section
          className="flex min-h-0 flex-1 items-center justify-center p-6 text-xs text-neutral-500"
          data-testid="spec-kit-empty"
        >
          No Spec Kit features match the search. Create a new spec set from the toolbar.
        </section>
      )}
    </div>
  );
}

function SpecKitFeatureStrip({
  features,
  selectedID,
  onSelect,
  newFeatureTitle,
  onNewFeatureTitle,
  onCreateFeature,
  creatingFeature,
  createError,
  canCreateFeature,
}: {
  features: SpecKitFeature[];
  selectedID: string;
  onSelect: (id: string) => void;
  newFeatureTitle: string;
  onNewFeatureTitle: (value: string) => void;
  onCreateFeature: () => void;
  creatingFeature: boolean;
  createError?: string;
  canCreateFeature: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-5 py-3 text-xs dark:border-neutral-800">
      <div className="flex min-w-0 flex-1 items-center gap-2 overflow-x-auto">
        {features.map((feature) => (
          <button
            key={feature.id}
            type="button"
            data-testid={`spec-kit-feature-${feature.id}`}
            data-active={selectedID === feature.id}
            onClick={() => onSelect(feature.id)}
            className={cn(
              'inline-flex h-9 shrink-0 items-center gap-2 rounded-md border border-neutral-300 px-3 text-left text-xs text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-900',
              selectedID === feature.id &&
                'border-sky-600 bg-sky-50 text-sky-800 dark:border-sky-500 dark:bg-sky-950/30 dark:text-sky-200'
            )}
          >
            <span className="max-w-[16rem] truncate font-medium">{feature.title}</span>
            <span className="font-mono text-[11px] text-neutral-500">{feature.id}</span>
            <span className="text-neutral-500">{feature.task_count} tasks</span>
          </button>
        ))}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <label className="flex items-center gap-2">
          <span className="sr-only">New spec set</span>
          <input
            data-testid="spec-kit-new-title"
            value={newFeatureTitle}
            disabled={!canCreateFeature || creatingFeature}
            onChange={(e) => onNewFeatureTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onCreateFeature();
            }}
            placeholder="Feature title"
            className="h-8 w-48 rounded-md border border-neutral-300 bg-white px-2 text-xs disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-950"
          />
        </label>
        <button
          type="button"
          data-testid="spec-kit-create-feature"
          disabled={!canCreateFeature || !newFeatureTitle.trim() || creatingFeature}
          onClick={onCreateFeature}
          className="inline-flex h-8 items-center justify-center gap-2 rounded-md border border-neutral-300 px-3 text-xs font-medium text-neutral-700 hover:bg-neutral-100 disabled:cursor-wait disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-900"
        >
          <Plus className="h-3.5 w-3.5" />
          Create Spec Set
        </button>
        {createError && (
          <p className="max-w-64 text-xs text-rose-700 dark:text-rose-300">{createError}</p>
        )}
      </div>
    </div>
  );
}

function SpecKitFileEditor({
  files,
  selectedPath,
  selectedFile,
  content,
  dirty,
  loading,
  error,
  saving,
  onSelectFile,
  onChange,
  onSave,
}: {
  files: SpecKitWorkspaceFile[];
  selectedPath: string;
  selectedFile?: SpecKitWorkspaceFile;
  content: string;
  dirty: boolean;
  loading: boolean;
  error?: string;
  saving: boolean;
  onSelectFile: (path: string) => void;
  onChange: (content: string) => void;
  onSave: () => void;
}) {
  return (
    <section className="mt-5 min-h-[560px]" data-testid="spec-kit-file-editor">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-xs font-semibold uppercase text-neutral-500">Spec Kit Files</h3>
          <div className="mt-1 font-mono text-[11px] text-neutral-500">
            {selectedFile?.path ?? 'No file selected'}
          </div>
        </div>
        <button
          type="button"
          data-testid="spec-kit-save-file"
          disabled={!selectedPath || !dirty || loading || saving}
          onClick={onSave}
          className="inline-flex h-8 items-center gap-2 rounded-md bg-sky-700 px-3 text-xs font-medium text-white hover:bg-sky-800 disabled:cursor-wait disabled:opacity-60 dark:bg-sky-600 dark:hover:bg-sky-500"
        >
          <Save className="h-3.5 w-3.5" />
          {saving ? 'Saving' : dirty ? 'Save' : 'Saved'}
        </button>
      </div>
      <div className="flex min-h-0 flex-wrap gap-1 border-b border-neutral-200 pb-2 dark:border-neutral-800">
        {files.map((file) => (
          <button
            key={file.path}
            type="button"
            data-testid={`spec-kit-file-tab-${file.path}`}
            data-active={selectedPath === file.path}
            onClick={() => onSelectFile(file.path)}
            className={cn(
              'inline-flex h-8 max-w-[18rem] items-center gap-1 rounded-md border border-neutral-300 px-2 text-xs text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-900',
              selectedPath === file.path && 'border-sky-600 bg-sky-50 text-sky-800 dark:border-sky-500 dark:bg-sky-950/30 dark:text-sky-200'
            )}
            title={file.path}
          >
            <FileText className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{file.feature_id ? file.name : file.path}</span>
          </button>
        ))}
      </div>
      {error && <div className="mt-3 text-xs text-rose-700 dark:text-rose-300">{error}</div>}
      {loading ? (
        <div className="mt-4 text-xs text-neutral-500">Loading file…</div>
      ) : files.length === 0 ? (
        <div className="mt-4 text-xs text-neutral-500">No editable Spec Kit files found.</div>
      ) : (
        <textarea
          data-testid="spec-kit-file-content"
          value={content}
          onChange={(e) => onChange(e.target.value)}
          spellCheck={false}
          className="mt-3 h-[520px] w-full resize-y rounded-md border border-neutral-300 bg-white p-3 font-mono text-xs leading-5 text-neutral-900 outline-none focus:border-sky-500 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-100"
        />
      )}
    </section>
  );
}

function SpecKitChangePlan({
  loading,
  error,
  changes,
  counts,
  hash,
  jsonl,
  warnings,
  allowDeletes,
  onAllowDeletes,
}: {
  loading: boolean;
  error?: string;
  changes: Array<{
    action: 'create' | 'update' | 'delete';
    kind: string;
    title: string;
    id?: string;
    source_id?: string;
  }>;
  counts?: { create: number; update: number; delete: number };
  hash?: string;
  jsonl?: string;
  warnings?: string[];
  allowDeletes: boolean;
  onAllowDeletes: (value: boolean) => void;
}) {
  return (
    <section className="mt-5" data-testid="spec-kit-change-plan">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-xs font-semibold uppercase text-neutral-500">Beads Change Plan</h3>
        {counts && (
          <span className="text-xs text-neutral-500" data-testid="spec-kit-change-counts">
            {counts.create} create · {counts.update} update · {counts.delete} delete
          </span>
        )}
        {hash && (
          <span className="font-mono text-[11px] text-neutral-500" data-testid="spec-kit-plan-hash">
            {hash.slice(0, 19)}
          </span>
        )}
      </div>
      {loading ? (
        <div className="mt-2 text-xs text-neutral-500">Planning changes…</div>
      ) : error ? (
        <div className="mt-2 text-xs text-rose-700 dark:text-rose-300">{error}</div>
      ) : changes.length === 0 ? (
        <div className="mt-2 text-xs text-neutral-500">No Beads changes detected.</div>
      ) : (
        <div className="mt-2 divide-y divide-neutral-200 border-y border-neutral-200 dark:divide-neutral-800 dark:border-neutral-800">
          {changes.map((change, index) => (
            <div
              key={`${change.action}-${change.kind}-${change.id ?? change.source_id ?? index}`}
              className="grid grid-cols-[5rem_5rem_1fr] gap-3 py-2 text-xs"
              data-testid={`spec-kit-change-${index}`}
            >
              <span
                className={cn(
                  'rounded px-1.5 py-0.5 text-center font-medium',
                  change.action === 'create' &&
                    'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
                  change.action === 'update' &&
                    'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300',
                  change.action === 'delete' &&
                    'bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300'
                )}
              >
                {change.action}
              </span>
              <span className="text-neutral-500">{change.kind}</span>
              <span>
                {change.title}
                {(change.id || change.source_id) && (
                  <span className="ml-2 font-mono text-neutral-500">
                    {change.id || change.source_id}
                  </span>
                )}
              </span>
            </div>
          ))}
        </div>
      )}
      {warnings && warnings.length > 0 && (
        <div className="mt-2 text-xs text-amber-700 dark:text-amber-300">
          {warnings.join(' ')}
        </div>
      )}
      {counts && counts.delete > 0 && (
        <label className="mt-3 flex items-center gap-2 text-xs text-neutral-700 dark:text-neutral-300">
          <input
            type="checkbox"
            checked={allowDeletes}
            onChange={(e) => onAllowDeletes(e.target.checked)}
            className="h-3.5 w-3.5 rounded border-neutral-300"
          />
          Allow {counts.delete} stale delete{counts.delete === 1 ? '' : 's'}
        </label>
      )}
      {jsonl && (
        <details className="mt-3 text-xs">
          <summary className="cursor-pointer text-neutral-600 dark:text-neutral-300">
            JSONL manifest
          </summary>
          <pre className="mt-2 max-h-44 overflow-auto rounded border border-neutral-200 bg-neutral-50 p-3 text-[11px] leading-5 dark:border-neutral-800 dark:bg-neutral-950">
            {jsonl}
          </pre>
        </details>
      )}
    </section>
  );
}

function SpecKitDraftItemEditor({
  item,
  onChange,
}: {
  item: WorkItem | undefined;
  onChange: (patch: Partial<WorkItem>) => void;
}) {
  if (!item) {
    return (
      <aside
        className="rounded-md border border-neutral-200 p-4 text-xs text-neutral-500 dark:border-neutral-800"
        data-testid="spec-kit-draft-empty"
      >
        Select a staged bead to review it.
      </aside>
    );
  }
  return (
    <aside
      className="min-h-[360px] rounded-md border border-neutral-200 p-4 dark:border-neutral-800"
      data-testid="spec-kit-draft-editor"
    >
      <div className="mb-3">
        <div className="font-mono text-[11px] text-neutral-500" data-testid="spec-kit-draft-id">
          {item.id}
        </div>
        <div className="mt-1 inline-flex rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] font-medium uppercase text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
          {item.kind}
        </div>
      </div>
      <label className="block text-xs">
        <span className="font-medium text-neutral-700 dark:text-neutral-300">Title</span>
        <input
          data-testid="spec-kit-draft-title"
          value={item.title}
          onChange={(e) => onChange({ title: e.target.value })}
          className="mt-1 h-8 w-full rounded-md border border-neutral-300 bg-white px-2 text-xs dark:border-neutral-700 dark:bg-neutral-950"
        />
      </label>
      <label className="mt-3 block text-xs">
        <span className="font-medium text-neutral-700 dark:text-neutral-300">Description</span>
        <textarea
          data-testid="spec-kit-draft-description"
          value={item.description ?? ''}
          onChange={(e) => onChange({ description: e.target.value })}
          rows={8}
          className="mt-1 w-full resize-y rounded-md border border-neutral-300 bg-white px-2 py-2 text-xs leading-5 dark:border-neutral-700 dark:bg-neutral-950"
        />
      </label>
      <label className="mt-3 block text-xs">
        <span className="font-medium text-neutral-700 dark:text-neutral-300">Labels</span>
        <input
          data-testid="spec-kit-draft-labels"
          value={(item.labels ?? []).join(', ')}
          onChange={(e) =>
            onChange({
              labels: e.target.value
                .split(',')
                .map((label) => label.trim())
                .filter(Boolean),
            })
          }
          className="mt-1 h-8 w-full rounded-md border border-neutral-300 bg-white px-2 font-mono text-[11px] dark:border-neutral-700 dark:bg-neutral-950"
        />
      </label>
    </aside>
  );
}

function SpecKitStoryList({ feature }: { feature: SpecKitFeature }) {
  const stories = feature.spec.user_stories ?? [];
  return (
    <section>
      <h3 className="text-xs font-semibold uppercase text-neutral-500">User Stories</h3>
      <div className="mt-2 divide-y divide-neutral-200 border-y border-neutral-200 dark:divide-neutral-800 dark:border-neutral-800">
        {stories.length === 0 ? (
          <div className="py-3 text-xs text-neutral-500">No user stories parsed.</div>
        ) : (
          stories.map((story) => (
            <div key={story.id} className="py-3 text-xs">
              <div className="flex items-center gap-2">
                <span className="font-mono text-neutral-500">{story.id}</span>
                <span className="font-medium">{story.title}</span>
                {story.priority && (
                  <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
                    {story.priority}
                  </span>
                )}
              </div>
              {(story.acceptance_scenarios ?? []).slice(0, 2).map((scenario) => (
                <p key={scenario} className="mt-1 text-neutral-500">
                  {scenario}
                </p>
              ))}
            </div>
          ))
        )}
      </div>
    </section>
  );
}

function SpecKitTaskList({ tasks }: { tasks: SpecKitTask[] }) {
  return (
    <section>
      <h3 className="text-xs font-semibold uppercase text-neutral-500">Tasks</h3>
      <div className="mt-2 divide-y divide-neutral-200 border-y border-neutral-200 dark:divide-neutral-800 dark:border-neutral-800">
        {tasks.length === 0 ? (
          <div className="py-3 text-xs text-neutral-500">No tasks parsed.</div>
        ) : (
          tasks.map((task) => (
            <div
              key={task.id}
              className="grid grid-cols-[5rem_1fr_auto] gap-3 py-2 text-xs"
              data-testid={`spec-kit-task-${task.id}`}
            >
              <span className="font-mono text-neutral-500">{task.id}</span>
              <span>
                {task.title}
                <span className="mt-1 block text-neutral-500">
                  {[task.story_id, task.phase, task.line ? `line ${task.line}` : '']
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              </span>
              <span className="flex gap-1">
                {task.parallel && (
                  <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
                    P
                  </span>
                )}
                {task.done && (
                  <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
                    done
                  </span>
                )}
              </span>
            </div>
          ))
        )}
      </div>
    </section>
  );
}

// DeferDialog — date input + Confirm/Cancel. Empty date → caller skips
// the patch, so confirming an empty form is silently a no-op.
interface DeferDialogProps {
  ids: string[];
  onClose: () => void;
  onApply: (iso: string) => void;
}
function DeferDialog({ ids, onClose, onApply }: DeferDialogProps) {
  const [date, setDate] = useState<string>('');

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Defer work items"
      data-testid="refine-defer-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-sm rounded-lg border border-neutral-200 bg-white p-5 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="mb-3">
          <h2 className="text-base font-semibold">Defer</h2>
          <p className="mt-0.5 text-xs text-neutral-500">
            Marking <span data-testid="refine-defer-count">{ids.length}</span> item
            {ids.length === 1 ? '' : 's'} with a defer-until date.
          </p>
        </header>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs font-medium text-neutral-700 dark:text-neutral-300">
            Revisit on
          </span>
          <input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            data-testid="refine-defer-date"
            className="rounded border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-900"
          />
        </label>

        <footer className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="refine-defer-cancel"
            className="rounded border border-neutral-300 bg-white px-3 py-1 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onApply(date.trim())}
            data-testid="refine-defer-confirm"
            className="rounded bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-700"
          >
            Confirm
          </button>
        </footer>
      </div>
    </div>
  );
}

// DismissDialog — optional reason textarea + Confirm/Cancel. Reason is
// captured but not sent today (see applyDismiss TODO).
interface DismissDialogProps {
  ids: string[];
  onClose: () => void;
  onApply: (reason: string) => void;
}
function DismissDialog({ ids, onClose, onApply }: DismissDialogProps) {
  const [reason, setReason] = useState<string>('');

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Dismiss work items"
      data-testid="refine-dismiss-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-5 shadow-xl dark:border-neutral-800 dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="mb-3">
          <h2 className="text-base font-semibold">Dismiss</h2>
          <p className="mt-0.5 text-xs text-neutral-500">
            Cancelling <span data-testid="refine-dismiss-count">{ids.length}</span> item
            {ids.length === 1 ? '' : 's'}. Reason is optional.
          </p>
        </header>

        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs font-medium text-neutral-700 dark:text-neutral-300">
            Reason (optional)
          </span>
          <textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            data-testid="refine-dismiss-reason"
            rows={3}
            className="resize-none rounded border border-neutral-300 bg-white px-2 py-1 dark:border-neutral-700 dark:bg-neutral-900"
          />
        </label>

        <footer className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="refine-dismiss-cancel"
            className="rounded border border-neutral-300 bg-white px-3 py-1 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onApply(reason.trim())}
            data-testid="refine-dismiss-confirm"
            className="rounded bg-rose-600 px-3 py-1 text-sm font-medium text-white hover:bg-rose-700"
          >
            Confirm
          </button>
        </footer>
      </div>
    </div>
  );
}
