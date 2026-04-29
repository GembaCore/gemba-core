// EpicDetail (gm-root.22.6): RHP detail-tab content for Epic items.
//
// This is the successor to EpicDrawer — same content, minus the Dialog
// shell (the RHP provides the panel, tab rail, and close affordance).
//
// Registered as kind 'epic' via useRegisterDetailContent. The /board/:epicId
// route handler calls popDetail({kind: 'epic', id: epicId}) on mount so the
// URL grows the ?rhp=epic:<id> segment via the RHP codec.
//
// Props: { id: string } — workspace-prefixed epic id (e.g. "gemba/gemba/gm-e1").
//
// Test-ids use the epic-detail-* prefix (migrated from epic-drawer-*).

import { useCallback, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Check, Copy, Layers, Play, Plus, Send, Terminal } from 'lucide-react';
import { useUpdateWorkItem, useWorkItem, useWorkItems } from '@/hooks/useWorkItems';
import { useCapabilities } from '@/capabilities';
import { NewSessionDialog } from '@/components/sessions/NewSessionDialog';
import { NewWorkItemDialog } from '@/components/board/NewWorkItemDialog';
import { cn } from '@/lib/utils';
import {
  STATE_CATEGORIES,
  type StateCategory,
  type WorkItem,
} from '@/types/core.gen';
import { rendererFor } from '@/components/board/descriptionRenderers';
import { epicChildren } from '@/components/board/epicHierarchy';
import { EpicMilestoneDropdown } from '@/components/board/EpicMilestoneDropdown';
import { useRegisterDetailContent } from '@/components/rhp/RhpDetail';
import { useRhp } from '@/components/rhp/RhpContext';

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

// Module-scope render function so useRegisterDetailContent's dependency
// on reg.render is stable across re-renders — prevents the registration
// useEffect from re-firing (and causing a bumpDetailReg loop) every time
// EpicDetailRegistration's parent re-renders. Pattern mirrors
// RecommendOrderDetail / WorkItemDetailRegistration.
function renderEpicDetail(id: string) {
  return <EpicDetail id={id} />;
}

// Registration component: mount once in the app tree (e.g. AppShell or
// BoardPage) to register the 'epic' kind with the RHP registry.
export function EpicDetailRegistration() {
  useRegisterDetailContent({
    kind: 'epic',
    icon: Layers,
    label: 'Epic',
    render: renderEpicDetail,
  });
  return null;
}

export interface EpicDetailProps {
  id: string;
}

export function EpicDetail({ id }: EpicDetailProps) {
  const { data: epicItem, isLoading, error } = useWorkItem(id);
  const { data: allItems } = useWorkItems();
  const children = useMemo(
    () => (allItems ? epicChildren(allItems, id) : []),
    [allItems, id]
  );
  const { popDetail } = useRhp();

  const onOpenChild = useCallback(
    (childId: string) => {
      // Pop the workitem kind in the RHP. The workitem kind is
      // registered by gm-root.22.5 (WorkItemDetail); if it isn't
      // registered yet the RHP renders a placeholder body which is
      // acceptable and harmless.
      popDetail({ kind: 'workitem', id: childId });
    },
    [popDetail]
  );

  return (
    <div className="flex h-full flex-col">
      <DetailHeader id={id} epic={epicItem} />
      <div className="flex-1 overflow-y-auto px-6 pb-10" data-testid="epic-detail-scroll">
        {isLoading ? (
          <div className="py-8 text-sm text-neutral-500" data-testid="epic-detail-loading">
            Loading epic…
          </div>
        ) : error ? (
          <div
            className="py-8 text-sm text-red-600 dark:text-red-400"
            data-testid="epic-detail-error"
          >
            {(error as Error).message}
          </div>
        ) : epicItem ? (
          <Body
            epic={epicItem}
            children={children}
            items={allItems ?? []}
            onOpenChild={onOpenChild}
          />
        ) : null}
      </div>
    </div>
  );
}

