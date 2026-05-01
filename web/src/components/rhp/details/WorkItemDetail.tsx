// WorkItemDetail — RHP detail tab for a WorkItem (gm-root.22.5).
//
// Renders the same data + actions as WorkItemDrawer did, adapted to the
// RHP tab context:
//   - No overlay shell or close button — those live on the RhpShell tab.
//   - Props: { id: string } — workspace-prefixed bead id.
//   - Internal nav stack: clicking a Relationship pushes a new id onto
//     the stack; a Back button pops it. This keeps the UX parity with
//     the old drawer without opening a second tab for every edge click.
//   - Registration: WorkItemDetailRegistration (sibling file) registers
//     the 'workitem' kind on mount so the tab rail and popDetail work
//     without callers passing icon / label.
//
// Test-ids: work-item-drawer-* → workitem-detail-* per design-doc
// convention (docs/design/rhp.md).

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Check,
  Copy,
  Pencil,
  Plus,
  Terminal,
  Trash2,
} from 'lucide-react';
import { useWorkItem, useUpdateWorkItem, useWorkItems } from '@/hooks/useWorkItems';
import { useAgents, useSprints } from '@/hooks/useAgents';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Capability, useCapabilities } from '@/capabilities';
import { cn } from '@/lib/utils';
import { KIND_MILESTONE } from '@/types/core.gen';
import type { AgentRef, DefinitionOfDone, StateCategory, WorkItem } from '@/types/core.gen';
import type { Evidence } from '@/types/core.gen';
import { rendererFor } from '@/components/board/descriptionRenderers';
import { canEdit } from '@/components/board/canEdit';
import { MilestoneChildrenPanel } from '@/components/board/MilestoneChildrenPanel';
import { NewSessionDialog } from '@/components/sessions/NewSessionDialog';
import { workItemsKeys } from '@/hooks/useWorkItems';
import {
  WorkItemBreadcrumb,
  buildWorkItemBreadcrumb,
  type WorkItemBreadcrumbCrumb,
} from './WorkItemBreadcrumb';

export interface WorkItemDetailProps {
  /** Workspace-prefixed bead id, e.g. `gemba/gemba/gm-1` or `gm-1`. */
  id: string;
}

export function WorkItemDetail({ id }: WorkItemDetailProps) {
  // Internal nav stack: seed from the prop id so the first render
  // already has a currentId. When the prop id changes (RHP
  // kind-replace), reset the stack.
  const [stack, setStack] = useState<string[]>([id]);

  useEffect(() => {
    setStack([id]);
  }, [id]);

  const currentId = stack[stack.length - 1] ?? id;
  const canGoBack = stack.length > 1;

  const goBack = useCallback(() => {
    setStack((s) => (s.length > 1 ? s.slice(0, -1) : s));
  }, []);

  const navigateTo = useCallback((targetId: string) => {
    setStack((s) => [...s, targetId]);
  }, []);

  return (
    <WorkItemDetailBody
      id={currentId}
      canGoBack={canGoBack}
      onBack={goBack}
      onNavigate={navigateTo}
    />
  );
}

interface WorkItemDetailBodyProps {
  id: string;
  canGoBack: boolean;
  onBack: () => void;
  onNavigate: (id: string) => void;
}

function WorkItemDetailBody({ id, canGoBack, onBack, onNavigate }: WorkItemDetailBodyProps) {
  const { data, isLoading, error } = useWorkItem(id);
  const queryClient = useQueryClient();
  const breadcrumb = useMemo(() => {
    if (!data) return [];
    const cachedItems = queryClient.getQueryData<WorkItem[]>(workItemsKeys.list()) ?? [];
    return buildWorkItemBreadcrumb(cachedItems, data);
  }, [data, queryClient]);
  const navigateBreadcrumb = useCallback(
    (crumb: WorkItemBreadcrumbCrumb) => {
      if (crumb.id !== id) onNavigate(crumb.id);
    },
    [id, onNavigate]
  );

  return (
    <>
      <DetailHeader
        id={id}
        title={data?.title ?? ''}
        canGoBack={canGoBack}
        onBack={onBack}
        item={data}
        breadcrumb={breadcrumb}
        onNavigateBreadcrumb={navigateBreadcrumb}
      />
      <div className="flex-1 overflow-y-auto px-4 pb-10" data-testid="workitem-detail-scroll">
        {isLoading ? (
          <div className="py-8 text-sm text-neutral-500" data-testid="workitem-detail-loading">
            Loading bead…
          </div>
        ) : error ? (
          <div
            className="py-8 text-sm text-red-600 dark:text-red-400"
            data-testid="workitem-detail-error"
          >
            {error.message}
          </div>
        ) : data ? (
          <BeadBody item={data} onNavigate={onNavigate} />
        ) : null}
      </div>
    </>
  );
}

function DetailHeader({
  id,
  title,
  canGoBack,
  onBack,
  item,
  breadcrumb,
  onNavigateBreadcrumb,
}: {
  id: string;
  title: string;
  canGoBack: boolean;
  onBack: () => void;
  item: WorkItem | undefined;
  breadcrumb: WorkItemBreadcrumbCrumb[];
  onNavigateBreadcrumb: (crumb: WorkItemBreadcrumbCrumb) => void;
}) {
  const [copied, setCopied] = useState(false);
  const [dispatchOpen, setDispatchOpen] = useState(false);
  const navigate = useNavigate();
  const copyId = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      setCopied(false);
    }
  }, [id]);

  return (
    <div
      className="flex items-start gap-2 border-b border-neutral-200 px-4 py-3 dark:border-neutral-800"
      data-testid="workitem-detail-header"
    >
      {canGoBack ? (
        <button
          type="button"
          onClick={onBack}
          className="mt-1 rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          aria-label="Back"
          data-testid="workitem-detail-back"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
      ) : null}
      <div className="min-w-0 flex-1">
        <WorkItemBreadcrumb crumbs={breadcrumb} onNavigate={onNavigateBreadcrumb} />
        <div className="truncate text-sm font-semibold text-neutral-900 dark:text-neutral-100">
          {title || id}
        </div>
        <div className="mt-0.5 flex items-center gap-1 text-xs text-neutral-500">
          <span className="font-mono" data-testid="workitem-detail-id">
            {id}
          </span>
          <button
            type="button"
            onClick={copyId}
            className="rounded p-0.5 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
            aria-label="Copy bead ID"
            data-testid="workitem-detail-copy"
          >
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </button>
        </div>
      </div>
      <DispatchButton item={item} onOpen={() => setDispatchOpen(true)} />
      {item && dispatchOpen ? (
        <NewSessionDialog
          open={dispatchOpen}
          onClose={() => setDispatchOpen(false)}
          prefilledBeadId={item.id}
          onStarted={() => navigate('/sessions')}
        />
      ) : null}
    </div>
  );
}

