import { ChevronRight } from 'lucide-react';
import { KIND_MILESTONE, type WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

export interface WorkItemBreadcrumbCrumb {
  id: string;
  title: string;
  kind: string;
  current: boolean;
}

export function buildWorkItemBreadcrumb(
  items: WorkItem[],
  current: WorkItem
): WorkItemBreadcrumbCrumb[] {
  const byID = new Map<string, WorkItem>();
  for (const item of items) byID.set(item.id, item);
  byID.set(current.id, current);

  const chain: Array<Pick<WorkItemBreadcrumbCrumb, 'id' | 'title' | 'kind'>> = [
    { id: current.id, title: current.title || current.id, kind: current.kind },
  ];
  const visited = new Set<string>([current.id]);
  let cursor: WorkItem | undefined = current;

  while (cursor) {
    const parentID = parentOf(cursor);
    if (!parentID || visited.has(parentID)) break;
    visited.add(parentID);

    const parent = byID.get(parentID);
    if (!parent) {
      chain.unshift({ id: parentID, title: parentID, kind: 'workitem' });
      break;
    }
    chain.unshift({ id: parent.id, title: parent.title || parent.id, kind: parent.kind });
    cursor = parent;
  }

  return chain.map((crumb, index) => ({
    ...crumb,
    current: index === chain.length - 1,
  }));
}

export function WorkItemBreadcrumb({
  crumbs,
  onNavigate,
}: {
  crumbs: WorkItemBreadcrumbCrumb[];
  onNavigate?: (crumb: WorkItemBreadcrumbCrumb) => void;
}) {
  if (crumbs.length <= 1) return null;

  return (
    <nav
      aria-label="Work item breadcrumb"
      data-testid="workitem-detail-breadcrumb"
      className="mb-1 flex min-w-0 flex-wrap items-center gap-1 text-[11px] text-neutral-500 dark:text-neutral-400"
    >
      {crumbs.map((crumb, index) => (
        <span key={`${crumb.id}-${index}`} className="flex min-w-0 items-center gap-1">
          {index > 0 ? (
            <ChevronRight className="h-3 w-3 shrink-0 text-neutral-400" aria-hidden />
          ) : null}
          <Crumb crumb={crumb} onNavigate={onNavigate} />
        </span>
      ))}
    </nav>
  );
}

function Crumb({
  crumb,
  onNavigate,
}: {
  crumb: WorkItemBreadcrumbCrumb;
  onNavigate?: (crumb: WorkItemBreadcrumbCrumb) => void;
}) {
  const content = (
    <>
      <span className="shrink-0 font-medium text-neutral-600 dark:text-neutral-300">
        {kindLabel(crumb.kind)}
      </span>
      <span className="truncate">{crumb.title}</span>
    </>
  );
  const className = cn(
    'inline-flex max-w-[13rem] min-w-0 items-center gap-1 rounded px-1 py-0.5',
    crumb.current
      ? 'text-neutral-500 dark:text-neutral-400'
      : 'hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100'
  );

  if (!crumb.current && onNavigate) {
    return (
      <button
        type="button"
        className={className}
        title={`${kindLabel(crumb.kind)}: ${crumb.title}`}
        onClick={() => onNavigate(crumb)}
      >
        {content}
      </button>
    );
  }

  return (
    <span
      className={className}
      title={`${kindLabel(crumb.kind)}: ${crumb.title}`}
      aria-current={crumb.current ? 'page' : undefined}
    >
      {content}
    </span>
  );
}

function parentOf(item: WorkItem): string | undefined {
  return (item.relationships ?? []).find((r) => r.kind === 'parent_child' && r.to === item.id)
    ?.from;
}

function kindLabel(kind: string): string {
  if (kind === KIND_MILESTONE) return 'Milestone';
  if (kind === 'epic') return 'Epic';
  return 'Work item';
}
