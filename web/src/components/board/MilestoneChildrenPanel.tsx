// MilestoneChildrenPanel (gm-98sq) — milestone-specific section in
// WorkItemDrawer. Lists epics already parented under this milestone
// (with Remove) and offers an unparented-epic picker (with Add).
//
// Mutations go through useUpdateWorkItem so we inherit optimistic
// cache writes + nonce-gated PATCH + cache invalidation. The
// parent-child edge plumbing lives in WorkItemPatch.parent_id
// (gm-gsbj): empty string clears the parent, non-empty re-parents.

import { useMemo, useState } from 'react';
import { Plus, X } from 'lucide-react';
import { useUpdateWorkItem } from '@/hooks/useWorkItems';
import { cn } from '@/lib/utils';
import type { WorkItem } from '@/types/core.gen';

const EPIC_KIND = 'epic';

export interface MilestoneChildrenPanelProps {
  milestone: WorkItem;
  // allItems is the full project dataset; the panel uses it both for
  // the children list (epics whose parent_child edge points here) and
  // for the add-list (epics that have no parent at all).
  allItems: WorkItem[];
}

// epicsParentedUnder returns the epics whose parent (via parent_child
// edge with .from = parentId) is the given milestone. Order: by id.
export function epicsParentedUnder(items: WorkItem[], parentId: string): WorkItem[] {
  const out = items
    .filter((it) => it.kind === EPIC_KIND)
    .filter((it) =>
      (it.relationships ?? []).some(
        (r) => r.kind === 'parent_child' && r.from === parentId && r.to === it.id,
      ),
    );
  return [...out].sort((a, b) => a.id.localeCompare(b.id));
}

// unparentedEpics returns epics with no parent_child edge into them.
// Those are the candidates the add-affordance offers.
export function unparentedEpics(items: WorkItem[]): WorkItem[] {
  const out = items
    .filter((it) => it.kind === EPIC_KIND)
    .filter(
      (it) => !(it.relationships ?? []).some((r) => r.kind === 'parent_child' && r.to === it.id),
    );
  return [...out].sort((a, b) => a.id.localeCompare(b.id));
}

export function MilestoneChildrenPanel({ milestone, allItems }: MilestoneChildrenPanelProps) {
  const update = useUpdateWorkItem();
  const [filter, setFilter] = useState('');

  const children = useMemo(
    () => epicsParentedUnder(allItems, milestone.id),
    [allItems, milestone.id],
  );
  const candidates = useMemo(() => unparentedEpics(allItems), [allItems]);
  const visibleCandidates = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return candidates;
    return candidates.filter(
      (c) => c.id.toLowerCase().includes(q) || c.title.toLowerCase().includes(q),
    );
  }, [candidates, filter]);

  const onAdd = (epicId: string) =>
    update.mutate({ id: epicId, patch: { parent_id: milestone.id } });
  const onRemove = (epicId: string) =>
    update.mutate({ id: epicId, patch: { parent_id: '' } });

  return (
    <section data-testid="section-milestone-children">
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        Child epics
      </h3>

      <div className="mb-4">
        {children.length === 0 ? (
          <div
            className="text-xs text-neutral-500"
            data-testid="milestone-children-empty"
          >
            No epics parented under this milestone yet.
          </div>
        ) : (
          <ul className="space-y-1" data-testid="milestone-children-list">
            {children.map((epic) => (
              <li
                key={epic.id}
                data-testid={`milestone-child-${epic.id}`}
                className="flex items-center gap-2 rounded border border-neutral-200 bg-white px-2 py-1 text-sm dark:border-neutral-800 dark:bg-neutral-900"
              >
                <span className="min-w-0 flex-1 truncate" title={epic.title}>
                  {epic.title || epic.id}
                </span>
                <span className="shrink-0 font-mono text-[10px] text-neutral-500">
                  {epic.id}
                </span>
                <button
                  type="button"
                  onClick={() => onRemove(epic.id)}
                  disabled={update.isPending}
                  data-testid={`milestone-child-remove-${epic.id}`}
                  aria-label={`Remove ${epic.id} from milestone`}
                  title="Remove from milestone"
                  className={cn(
                    'inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs',
                    'border-neutral-300 bg-white text-neutral-700 hover:bg-rose-50 hover:text-rose-700',
                    'disabled:cursor-not-allowed disabled:opacity-60',
                    'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-rose-950 dark:hover:text-rose-300',
                  )}
                >
                  <X className="h-3 w-3" />
                  Remove
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <h4 className="mb-1 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        Add an epic
      </h4>
      {candidates.length === 0 ? (
        <div
          className="text-xs text-neutral-500"
          data-testid="milestone-add-empty"
        >
          No unparented epics available.
        </div>
      ) : (
        <>
          <input
            type="search"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter epics by id or title…"
            data-testid="milestone-add-filter"
            className="mb-2 w-full rounded border border-neutral-300 bg-white px-2 py-1 text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
          />
          {visibleCandidates.length === 0 ? (
            <div
              className="text-xs text-neutral-500"
              data-testid="milestone-add-no-match"
            >
              No matches.
            </div>
          ) : (
            <ul className="max-h-64 space-y-1 overflow-y-auto" data-testid="milestone-add-list">
              {visibleCandidates.map((epic) => (
                <li
                  key={epic.id}
                  data-testid={`milestone-add-${epic.id}`}
                  className="flex items-center gap-2 rounded border border-neutral-200 bg-white px-2 py-1 text-sm dark:border-neutral-800 dark:bg-neutral-900"
                >
                  <span className="min-w-0 flex-1 truncate" title={epic.title}>
                    {epic.title || epic.id}
                  </span>
                  <span className="shrink-0 font-mono text-[10px] text-neutral-500">
                    {epic.id}
                  </span>
                  <button
                    type="button"
                    onClick={() => onAdd(epic.id)}
                    disabled={update.isPending}
                    data-testid={`milestone-add-button-${epic.id}`}
                    aria-label={`Add ${epic.id} to milestone`}
                    title="Add to milestone"
                    className={cn(
                      'inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs',
                      'border-emerald-300 bg-emerald-50 text-emerald-800 hover:bg-emerald-100',
                      'disabled:cursor-not-allowed disabled:opacity-60',
                      'dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 dark:hover:bg-emerald-900',
                    )}
                  >
                    <Plus className="h-3 w-3" />
                    Add
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}

      {update.isError ? (
        <div
          className="mt-2 text-xs text-rose-600 dark:text-rose-400"
          data-testid="milestone-children-error"
        >
          {update.error?.message ?? 'Update failed.'}
        </div>
      ) : null}
    </section>
  );
}