// DispatchButton renders the header's Start-session shortcut for a
// leaf WorkItem. Disabled when the bead is closed/canceled, before the
// item has loaded, or when no OrchestrationPlane is bound.
function DispatchButton({
  item,
  onOpen,
}: {
  item: WorkItem | undefined;
  onOpen: () => void;
}) {
  const { orchestrationPlane } = useCapabilities();
  const isTerminal =
    item?.state_category === 'completed' || item?.state_category === 'canceled';
  const noPlane = !orchestrationPlane;
  const disabled = !item || isTerminal || noPlane;
  const title = !item
    ? 'Loading…'
    : isTerminal
      ? 'Bead is closed; cannot dispatch a session.'
      : noPlane
        ? 'No orchestration plane bound; cannot dispatch a session.'
        : 'Start a session for this bead';
  return (
    <button
      type="button"
      onClick={onOpen}
      disabled={disabled}
      title={title}
      data-testid="workitem-detail-dispatch"
      className={cn(
        'mt-1 inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs font-medium',
        disabled
          ? 'cursor-not-allowed border-neutral-200 bg-neutral-50 text-neutral-400 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-600'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-50 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800'
      )}
    >
      <Terminal className="h-3 w-3" aria-hidden />
      Dispatch
    </button>
  );
}

type DetailTab = 'description' | 'edges' | 'dod' | 'sprint' | 'activity' | 'extensions';

function BeadBody({ item, onNavigate }: { item: WorkItem; onNavigate: (id: string) => void }) {
  const grouped = useMemo(() => groupRelationships(item), [item]);
  const customGroups = useMemo(() => groupCustom(item.custom), [item.custom]);
  const timestamps = useMemo(() => extractTimestamps(item), [item]);
  const sprintBudget = useMemo(() => extractSprintBudget(item.custom), [item.custom]);
  const closeReason = useMemo(() => extractCloseReason(item.custom), [item.custom]);
  const { workPlane } = useCapabilities();
  const DescriptionRenderer = rendererFor(workPlane?.description_format);
  const adaptorReadOnly = workPlane?.read_only === true;
  const fieldTypeByName = useMemo(() => {
    const out = new Map<string, string>();
    for (const fe of workPlane?.field_extensions ?? []) out.set(fe.name, fe.type);
    return out;
  }, [workPlane?.field_extensions]);
  const editCtx = { item, adaptorReadOnly };
  const update = useUpdateWorkItem();

  const hasExtensions = customGroups.length > 0;
  const [tab, setTab] = useState<DetailTab>('description');
  useEffect(() => {
    if (tab === 'extensions' && !hasExtensions) setTab('description');
  }, [tab, hasExtensions]);

  return (
    <div className="space-y-6 pt-4">
      <TitleEditor
        item={item}
        canEdit={canEdit('title', editCtx)}
        saving={update.isPending}
        onSave={(text) => update.mutate({ id: item.id, patch: { title: text } })}
      />

      <Section title="Overview" testid="section-overview">
        <div className="flex flex-wrap items-center gap-2">
          <StatusEditor
            item={item}
            stateMap={workPlane?.state_map ?? {}}
            canEdit={canEdit('status', editCtx)}
            disabled={update.isPending}
            onChange={(status, stateCategory) =>
              update.mutate({
                id: item.id,
                patch: { status, state_category: stateCategory },
              })
            }
          />
          <Chip label="type" value={item.kind} />
          <PriorityEditor
            item={item}
            canEdit={canEdit('priority', editCtx)}
            disabled={update.isPending}
            onChange={(p) => update.mutate({ id: item.id, patch: { priority: p } })}
          />
          <CloseButton
            item={item}
            adaptorReadOnly={adaptorReadOnly}
            disabled={update.isPending}
            onClose={() =>
              update.mutate({ id: item.id, patch: { state_category: 'completed' } })
            }
          />
        </div>
        {update.isError ? (
          <div className="mt-2 text-xs text-rose-600 dark:text-rose-400" data-testid="work-item-edit-error">
            {update.error?.message ?? 'Update failed.'}
          </div>
        ) : null}
        <DefRow label="Assignee">
          <AgentEditor
            agent={item.assignee ?? null}
            canEdit={canEdit('assignee', editCtx)}
            saving={update.isPending}
            testidPrefix="work-item-assignee"
            onSave={(next) => update.mutate({ id: item.id, patch: { assignee: next } })}
          />
        </DefRow>
        <DefRow label="Owner">
          <AgentEditor
            agent={item.owner ?? null}
            canEdit={canEdit('owner', editCtx)}
            saving={update.isPending}
            testidPrefix="work-item-owner"
            onSave={(next) => update.mutate({ id: item.id, patch: { owner: next } })}
          />
        </DefRow>
        <DefRow label="Labels">
          <LabelsEditor
            item={item}
            canEdit={canEdit('labels', editCtx)}
            saving={update.isPending}
            onSave={(labels) => update.mutate({ id: item.id, patch: { labels } })}
          />
        </DefRow>
      </Section>

      {item.kind === KIND_MILESTONE ? <MilestoneChildrenPanelMount milestone={item} /> : null}

      <DetailTabBar tab={tab} onChange={setTab} hasExtensions={hasExtensions} />

      {tab === 'description' ? (
        <>
          <Section title="Description" testid="section-description">
            <DescriptionEditor
              item={item}
              renderer={DescriptionRenderer}
              canEdit={canEdit('description', editCtx)}
              saving={update.isPending}
              onSave={(text) => update.mutate({ id: item.id, patch: { description: text } })}
            />
          </Section>
          {closeReason ? (
            <Section title="Close reason" testid="section-close-reason">
              <pre className="whitespace-pre-wrap break-words font-sans text-sm text-neutral-800 dark:text-neutral-200">
                {closeReason}
              </pre>
            </Section>
          ) : null}
          <Capability has="has_evidence">
            <Section title="Evidence" testid="section-evidence">
              {item.evidence && item.evidence.length > 0 ? (
                <ul className="space-y-2">
                  {item.evidence.map((e) => (
                    <EvidenceRow key={e.id} evidence={e} />
                  ))}
                </ul>
              ) : item.state_category === 'completed' ? (
                <div
                  className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200"
                  data-testid="evidence-missing-banner"
                  role="status"
                >
                  Evidence required for closed items, but none is attached.
                </div>
              ) : (
                <Muted>No evidence attached.</Muted>
              )}
            </Section>
          </Capability>
        </>
      ) : null}

      {tab === 'edges' ? (
        <Section title="Relationships" testid="section-relationships">
          <RelGroup label="blocks" rows={grouped.blocks} onNavigate={onNavigate} />
          <RelGroup label="blocked by" rows={grouped.blockedBy} onNavigate={onNavigate} />
          <RelGroup label="parent" rows={grouped.parent} onNavigate={onNavigate} />
          <RelGroup label="children" rows={grouped.children} onNavigate={onNavigate} />
          <RelGroup label="relates to" rows={grouped.relatesTo} onNavigate={onNavigate} />
          {grouped.extension.length > 0 ? (
            <RelGroup
              label="extension edges"
              rows={grouped.extension}
              onNavigate={onNavigate}
              ext
            />
          ) : null}
          {!grouped.any ? <Muted>No relationships.</Muted> : null}
        </Section>
      ) : null}

      {tab === 'dod' ? (
        <Section title="Definition of Done" testid="section-dod">
          <DoDBanner />
          <DoDSynthBanner dod={item.dod ?? null} />
          <DoDEditor
            dod={item.dod ?? null}
            renderer={DescriptionRenderer}
            canEdit={canEdit('dod', editCtx)}
            saving={update.isPending}
            onSave={(next) => update.mutate({ id: item.id, patch: { dod: next } })}
          />
        </Section>
      ) : null}

      {tab === 'sprint' ? (
        <Section title="Sprint & budget" testid="section-sprint">
          <DefRow label="Sprint">
            <SprintEditor
              sprintId={item.sprint_id ?? null}
              canEdit={canEdit('sprint_id', editCtx)}
              saving={update.isPending}
              onSave={(next) => update.mutate({ id: item.id, patch: { sprint_id: next } })}
            />
          </DefRow>
          {sprintBudget ? (
            <div className="mt-2 space-y-1 text-sm">
              <DefRow label="Budget used">
                <span className="font-mono">
                  {sprintBudget.used} / {sprintBudget.limit}
                </span>
              </DefRow>
              <DefRow label="Thresholds">
                <span className="font-mono text-xs text-neutral-500">
                  inform={sprintBudget.inform} warn={sprintBudget.warn} stop={sprintBudget.stop}
                </span>
              </DefRow>
            </div>
          ) : null}
          {!sprintBudget ? <div className="mt-2 text-xs text-neutral-500">No budget set.</div> : null}
        </Section>
      ) : null}

      {tab === 'activity' ? (
        <>
          <Section title="Timestamps" testid="section-timestamps">
            <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
              <Timestamp label="Created" ts={timestamps.created} />
              <Timestamp label="Started" ts={timestamps.started} />
              <Timestamp label="Updated" ts={timestamps.updated} />
              <Timestamp label="Closed" ts={timestamps.closed} />
            </dl>
          </Section>
          <Section title="Derived signals" testid="section-derived">
            {item.derived ? (
              <div className="flex flex-wrap gap-2 text-xs">
                <DerivedPill label="agent-claimable" on={item.derived.agent_claimable} />
                <DerivedPill
                  label="human-action-required"
                  on={item.derived.human_action_required}
                />
                <DerivedPill label="review-pending" on={item.derived.review_pending} />
              </div>
            ) : (
              <Muted>Not populated by the adaptor.</Muted>
            )}
          </Section>
        </>
      ) : null}

      {tab === 'extensions' && hasExtensions ? (
        <Section title="Extension fields" testid="section-custom">
          <div className="space-y-4">
            {customGroups.map((g) => (
              <div key={g.namespace}>
                <div className="mb-1 font-mono text-xs uppercase tracking-wide text-neutral-500">
                  {g.namespace}
                </div>
                <dl className="space-y-1">
                  {g.entries.map(([k, v]) => (
                    <CustomRow
                      key={k}
                      fullKey={k}
                      value={v}
                      fieldType={fieldTypeByName.get(k)}
                      renderer={DescriptionRenderer}
                      canEdit={canEdit('custom', editCtx)}
                      saving={update.isPending}
                      onSave={(next) =>
                        update.mutate({
                          id: item.id,
                          patch: { custom: { ...(item.custom ?? {}), [k]: next } },
                        })
                      }
                    />
                  ))}
                </dl>
              </div>
            ))}
          </div>
        </Section>
      ) : null}
    </div>
  );
}