function DetailHeader({ id, epic }: { id: string; epic: WorkItem | undefined }) {
  const [copied, setCopied] = useState(false);
  const copyId = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      setCopied(false);
    }
  }, [id]);
  const title = epic?.title ?? '';
  return (
    <div className="flex items-start gap-2 border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
      <div className="min-w-0 flex-1">
        <div className="truncate text-base font-semibold text-neutral-900 dark:text-neutral-100">
          {title || id}
        </div>
        <div className="mt-1 flex items-center gap-2 text-xs text-neutral-500">
          <span className="rounded bg-violet-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-violet-700 dark:bg-violet-950 dark:text-violet-300">
            epic
          </span>
          <span className="font-mono" data-testid="epic-detail-id">
            {id}
          </span>
          <button
            type="button"
            onClick={copyId}
            className="rounded p-0.5 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
            aria-label="Copy epic ID"
            data-testid="epic-detail-copy"
          >
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </button>
        </div>
      </div>
      {epic ? <EpicActions epic={epic} /> : null}
    </div>
  );
}

// EpicActions mirrors EpicDrawer's actions per ui-spec §5.6 / gm-vzy.
// The close button is absent — the RHP tab rail provides that affordance.
function EpicActions({ epic }: { epic: WorkItem }) {
  const mutation = useUpdateWorkItem();
  const navigate = useNavigate();
  const { orchestrationPlane } = useCapabilities();
  const [dispatchOpen, setDispatchOpen] = useState(false);
  const [newChildOpen, setNewChildOpen] = useState(false);
  const agentClaimable = epic.derived?.agent_claimable === true;
  const isStaged = epic.state_category === 'staged';
  const isStarted = epic.state_category === 'started';
  const isTerminal = epic.state_category === 'completed' || epic.state_category === 'canceled';

  const newChildDisabledReason = isTerminal
    ? 'Epic is closed; reopen before adding children.'
    : null;

  const dispatchDisabledReason = isTerminal
    ? 'Bead is closed; cannot dispatch a session.'
    : !orchestrationPlane
      ? 'No orchestration plane bound; cannot dispatch a session.'
      : null;

  const stageDisabledReason = !agentClaimable
    ? 'Not agent-claimable: check derived signals or open escalations.'
    : isStaged
      ? 'Epic is already staged.'
      : mutation.isPending
        ? 'Mutation in flight.'
        : null;
  const startDisabledReason = !agentClaimable
    ? 'Not agent-claimable: check derived signals or open escalations.'
    : isStarted
      ? 'Workers already started.'
      : !isStaged
        ? 'Stage the epic before starting workers.'
        : mutation.isPending
          ? 'Mutation in flight.'
          : null;

  const onStage = useCallback(() => {
    if (stageDisabledReason) return;
    mutation.mutate({ id: epic.id, patch: { state_category: 'staged' } });
  }, [epic.id, mutation, stageDisabledReason]);

  const onStart = useCallback(() => {
    if (startDisabledReason) return;
    mutation.mutate({ id: epic.id, patch: { state_category: 'started' } });
  }, [epic.id, mutation, startDisabledReason]);

  return (
    <div className="mt-0.5 flex items-center gap-1">
      <ActionButton
        label="Stage"
        icon={<Send className="h-3 w-3" />}
        onClick={onStage}
        disabledReason={stageDisabledReason}
        testid="epic-detail-stage"
      />
      <ActionButton
        label="Start workers"
        icon={<Play className="h-3 w-3" />}
        onClick={onStart}
        disabledReason={startDisabledReason}
        testid="epic-detail-start"
      />
      <ActionButton
        label="Dispatch"
        icon={<Terminal className="h-3 w-3" />}
        onClick={() => setDispatchOpen(true)}
        disabledReason={dispatchDisabledReason}
        testid="epic-detail-dispatch"
      />
      <ActionButton
        label="New child"
        icon={<Plus className="h-3 w-3" />}
        onClick={() => setNewChildOpen(true)}
        disabledReason={newChildDisabledReason}
        testid="epic-detail-new-child"
      />
      {dispatchOpen ? (
        <NewSessionDialog
          open={dispatchOpen}
          onClose={() => setDispatchOpen(false)}
          prefilledBeadId={epic.id}
          onStarted={() => navigate('/sessions')}
        />
      ) : null}
      {newChildOpen ? (
        <NewWorkItemDialog
          open={newChildOpen}
          onClose={() => setNewChildOpen(false)}
          parentId={epic.id}
          parentTitle={epic.title}
        />
      ) : null}
    </div>
  );
}

