// EpicDrawer (gm-root.6 / ui-spec §5.6): drill-in for Epic cards.
//
// MVP scope: header + child-WorkItem list grouped by state. Stage /
// Start workers actions are deferred — they need server-side mutation
// routes (none in M1/M2 yet) plus the X-GEMBA-Confirm nonce flow
// (gm-e4.4). Filed as follow-ups, not blockers for this slice.

import { useCallback, useEffect, useMemo, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { Check, Copy, X } from 'lucide-react';
import { useWorkItem, useWorkItems } from '@/hooks/useWorkItems';
import { useCapabilities } from '@/capabilities';
import { cn } from '@/lib/utils';
import {
  STATE_CATEGORIES,
  type StateCategory,
  type WorkItem,
} from '@/types/core.gen';
import { rendererFor } from './descriptionRenderers';
import { epicChildren } from './epicHierarchy';

const STATE_LABELS: Record<StateCategory, string> = {
  backlog: 'Backlog',
  unstarted: 'Unstarted',
  started: 'Started',
  completed: 'Completed',
  canceled: 'Canceled',
};

export interface EpicDrawerProps {
  openId: string | null;
  onClose: () => void;
  // onOpenChild lets the parent route a child WorkItem click into the
  // existing single-WorkItem drawer (WorkItemDrawer). Optional — when
  // omitted, child rows are inert.
  onOpenChild?: (id: string) => void;
}

export function EpicDrawer({ openId, onClose, onOpenChild }: EpicDrawerProps) {
  // Mirror WorkItemDrawer's nav-stack pattern in a slimmed-down form: the
  // drawer doesn't navigate between Epics here, but parent-initiated
  // openId changes still need the safe reset.
  const [currentId, setCurrentId] = useState<string | null>(openId);
  useEffect(() => setCurrentId(openId), [openId]);

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) onClose();
    },
    [onClose]
  );

  return (
    <Dialog.Root open={!!openId} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay
          className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm"
          data-testid="epic-drawer-overlay"
        />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed right-0 top-0 z-50 flex h-full w-full max-w-2xl flex-col border-l border-neutral-200 bg-white shadow-xl outline-none dark:border-neutral-800 dark:bg-neutral-950"
          data-testid="epic-drawer-content"
        >
          {currentId ? <EpicDrawerBody id={currentId} onOpenChild={onOpenChild} /> : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

interface EpicDrawerBodyProps {
  id: string;
  onOpenChild?: (id: string) => void;
}

function EpicDrawerBody({ id, onOpenChild }: EpicDrawerBodyProps) {
  const { data: epicItem, isLoading, error } = useWorkItem(id);
  // Children come from the list payload (populated by gm-peg) so we
  // avoid an N+1 Get per child. The relationships graph on the list
  // result is enough to pick out the direct children of this Epic.
  const { data: allItems } = useWorkItems();
  const children = useMemo(
    () => (allItems ? epicChildren(allItems, id) : []),
    [allItems, id]
  );

  return (
    <>
      <DrawerHeader id={id} title={epicItem?.title ?? ''} />
      <div className="flex-1 overflow-y-auto px-6 pb-10" data-testid="epic-drawer-scroll">
        {isLoading ? (
          <div className="py-8 text-sm text-neutral-500" data-testid="epic-drawer-loading">
            Loading epic…
          </div>
        ) : error ? (
          <div
            className="py-8 text-sm text-red-600 dark:text-red-400"
            data-testid="epic-drawer-error"
          >
            {error.message}
          </div>
        ) : epicItem ? (
          <Body epic={epicItem} children={children} onOpenChild={onOpenChild} />
        ) : null}
      </div>
    </>
  );
}

function DrawerHeader({ id, title }: { id: string; title: string }) {
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
  return (
    <div className="flex items-start gap-2 border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
      <div className="min-w-0 flex-1">
        <Dialog.Title className="truncate text-base font-semibold text-neutral-900 dark:text-neutral-100">
          {title || id}
        </Dialog.Title>
        <div className="mt-1 flex items-center gap-2 text-xs text-neutral-500">
          <span className="rounded bg-violet-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-violet-700 dark:bg-violet-950 dark:text-violet-300">
            epic
          </span>
          <span className="font-mono" data-testid="epic-drawer-id">
            {id}
          </span>
          <button
            type="button"
            onClick={copyId}
            className="rounded p-0.5 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
            aria-label="Copy epic ID"
            data-testid="epic-drawer-copy"
          >
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </button>
        </div>
      </div>
      <Dialog.Close
        className="mt-1 rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        aria-label="Close"
        data-testid="epic-drawer-close"
      >
        <X className="h-4 w-4" />
      </Dialog.Close>
    </div>
  );
}

interface BodyProps {
  epic: WorkItem;
  children: WorkItem[];
  onOpenChild?: (id: string) => void;
}

function Body({ epic, children, onOpenChild }: BodyProps) {
  const { workPlane } = useCapabilities();
  const DescriptionRenderer = rendererFor(workPlane?.description_format);

  // Bucket children by state for the §5.6 progress section.
  const byState: Record<StateCategory, WorkItem[]> = {
    backlog: [],
    unstarted: [],
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