// DetailTabBar — horizontal tab strip below the Overview strip.
function DetailTabBar({
  tab,
  onChange,
  hasExtensions,
}: {
  tab: DetailTab;
  onChange: (t: DetailTab) => void;
  hasExtensions: boolean;
}) {
  const tabs: Array<{ id: DetailTab; label: string }> = [
    { id: 'description', label: 'Description' },
    { id: 'edges', label: 'Edges' },
    { id: 'dod', label: 'DoD' },
    { id: 'sprint', label: 'Sprint' },
    { id: 'activity', label: 'Activity' },
  ];
  if (hasExtensions) tabs.push({ id: 'extensions', label: 'Extensions' });
  return (
    <div
      role="tablist"
      aria-label="Work item details"
      data-testid="workitem-detail-tabs"
      className="-mx-1 flex flex-wrap items-center gap-0.5 border-b border-neutral-200 dark:border-neutral-800"
    >
      {tabs.map((t) => {
        const active = tab === t.id;
        return (
          <button
            key={t.id}
            role="tab"
            type="button"
            aria-selected={active}
            data-active={active}
            data-testid={`detail-tab-${t.id}`}
            onClick={() => onChange(t.id)}
            className={cn(
              '-mb-px border-b-2 px-3 py-1.5 text-xs',
              active
                ? 'border-neutral-900 text-neutral-900 dark:border-neutral-100 dark:text-neutral-100'
                : 'border-transparent text-neutral-500 hover:text-neutral-800 dark:hover:text-neutral-200'
            )}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

function DoDSynthBanner({ dod }: { dod: DefinitionOfDone | null }) {
  if (!dod) {
    return (
      <div
        data-testid="work-item-dod-synth-banner"
        data-state="missing"
        className="mb-2 rounded border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-900 dark:border-sky-900/40 dark:bg-sky-950/30 dark:text-sky-200"
      >
        No DoD authored. Click <span className="font-semibold">Edit</span> to
        capture acceptance criteria the agent will defend.
      </div>
    );
  }
  if (typeof dod.version === 'string' && dod.version.startsWith('synthesized')) {
    return (
      <div
        data-testid="work-item-dod-synth-banner"
        data-state="synthesized"
        className="mb-2 rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200"
      >
        Synthesized from kind / labels (server default). Click{' '}
        <span className="font-semibold">Edit</span> to author the criteria you
        want this bead to defend.
      </div>
    );
  }
  return null;
}

function DoDBanner() {
  return (
    <div
      data-testid="work-item-dod-banner"
      className="mb-2 rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200"
    >
      Informational only — the DoD documents expected outcomes but does not
      gate state transitions. Closing a bead does not validate these criteria.
    </div>
  );
}

function TitleEditor({
  item,
  canEdit,
  saving,
  onSave,
}: {
  item: WorkItem;
  canEdit: boolean;
  saving: boolean;
  onSave: (text: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(item.title);
  useEffect(() => {
    if (!editing) setDraft(item.title);
  }, [item.title, editing]);

  if (editing) {
    return (
      <div className="space-y-2" data-testid="work-item-title-editing">
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          autoFocus
          className="w-full rounded border border-neutral-300 bg-white px-2 py-1 text-base font-semibold dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
        />
        <div className="flex justify-end gap-2 text-xs">
          <button
            type="button"
            onClick={() => {
              setEditing(false);
              setDraft(item.title);
            }}
            disabled={saving}
            className="rounded border border-neutral-300 px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              const trimmed = draft.trim();
              if (trimmed.length > 0 && trimmed !== item.title) {
                onSave(trimmed);
              }
              setEditing(false);
            }}
            disabled={saving}
            className="rounded border border-sky-500 bg-sky-500 px-2 py-1 text-white hover:bg-sky-600 disabled:opacity-60"
            data-testid="work-item-title-save"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-start gap-2">
      <h2 className="flex-1 text-base font-semibold text-neutral-900 dark:text-neutral-100">
        {item.title}
      </h2>
      {canEdit ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="mt-1 rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          aria-label="Edit title"
          data-testid="work-item-title-edit"
        >
          <Pencil className="h-3 w-3" />
        </button>
      ) : null}
    </div>
  );
}

function PriorityEditor({
  item,
  canEdit,
  disabled,
  onChange,
}: {
  item: WorkItem;
  canEdit: boolean;
  disabled: boolean;
  onChange: (priority: number | null) => void;
}) {
  if (!canEdit) {
    return item.priority != null ? (
      <Chip label="P" value={String(item.priority)} />
    ) : null;
  }
  const value = item.priority ?? '';
  return (
    <label className="inline-flex items-center gap-1 text-xs" data-testid="work-item-priority-editor">
      <span className="text-neutral-500">P</span>
      <select
        value={value}
        disabled={disabled}
        onChange={(e) => {
          const raw = e.target.value;
          onChange(raw === '' ? null : Number(raw));
        }}
        className={cn(
          'rounded border border-neutral-300 bg-white px-1.5 py-0.5 text-xs font-mono',
          'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100',
          disabled && 'opacity-60'
        )}
      >
        <option value="">—</option>
        {[0, 1, 2, 3, 4].map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
    </label>
  );
}

function LabelsEditor({
  item,
  canEdit,
  saving,
  onSave,
}: {
  item: WorkItem;
  canEdit: boolean;
  saving: boolean;
  onSave: (labels: string[]) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState((item.labels ?? []).join(', '));
  useEffect(() => {
    if (!editing) setDraft((item.labels ?? []).join(', '));
  }, [item.labels, editing]);

  if (editing) {
    return (
      <div className="space-y-2" data-testid="work-item-labels-editing">
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          autoFocus
          placeholder="comma,separated,labels"
          className="w-full rounded border border-neutral-300 bg-white px-2 py-1 text-xs font-mono dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
        />
        <div className="flex justify-end gap-2 text-xs">
          <button
            type="button"
            onClick={() => {
              setEditing(false);
              setDraft((item.labels ?? []).join(', '));
            }}
            disabled={saving}
            className="rounded border border-neutral-300 px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              const next = draft
                .split(',')
                .map((s) => s.trim())
                .filter((s) => s.length > 0);
              onSave(next);
              setEditing(false);
            }}
            disabled={saving}
            className="rounded border border-sky-500 bg-sky-500 px-2 py-1 text-white hover:bg-sky-600 disabled:opacity-60"
            data-testid="work-item-labels-save"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-start gap-2">
      <div className="flex-1">
        {item.labels && item.labels.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {item.labels.map((l) => (
              <span
                key={l}
                className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-xs text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
              >
                {l}
              </span>
            ))}
          </div>
        ) : (
          <Muted>none</Muted>
        )}
      </div>
      {canEdit ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          aria-label="Edit labels"
          data-testid="work-item-labels-edit"
        >
          <Pencil className="h-3 w-3" />
        </button>
      ) : null}
    </div>
  );
}

function StatusEditor({
  item,
  stateMap,
  canEdit,
  disabled,
  onChange,
}: {
  item: WorkItem;
  stateMap: Record<string, StateCategory>;
  canEdit: boolean;
  disabled: boolean;
  onChange: (status: string, stateCategory: StateCategory) => void;
}) {
  const tokens = Object.keys(stateMap).sort();
  if (!canEdit || tokens.length === 0) {
    return (
      <>
        <Chip label="status" value={item.status} />
        <Chip label="state" value={item.state_category} />
      </>
    );
  }
  return (
    <label className="inline-flex items-center gap-1 text-xs" data-testid="work-item-status-editor">
      <span className="text-neutral-500">status</span>
      <select
        value={item.status}
        disabled={disabled}
        onChange={(e) => {
          const next = e.target.value;
          onChange(next, stateMap[next] ?? item.state_category);
        }}
        className={cn(
          'rounded border border-neutral-300 bg-white px-1.5 py-0.5 text-xs font-mono',
          'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100',
          disabled && 'opacity-60'
        )}
      >
        {!tokens.includes(item.status) ? (
          <option value={item.status}>{item.status}</option>
        ) : null}
        {tokens.map((t) => (
          <option key={t} value={t}>
            {t}
          </option>
        ))}
      </select>
    </label>
  );
}

function CloseButton({
  item,
  adaptorReadOnly,
  disabled,
  onClose,
}: {
  item: WorkItem;
  adaptorReadOnly: boolean;
  disabled: boolean;
  onClose: () => void;
}) {
  if (adaptorReadOnly) return null;
  if (item.assignee?.agent_kind !== 'human') return null;
  if (item.state_category === 'completed' || item.state_category === 'canceled') return null;
  return (
    <button
      type="button"
      onClick={onClose}
      disabled={disabled}
      className={cn(
        'ml-auto inline-flex items-center gap-1 rounded border border-emerald-300 bg-emerald-50 px-2 py-0.5 text-xs text-emerald-800',
        'hover:bg-emerald-100',
        'dark:border-emerald-700 dark:bg-emerald-950 dark:text-emerald-200 dark:hover:bg-emerald-900',
        disabled && 'opacity-60'
      )}
      data-testid="work-item-close-button"
    >
      Close
    </button>
  );
}

function DescriptionEditor({
  item,
  renderer: Renderer,
  canEdit,
  saving,
  onSave,
}: {
  item: WorkItem;
  renderer: React.ComponentType<{ source: string }>;
  canEdit: boolean;
  saving: boolean;
  onSave: (text: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(item.description ?? '');

  useEffect(() => {
    if (!editing) setDraft(item.description ?? '');
  }, [item.description, editing]);

  if (editing) {
    return (
      <div className="space-y-2" data-testid="work-item-description-editing">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={8}
          className="w-full rounded border border-neutral-300 bg-white p-2 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          autoFocus
        />
        <div className="flex justify-end gap-2 text-xs">
          <button
            type="button"
            onClick={() => {
              setEditing(false);
              setDraft(item.description ?? '');
            }}
            disabled={saving}
            className="rounded border border-neutral-300 px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              onSave(draft);
              setEditing(false);
            }}
            disabled={saving}
            className="rounded border border-sky-500 bg-sky-500 px-2 py-1 text-white hover:bg-sky-600 disabled:opacity-60"
            data-testid="work-item-description-save"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {item.description ? (
        <Renderer source={item.description} />
      ) : (
        <Muted>No description.</Muted>
      )}
      {canEdit ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="inline-flex items-center gap-1 text-[11px] text-neutral-500 hover:text-neutral-800 dark:hover:text-neutral-200"
          data-testid="work-item-description-edit"
        >
          <Pencil className="h-3 w-3" />
          {item.description ? 'Edit' : 'Add description'}
        </button>
      ) : null}
    </div>
  );
}

function MilestoneChildrenPanelMount({ milestone }: { milestone: WorkItem }) {
  const { data: allItems = [] } = useWorkItems();
  return <MilestoneChildrenPanel milestone={milestone} allItems={allItems} />;
}

function Section({
  title,
  testid,
  children,
}: {
  title: string;
  testid: string;
  children: React.ReactNode;
}) {
  return (
    <section data-testid={testid}>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        {title}
      </h3>
      <div className="space-y-2">{children}</div>
    </section>
  );
}

function DefRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="w-20 shrink-0 text-xs uppercase tracking-wide text-neutral-500">
        {label}
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-xs text-neutral-500">{children}</span>;
}

function Chip({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded bg-neutral-100 px-2 py-0.5 text-xs dark:bg-neutral-800">
      <span className="text-neutral-500">{label}</span>
      <span className="font-mono text-neutral-900 dark:text-neutral-100">{value}</span>
    </span>
  );
}

function AgentEditor({
  agent,
  canEdit,
  saving,
  testidPrefix,
  onSave,
}: {
  agent: AgentRef | null;
  canEdit: boolean;
  saving: boolean;
  testidPrefix: string;
  onSave: (next: AgentRef | null) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [id, setId] = useState(agent?.id ?? '');
  const [name, setName] = useState(agent?.name ?? '');
  const [kind, setKind] = useState<'human' | 'agent'>(agent?.agent_kind ?? 'human');
  const { data: roster } = useAgents();
  useEffect(() => {
    if (!editing) {
      setId(agent?.id ?? '');
      setName(agent?.name ?? '');
      setKind(agent?.agent_kind ?? 'human');
    }
  }, [agent, editing]);

  if (editing) {
    return (
      <div className="space-y-2" data-testid={`${testidPrefix}-editing`}>
        {roster && roster.length > 0 ? (
          <select
            value={id}
            data-testid={`${testidPrefix}-picker`}
            onChange={(e) => {
              const picked = (roster ?? []).find((a) => a.id === e.target.value);
              if (picked) {
                setId(picked.id);
                setName(picked.name);
                setKind(picked.agent_kind);
              } else {
                setId('');
              }
            }}
            className="w-full rounded border border-neutral-300 bg-white px-1.5 py-0.5 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          >
            <option value="">— pick from roster (or type below) —</option>
            {roster.map((a) => (
              <option key={a.id} value={a.id}>
                [{a.agent_kind}] {a.name} ({a.id})
              </option>
            ))}
          </select>
        ) : null}
        <div className="flex flex-wrap items-center gap-1 text-xs">
          <select
            value={kind}
            onChange={(e) => setKind(e.target.value as 'human' | 'agent')}
            className="rounded border border-neutral-300 bg-white px-1.5 py-0.5 font-mono dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          >
            <option value="human">human</option>
            <option value="agent">agent</option>
          </select>
          <input
            value={id}
            onChange={(e) => setId(e.target.value)}
            placeholder="id"
            className="flex-1 rounded border border-neutral-300 bg-white px-2 py-0.5 font-mono dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          />
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="name"
            className="flex-1 rounded border border-neutral-300 bg-white px-2 py-0.5 font-mono dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          />
        </div>
        <div className="flex justify-end gap-2 text-xs">
          <button
            type="button"
            onClick={() => setEditing(false)}
            disabled={saving}
            className="rounded border border-neutral-300 px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              const trimmedId = id.trim();
              if (!trimmedId) {
                onSave(null);
              } else {
                onSave({
                  id: trimmedId,
                  name: name.trim() || trimmedId,
                  agent_kind: kind,
                });
              }
              setEditing(false);
            }}
            disabled={saving}
            className="rounded border border-sky-500 bg-sky-500 px-2 py-1 text-white hover:bg-sky-600 disabled:opacity-60"
            data-testid={`${testidPrefix}-save`}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-start gap-2">
      <div className="flex-1">
        <AgentPill agent={agent} />
      </div>
      {canEdit ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          aria-label="Edit"
          data-testid={`${testidPrefix}-edit`}
        >
          <Pencil className="h-3 w-3" />
        </button>
      ) : null}
    </div>
  );
}

function SprintEditor({
  sprintId,
  canEdit,
  saving,
  onSave,
}: {
  sprintId: string | null;
  canEdit: boolean;
  saving: boolean;
  onSave: (next: string | null) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(sprintId ?? '');
  const { data: roster } = useSprints();
  useEffect(() => {
    if (!editing) setDraft(sprintId ?? '');
  }, [sprintId, editing]);

  if (editing) {
    return (
      <div className="space-y-2" data-testid="work-item-sprint-editing">
        {roster && roster.length > 0 ? (
          <select
            value={draft}
            data-testid="work-item-sprint-picker"
            onChange={(e) => setDraft(e.target.value)}
            className="w-full rounded border border-neutral-300 bg-white px-2 py-1 font-mono text-sm dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          >
            <option value="">— pick a sprint (or type below) —</option>
            {roster.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.id})
              </option>
            ))}
          </select>
        ) : null}
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          autoFocus
          placeholder="sprint-id (blank to clear)"
          className="w-full rounded border border-neutral-300 bg-white px-2 py-1 font-mono text-sm dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
        />
        <div className="flex justify-end gap-2 text-xs">
          <button
            type="button"
            onClick={() => setEditing(false)}
            disabled={saving}
            className="rounded border border-neutral-300 px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              const trimmed = draft.trim();
              onSave(trimmed.length > 0 ? trimmed : null);
              setEditing(false);
            }}
            disabled={saving}
            className="rounded border border-sky-500 bg-sky-500 px-2 py-1 text-white hover:bg-sky-600 disabled:opacity-60"
            data-testid="work-item-sprint-save"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-start gap-2">
      <div className="flex-1">
        {sprintId ? (
          <span className="font-mono text-sm">{sprintId}</span>
        ) : (
          <Muted>No sprint.</Muted>
        )}
      </div>
      {canEdit ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          aria-label="Edit sprint"
          data-testid="work-item-sprint-edit"
        >
          <Pencil className="h-3 w-3" />
        </button>
      ) : null}
    </div>
  );
}