function ActionButton({
  label,
  icon,
  onClick,
  disabledReason,
  testid,
}: {
  label: string;
  icon: React.ReactNode;
  onClick: () => void;
  disabledReason: string | null;
  testid: string;
}) {
  const disabled = disabledReason !== null;
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      title={disabledReason ?? label}
      data-testid={testid}
      data-disabled={disabled ? 'true' : undefined}
      className={cn(
        'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs font-medium',
        disabled
          ? 'cursor-not-allowed border-neutral-200 bg-neutral-50 text-neutral-400 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-600'
          : 'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-50 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800'
      )}
    >
      {icon}
      {label}
    </button>
  );
}

interface BodyProps {
  epic: WorkItem;
  children: WorkItem[];
  items: WorkItem[];
  onOpenChild?: (id: string) => void;
}

function Body({ epic, children, items, onOpenChild }: BodyProps) {
  const { workPlane } = useCapabilities();
  const DescriptionRenderer = rendererFor(workPlane?.description_format);

  const byState: Record<StateCategory, WorkItem[]> = {
    backlog: [],
    unstarted: [],
    staged: [],
    started: [],
    completed: [],
    canceled: [],
  };
  for (const c of children) byState[c.state_category]?.push(c);

  return (
    <div className="space-y-6 pt-4">
      <section data-testid="epic-section-state">
        <div className="text-xs uppercase tracking-wide text-neutral-500">State</div>
        <div className="mt-1 text-sm">
          {epic.status} · <span className="font-mono">{epic.state_category}</span>
        </div>
      </section>

      <section data-testid="epic-section-milestone">
        <div className="mb-1 text-xs uppercase tracking-wide text-neutral-500">Milestone</div>
        <EpicMilestoneDropdown epic={epic} items={items} />
      </section>

      {epic.description ? (
        <section data-testid="epic-section-description">
          <div className="mb-1 text-xs uppercase tracking-wide text-neutral-500">
            Description
          </div>
          <DescriptionRenderer source={epic.description} />
        </section>
      ) : null}

      <section data-testid="epic-section-children">
        <div className="mb-2 text-xs uppercase tracking-wide text-neutral-500">
          Children ({children.length})
        </div>
        {children.length === 0 ? (
          <div className="text-xs text-neutral-500">No child items.</div>
        ) : (
          <div className="space-y-3">
            {STATE_CATEGORIES.map((cat) => {
              const rows = byState[cat];
              if (rows.length === 0) return null;
              return (
                <div key={cat} data-testid={`epic-children-${cat}`}>
                  <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-600 dark:text-neutral-400">
                    {STATE_LABELS[cat]} ({rows.length})
                  </div>
                  <ul className="space-y-1">
                    {rows.map((c) => (
                      <li key={c.id}>
                        <ChildRow item={c} onOpenChild={onOpenChild} />
                      </li>
                    ))}
                  </ul>
                </div>
              );
            })}
          </div>
        )}
      </section>
    </div>
  );
}

function ChildRow({
  item,
  onOpenChild,
}: {
  item: WorkItem;
  onOpenChild?: (id: string) => void;
}) {
  const interactive = !!onOpenChild;
  return (
    <button
      type="button"
      disabled={!interactive}
      onClick={interactive ? () => onOpenChild(item.id) : undefined}
      className={cn(
        'flex w-full items-center gap-2 rounded border border-neutral-200 px-2 py-1 text-left text-xs',
        'dark:border-neutral-800',
        interactive
          ? 'hover:border-neutral-300 hover:bg-neutral-50 dark:hover:border-neutral-700 dark:hover:bg-neutral-900'
          : 'cursor-default'
      )}
    >
      <span className="font-mono text-[11px] text-neutral-500">{item.id}</span>
      <span className="truncate text-neutral-800 dark:text-neutral-200">{item.title}</span>
      <span className="ml-auto rounded bg-neutral-100 px-1 text-[10px] font-mono text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400">
        {item.kind}
      </span>
    </button>
  );
}