function DoDEditor({
  dod,
  renderer: Renderer,
  canEdit,
  saving,
  onSave,
}: {
  dod: DefinitionOfDone | null;
  renderer: React.ComponentType<{ source: string }>;
  canEdit: boolean;
  saving: boolean;
  onSave: (next: DefinitionOfDone | null) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [criteria, setCriteria] = useState<string[]>(dod?.acceptance_criteria ?? []);
  const [notes, setNotes] = useState(dod?.notes ?? '');
  const [version, setVersion] = useState(dod?.version ?? '');
  useEffect(() => {
    if (!editing) {
      setCriteria(dod?.acceptance_criteria ?? []);
      setNotes(dod?.notes ?? '');
      setVersion(dod?.version ?? '');
    }
  }, [dod, editing]);

  const startEditing = () => {
    setCriteria(dod?.acceptance_criteria?.length ? dod.acceptance_criteria : ['']);
    setNotes(dod?.notes ?? '');
    setVersion(dod?.version ?? '');
    setEditing(true);
  };

  const updateCriterion = (i: number, text: string) => {
    setCriteria((prev) => prev.map((c, idx) => (idx === i ? text : c)));
  };
  const addCriterion = () => setCriteria((prev) => [...prev, '']);
  const removeCriterion = (i: number) =>
    setCriteria((prev) => prev.filter((_, idx) => idx !== i));
  const moveCriterion = (i: number, delta: -1 | 1) => {
    setCriteria((prev) => {
      const j = i + delta;
      if (j < 0 || j >= prev.length) return prev;
      const next = prev.slice();
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });
  };

  if (editing) {
    return (
      <div className="space-y-3" data-testid="work-item-dod-editing">
        <div className="space-y-1">
          <div className="text-xs text-neutral-500">Acceptance criteria</div>
          <ul className="space-y-1" data-testid="work-item-dod-criteria-list">
            {criteria.map((c, i) => (
              <li
                key={i}
                className="flex items-center gap-1"
                data-testid={`work-item-dod-criterion-${i}`}
              >
                <input
                  value={c}
                  onChange={(e) => updateCriterion(i, e.target.value)}
                  placeholder="criterion"
                  className="flex-1 rounded border border-neutral-300 bg-white px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
                />
                <button
                  type="button"
                  onClick={() => moveCriterion(i, -1)}
                  disabled={i === 0}
                  aria-label="Move up"
                  data-testid={`work-item-dod-criterion-${i}-up`}
                  className="rounded p-1 text-neutral-500 hover:bg-neutral-100 disabled:opacity-30 dark:hover:bg-neutral-800"
                >
                  <ArrowUp className="h-3 w-3" />
                </button>
                <button
                  type="button"
                  onClick={() => moveCriterion(i, 1)}
                  disabled={i === criteria.length - 1}
                  aria-label="Move down"
                  data-testid={`work-item-dod-criterion-${i}-down`}
                  className="rounded p-1 text-neutral-500 hover:bg-neutral-100 disabled:opacity-30 dark:hover:bg-neutral-800"
                >
                  <ArrowDown className="h-3 w-3" />
                </button>
                <button
                  type="button"
                  onClick={() => removeCriterion(i)}
                  aria-label="Remove criterion"
                  data-testid={`work-item-dod-criterion-${i}-remove`}
                  className="rounded p-1 text-neutral-500 hover:bg-rose-100 hover:text-rose-700 dark:hover:bg-rose-950 dark:hover:text-rose-300"
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </li>
            ))}
          </ul>
          <button
            type="button"
            onClick={addCriterion}
            className="inline-flex items-center gap-1 rounded border border-neutral-300 px-2 py-0.5 text-xs text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-900"
            data-testid="work-item-dod-add-criterion"
          >
            <Plus className="h-3 w-3" /> Add criterion
          </button>
        </div>
        <label className="block text-xs text-neutral-500">
          Notes
          <textarea
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            rows={4}
            className="mt-1 w-full rounded border border-neutral-300 bg-white p-2 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          />
        </label>
        <label className="block text-xs text-neutral-500">
          Version
          <input
            value={version}
            onChange={(e) => setVersion(e.target.value)}
            className="mt-1 w-full rounded border border-neutral-300 bg-white px-2 py-1 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          />
        </label>
        <div className="flex justify-end gap-2 text-xs">
          <button
            type="button"
            onClick={() => setEditing(false)}
            disabled={saving}
            className="rounded border border-neutral-300 px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              const list = criteria.map((s) => s.trim()).filter((s) => s.length > 0);
              const n = notes.trim();
              const v = version.trim();
              if (list.length === 0 && !n && !v) {
                onSave(null);
              } else {
                onSave({
                  acceptance_criteria: list,
                  notes: n || undefined,
                  version: v || undefined,
                });
              }
              setEditing(false);
            }}
            disabled={saving}
            className="rounded border border-sky-500 bg-sky-500 px-2 py-1 text-white hover:bg-sky-600 disabled:opacity-60"
            data-testid="work-item-dod-save"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {dod ? (
        <div className="space-y-2 text-sm">
          <ul className="list-disc space-y-1 pl-5">
            {dod.acceptance_criteria.map((c, i) => (
              <li key={i}>{c}</li>
            ))}
          </ul>
          {dod.notes ? <Renderer source={dod.notes} /> : null}
          {dod.version ? (
            <div className="text-xs text-neutral-500">version {dod.version}</div>
          ) : null}
        </div>
      ) : (
        <Muted>No DoD declared.</Muted>
      )}
      {canEdit ? (
        <button
          type="button"
          onClick={startEditing}
          className="inline-flex items-center gap-1 text-[11px] text-neutral-500 hover:text-neutral-800 dark:hover:text-neutral-200"
          data-testid="work-item-dod-edit"
        >
          <Pencil className="h-3 w-3" />
          {dod ? 'Edit' : 'Add DoD'}
        </button>
      ) : null}
    </div>
  );
}

function AgentPill({ agent }: { agent: WorkItem['assignee'] | null }) {
  if (!agent) return <Muted>unassigned</Muted>;
  return (
    <span className="inline-flex items-center gap-2 text-sm">
      <span
        className={cn(
          'rounded px-1.5 py-0.5 text-xs',
          agent.agent_kind === 'agent'
            ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-200'
            : 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200'
        )}
      >
        {agent.agent_kind}
      </span>
      <span className="font-mono text-xs">{agent.name || agent.id}</span>
      {agent.role ? <span className="text-xs text-neutral-500">· {agent.role}</span> : null}
      {agent.dialect ? <span className="text-xs text-neutral-500">· {agent.dialect}</span> : null}
    </span>
  );
}

function DerivedPill({ label, on }: { label: string; on: boolean }) {
  return (
    <span
      className={cn(
        'rounded px-2 py-0.5',
        on
          ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200'
          : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
      )}
    >
      {on ? '●' : '○'} {label}
    </span>
  );
}

function Timestamp({ label, ts }: { label: string; ts: string | null }) {
  return (
    <>
      <dt className="text-xs uppercase tracking-wide text-neutral-500">{label}</dt>
      <dd className="font-mono text-xs text-neutral-700 dark:text-neutral-300">
        {ts ?? <span className="text-neutral-500">—</span>}
      </dd>
    </>
  );
}

interface RelRow {
  id: string;
  hint?: string;
}

function RelGroup({
  label,
  rows,
  onNavigate,
  ext = false,
}: {
  label: string;
  rows: RelRow[];
  onNavigate: (id: string) => void;
  ext?: boolean;
}) {
  if (rows.length === 0) return null;
  return (
    <div data-testid={`relgroup-${label.replace(/\s+/g, '-')}`}>
      <div className="mb-1 text-xs uppercase tracking-wide text-neutral-500">{label}</div>
      <ul className="flex flex-wrap gap-1">
        {rows.map((r, i) => (
          <li key={`${r.id}-${i}`}>
            <button
              type="button"
              onClick={() => onNavigate(r.id)}
              className="inline-flex items-center gap-1 rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-xs text-neutral-800 hover:bg-neutral-200 dark:bg-neutral-800 dark:text-neutral-200 dark:hover:bg-neutral-700"
            >
              {r.id}
              {r.hint ? <span className="text-[10px] text-neutral-500">{r.hint}</span> : null}
              {ext ? (
                <span className="rounded bg-amber-200/60 px-1 text-[9px] uppercase text-amber-900 dark:bg-amber-900/40 dark:text-amber-100">
                  ext
                </span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function EvidenceRow({ evidence }: { evidence: Evidence }) {
  // gm-t4af / DD-13: synthesized entries are auto-derived from
  // git/PRs/work-history; operator-curated entries are not. Show a
  // small "auto" pill so the operator can tell them apart.
  const synthesized = evidence.payload?.synthesized === true;
  return (
    <li
      className="rounded border border-neutral-200 p-2 text-sm dark:border-neutral-800"
      data-testid={`evidence-row-${evidence.id}`}
    >
      <div className="flex items-center gap-2 text-xs text-neutral-500">
        <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono dark:bg-neutral-800">
          {evidence.kind}
        </span>
        {synthesized ? (
          <span
            className="rounded bg-sky-100 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-sky-700 dark:bg-sky-950 dark:text-sky-300"
            title="Auto-derived from git / PRs / work-history"
            data-testid="evidence-synth-marker"
          >
            auto
          </span>
        ) : null}
        <span className="font-mono">{evidence.source}</span>
        {evidence.ref ? <EvidenceRef evidence={evidence} /> : null}
        <span className="ml-auto font-mono">{formatTs(evidence.captured_at)}</span>
      </div>
      {evidence.summary ? <div className="mt-1">{evidence.summary}</div> : null}
    </li>
  );
}

function EvidenceRef({ evidence }: { evidence: Evidence }) {
  const href = resolveEvidenceHref(evidence);
  const ref = evidence.ref ?? '';
  if (!href) {
    return <span className="truncate font-mono">{ref}</span>;
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="truncate font-mono text-sky-600 underline-offset-2 hover:underline dark:text-sky-400"
      data-testid={`evidence-ref-${evidence.id}`}
    >
      {ref}
    </a>
  );
}

function resolveEvidenceHref(evidence: Evidence): string | null {
  const ref = evidence.ref?.trim();
  if (!ref) return null;
  if (/^https?:\/\//i.test(ref)) return ref;
  if (evidence.kind === 'url') return ref;
  return null;
}

type CustomEditorKind = 'string' | 'number' | 'boolean' | 'markdown' | 'json';

function CustomRow({
  fullKey,
  value,
  fieldType,
  renderer: Renderer,
  canEdit: canEditField,
  saving,
  onSave,
}: {
  fullKey: string;
  value: unknown;
  fieldType: string | undefined;
  renderer: React.ComponentType<{ source: string }>;
  canEdit: boolean;
  saving: boolean;
  onSave: (next: unknown) => void;
}) {
  const short = fullKey.includes(':') ? fullKey.split(':').slice(1).join(':') : fullKey;
  const kind = customEditorKind(fieldType, value);
  const testBase = `custom-${fullKey}`;
  const [editing, setEditing] = useState(false);

  return (
    <div className="flex gap-2 text-sm" data-testid={`${testBase}-row`}>
      <dt className="w-40 shrink-0 truncate font-mono text-xs text-neutral-500" title={fullKey}>
        {short}
      </dt>
      <dd className="min-w-0 flex-1">
        {editing ? (
          <CustomRowEditor
            fullKey={fullKey}
            value={value}
            kind={kind}
            saving={saving}
            onCancel={() => setEditing(false)}
            onSave={(next) => {
              setEditing(false);
              onSave(next);
            }}
          />
        ) : (
          <div className="flex items-start gap-1">
            <div className="min-w-0 flex-1">
              {kind === 'markdown' && typeof value === 'string' ? (
                <Renderer source={value} />
              ) : kind === 'boolean' && typeof value === 'boolean' ? (
                <span
                  data-testid={`${testBase}-value`}
                  className="font-mono text-xs text-neutral-800 dark:text-neutral-200"
                >
                  {value ? 'true' : 'false'}
                </span>
              ) : (
                <pre
                  data-testid={`${testBase}-value`}
                  className="whitespace-pre-wrap break-words font-mono text-xs text-neutral-800 dark:text-neutral-200"
                >
                  {renderCustomValue(value)}
                </pre>
              )}
            </div>
            {canEditField ? (
              <button
                type="button"
                onClick={() => setEditing(true)}
                disabled={saving}
                data-testid={`${testBase}-edit`}
                aria-label={`Edit ${fullKey}`}
                className="shrink-0 rounded p-0.5 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
              >
                <Pencil className="h-3 w-3" />
              </button>
            ) : null}
          </div>
        )}
      </dd>
    </div>
  );
}

function customEditorKind(fieldType: string | undefined, value: unknown): CustomEditorKind {
  if (fieldType) {
    const t = fieldType.toLowerCase();
    if (t === 'string' || t === 'text') return 'string';
    if (t === 'number' || t === 'int' || t === 'integer' || t === 'float') return 'number';
    if (t === 'boolean' || t === 'bool') return 'boolean';
    if (t === 'markdown' || t === 'md') return 'markdown';
  }
  if (typeof value === 'string') return 'string';
  if (typeof value === 'number') return 'number';
  if (typeof value === 'boolean') return 'boolean';
  return 'json';
}

function CustomRowEditor({
  fullKey,
  value,
  kind,
  saving,
  onCancel,
  onSave,
}: {
  fullKey: string;
  value: unknown;
  kind: CustomEditorKind;
  saving: boolean;
  onCancel: () => void;
  onSave: (next: unknown) => void;
}) {
  const testBase = `custom-${fullKey}`;
  const initial = initialEditorText(kind, value);
  const [draft, setDraft] = useState(initial);
  const [error, setError] = useState<string | null>(null);

  const commit = () => {
    const parsed = parseEditorValue(kind, draft);
    if (!parsed.ok) {
      setError(parsed.error);
      return;
    }
    if (stableEqual(parsed.value, value)) {
      onCancel();
      return;
    }
    onSave(parsed.value);
  };

  if (kind === 'boolean') {
    const checked = draft === 'true';
    return (
      <div className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={checked}
          disabled={saving}
          data-testid={`${testBase}-input`}
          onChange={(e) => setDraft(e.target.checked ? 'true' : 'false')}
        />
        <SaveCancel testBase={testBase} saving={saving} onSave={commit} onCancel={onCancel} />
      </div>
    );
  }

  const isMultiline = kind === 'markdown' || kind === 'json';
  return (
    <div className="space-y-1">
      {isMultiline ? (
        <textarea
          value={draft}
          disabled={saving}
          data-testid={`${testBase}-input`}
          onChange={(e) => setDraft(e.target.value)}
          rows={kind === 'json' ? 4 : 6}
          className="w-full rounded border border-neutral-300 bg-white px-2 py-1 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
        />
      ) : (
        <input
          type={kind === 'number' ? 'number' : 'text'}
          value={draft}
          disabled={saving}
          data-testid={`${testBase}-input`}
          onChange={(e) => setDraft(e.target.value)}
          className="w-full rounded border border-neutral-300 bg-white px-2 py-1 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
        />
      )}
      {error ? (
        <div className="text-xs text-rose-600 dark:text-rose-400" data-testid={`${testBase}-error`}>
          {error}
        </div>
      ) : null}
      <SaveCancel testBase={testBase} saving={saving} onSave={commit} onCancel={onCancel} />
    </div>
  );
}

function SaveCancel({
  testBase,
  saving,
  onSave,
  onCancel,
}: {
  testBase: string;
  saving: boolean;
  onSave: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        onClick={onSave}
        disabled={saving}
        data-testid={`${testBase}-save`}
        className="rounded bg-neutral-900 px-2 py-0.5 text-xs text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
      >
        Save
      </button>
      <button
        type="button"
        onClick={onCancel}
        disabled={saving}
        data-testid={`${testBase}-cancel`}
        className="rounded px-2 py-0.5 text-xs text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:text-neutral-400 dark:hover:bg-neutral-800"
      >
        Cancel
      </button>
    </div>
  );
}

function initialEditorText(kind: CustomEditorKind, value: unknown): string {
  if (kind === 'string' || kind === 'markdown') {
    return typeof value === 'string' ? value : '';
  }
  if (kind === 'number') {
    return typeof value === 'number' ? String(value) : '';
  }
  if (kind === 'boolean') {
    return value === true ? 'true' : 'false';
  }
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

type ParseResult = { ok: true; value: unknown } | { ok: false; error: string };

function parseEditorValue(kind: CustomEditorKind, draft: string): ParseResult {
  if (kind === 'string' || kind === 'markdown') return { ok: true, value: draft };
  if (kind === 'number') {
    if (draft.trim() === '') return { ok: true, value: null };
    const n = Number(draft);
    if (!Number.isFinite(n)) return { ok: false, error: 'Not a number' };
    return { ok: true, value: n };
  }
  if (kind === 'boolean') {
    return { ok: true, value: draft === 'true' };
  }
  if (draft.trim() === '') return { ok: true, value: null };
  try {
    return { ok: true, value: JSON.parse(draft) };
  } catch (e) {
    return { ok: false, error: (e as Error).message };
  }
}

function stableEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  try {
    return JSON.stringify(a) === JSON.stringify(b);
  } catch {
    return false;
  }
}

// ---------- derivations ----------

interface GroupedRelationships {
  blocks: RelRow[];
  blockedBy: RelRow[];
  parent: RelRow[];
  children: RelRow[];
  relatesTo: RelRow[];
  extension: RelRow[];
  any: boolean;
}

function groupRelationships(item: WorkItem): GroupedRelationships {
  const out: GroupedRelationships = {
    blocks: [],
    blockedBy: [],
    parent: [],
    children: [],
    relatesTo: [],
    extension: [],
    any: false,
  };
  const selfId = item.id;
  for (const r of item.relationships ?? []) {
    if (r.kind === 'blocks') {
      if (r.from === selfId) out.blocks.push({ id: r.to });
      else if (r.to === selfId) out.blockedBy.push({ id: r.from });
    } else if (r.kind === 'parent_child') {
      if (r.from === selfId) out.children.push({ id: r.to });
      else if (r.to === selfId) out.parent.push({ id: r.from });
    } else if (r.kind === 'relates_to') {
      const other = r.from === selfId ? r.to : r.from;
      out.relatesTo.push({ id: other });
    }
  }
  const ext = extractExtensionEdges(item.custom);
  out.extension = ext;
  out.any =
    out.blocks.length +
      out.blockedBy.length +
      out.parent.length +
      out.children.length +
      out.relatesTo.length +
      out.extension.length >
    0;
  return out;
}

function extractExtensionEdges(custom: Record<string, unknown> | undefined): RelRow[] {
  if (!custom) return [];
  const rows: RelRow[] = [];
  for (const key of ['beads:dependencies', 'beads:dependents']) {
    const raw = custom[key];
    if (!Array.isArray(raw)) continue;
    for (const entry of raw) {
      if (typeof entry === 'string') {
        rows.push({ id: entry, hint: keyHint(key) });
      } else if (entry && typeof entry === 'object') {
        const obj = entry as Record<string, unknown>;
        const id = pickStringField(obj, ['issue_id', 'id', 'to']);
        const kind = typeof obj.kind === 'string' ? obj.kind : undefined;
        if (id) rows.push({ id, hint: kind ?? keyHint(key) });
      }
    }
  }
  return rows;
}

function keyHint(key: string): string {
  return key === 'beads:dependents' ? '←' : '→';
}

function pickStringField(obj: Record<string, unknown>, keys: string[]): string | undefined {
  for (const k of keys) {
    const v = obj[k];
    if (typeof v === 'string' && v.length > 0) return v;
  }
  return undefined;
}

interface CustomGroup {
  namespace: string;
  entries: [string, unknown][];
}

function groupCustom(custom: Record<string, unknown> | undefined): CustomGroup[] {
  if (!custom) return [];
  const suppress = new Set([
    'beads:dependencies',
    'beads:dependents',
    'beads:close_reason',
    'beads:sprint',
    'beads:budget',
    'beads:started_at',
    'beads:closed_at',
  ]);
  const byNs = new Map<string, [string, unknown][]>();
  for (const [k, v] of Object.entries(custom)) {
    if (suppress.has(k)) continue;
    const ns = k.includes(':') ? k.split(':')[0] : '(no-namespace)';
    const list = byNs.get(ns) ?? [];
    list.push([k, v]);
    byNs.set(ns, list);
  }
  return Array.from(byNs.entries())
    .map(([namespace, entries]) => ({ namespace, entries: entries.sort() }))
    .sort((a, b) => a.namespace.localeCompare(b.namespace));
}

interface Timestamps {
  created: string | null;
  started: string | null;
  updated: string | null;
  closed: string | null;
}

function extractTimestamps(item: WorkItem): Timestamps {
  const c = item.custom ?? {};
  return {
    created: formatTs(item.created_at),
    updated: formatTs(item.updated_at),
    started: formatCustomTs(c['beads:started_at']),
    closed: formatCustomTs(c['beads:closed_at']),
  };
}

interface SprintBudget {
  limit: number;
  used: number;
  inform: number;
  warn: number;
  stop: number;
}

function extractSprintBudget(custom: Record<string, unknown> | undefined): SprintBudget | null {
  if (!custom) return null;
  const raw = custom['beads:budget'];
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  const pick = (k: string) => (typeof o[k] === 'number' ? (o[k] as number) : undefined);
  const limit = pick('limit');
  const used = pick('used');
  const inform = pick('inform');
  const warn = pick('warn');
  const stop = pick('stop');
  if (
    limit === undefined ||
    used === undefined ||
    inform === undefined ||
    warn === undefined ||
    stop === undefined
  ) {
    return null;
  }
  return { limit, used, inform, warn, stop };
}

function extractCloseReason(custom: Record<string, unknown> | undefined): string | null {
  if (!custom) return null;
  const raw = custom['beads:close_reason'];
  return typeof raw === 'string' && raw.length > 0 ? raw : null;
}

function formatTs(iso: string | null | undefined): string | null {
  if (!iso) return null;
  return iso;
}

function formatCustomTs(v: unknown): string | null {
  return typeof v === 'string' && v.length > 0 ? v : null;
}

function renderCustomValue(v: unknown): string {
  if (v === null || v === undefined) return '—';
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
