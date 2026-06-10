import { useMemo } from 'react';
import { Flag, Layers3, ListTodo } from 'lucide-react';
import type { WorkItem, WorkItemID } from '@/types/core.gen';
import { cn } from '@/lib/utils';
import { relativeTime } from './relativeTime';
import { compareWorkItemsByOrder, sortWorkItems, type BoardOrderKey } from './boardOrder';

interface CascadeNode {
  item: WorkItem;
  children: CascadeNode[];
}

interface CascadeModel {
  roots: CascadeNode[];
  unassigned: CascadeNode[];
}

export interface BeadsCascadeViewProps {
  items: WorkItem[];
  orderKey: BoardOrderKey;
  onSelect: (item: WorkItem) => void;
}

export function BeadsCascadeView({ items, orderKey, onSelect }: BeadsCascadeViewProps) {
  const model = useMemo(() => buildCascadeModel(items, orderKey), [items, orderKey]);
  const counts = useMemo(() => summarize(items), [items]);

  if (items.length === 0) {
    return (
      <div
        data-testid="beads-cascade-empty"
        className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center"
      >
        <p className="text-sm text-neutral-600 dark:text-neutral-300">No beads yet.</p>
        <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">
          Create a milestone, epic, or bead to start building the project structure.
        </p>
      </div>
    );
  }

  return (
    <div data-testid="beads-cascade" className="h-full overflow-auto">
      <div className="sticky top-0 z-10 flex flex-wrap items-center gap-2 border-b border-neutral-200 bg-white/95 px-4 py-2 text-xs text-neutral-600 backdrop-blur dark:border-neutral-800 dark:bg-neutral-950/95 dark:text-neutral-300">
        <span className="font-medium text-neutral-900 dark:text-neutral-100">Cascade</span>
        <CountPill label="Milestones" value={counts.milestones} />
        <CountPill label="Epics" value={counts.epics} />
        <CountPill label="Beads" value={counts.beads} />
      </div>
      <div className="divide-y divide-neutral-200 dark:divide-neutral-800">
        {model.roots.map((node) => (
          <CascadeNodeRow key={node.item.id} node={node} depth={0} onSelect={onSelect} />
        ))}
        {model.unassigned.length > 0 ? (
          <section data-testid="beads-cascade-unassigned" className="py-2">
            <div className="px-4 pb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
              Unassigned
            </div>
            {model.unassigned.map((node) => (
              <CascadeNodeRow key={node.item.id} node={node} depth={0} onSelect={onSelect} />
            ))}
          </section>
        ) : null}
      </div>
    </div>
  );
}

function CascadeNodeRow({
  node,
  depth,
  onSelect,
}: {
  node: CascadeNode;
  depth: number;
  onSelect: (item: WorkItem) => void;
}) {
  const item = node.item;
  const childCount = node.children.length;
  return (
    <div data-testid={`beads-cascade-row-${item.id}`}>
      <button
        type="button"
        onClick={() => onSelect(item)}
        style={{ paddingLeft: `${16 + depth * 24}px` }}
        className={cn(
          'flex w-full items-center gap-2 px-4 py-2 text-left text-sm transition-colors',
          'hover:bg-neutral-50 focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-inset',
          'dark:hover:bg-neutral-900'
        )}
      >
        <KindIcon kind={item.kind} />
        <span className="w-28 shrink-0 truncate font-mono text-[11px] text-neutral-500 dark:text-neutral-400">
          {item.id}
        </span>
        <span className="min-w-0 flex-1 truncate font-medium text-neutral-900 dark:text-neutral-100">
          {item.title}
        </span>
        <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
          {item.kind}
        </span>
        <span
          className="rounded bg-sky-50 px-1.5 py-0.5 text-[10px] font-medium text-sky-700 dark:bg-sky-950 dark:text-sky-300"
          title={item.status}
        >
          {item.state_category}
        </span>
        {childCount > 0 ? (
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
            {childCount}
          </span>
        ) : null}
        <span className="w-20 shrink-0 text-right text-[11px] text-neutral-500">
          {relativeTime(item.updated_at)}
        </span>
      </button>
      {node.children.map((child) => (
        <CascadeNodeRow key={child.item.id} node={child} depth={depth + 1} onSelect={onSelect} />
      ))}
    </div>
  );
}

function CountPill({ label, value }: { label: string; value: number }) {
  return (
    <span className="rounded bg-neutral-100 px-2 py-0.5 text-[11px] text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
      {value} {label}
    </span>
  );
}

function KindIcon({ kind }: { kind: string }) {
  if (kind === 'milestone') {
    return <Flag className="h-3.5 w-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />;
  }
  if (kind === 'epic') {
    return <Layers3 className="h-3.5 w-3.5 shrink-0 text-violet-600 dark:text-violet-400" />;
  }
  return <ListTodo className="h-3.5 w-3.5 shrink-0 text-sky-600 dark:text-sky-400" />;
}

function buildCascadeModel(items: WorkItem[], orderKey: BoardOrderKey): CascadeModel {
  const byID = new Map<WorkItemID, WorkItem>();
  const childrenByParent = new Map<WorkItemID, WorkItem[]>();
  for (const item of items) byID.set(item.id, item);

  for (const item of items) {
    for (const rel of item.relationships ?? []) {
      if (rel.kind !== 'parent_child' || rel.to !== item.id || !byID.has(rel.from)) continue;
      const siblings = childrenByParent.get(rel.from) ?? [];
      siblings.push(item);
      childrenByParent.set(rel.from, siblings);
    }
  }

  const visited = new Set<WorkItemID>();
  const rootMilestones = sortWorkItems(
    items.filter((item) => item.kind === 'milestone' && !parentInSet(item, byID)),
    orderKey
  );
  const roots = rootMilestones.map((item) =>
    buildNode(item, childrenByParent, visited, orderKey, new Set())
  );

  const unassignedSeeds = sortWorkItems(
    items.filter((item) => !visited.has(item.id) && !parentInSet(item, byID)),
    orderKey
  );
  const unassigned = unassignedSeeds.map((item) =>
    buildNode(item, childrenByParent, visited, orderKey, new Set())
  );

  return { roots, unassigned };
}

function buildNode(
  item: WorkItem,
  childrenByParent: Map<WorkItemID, WorkItem[]>,
  visited: Set<WorkItemID>,
  orderKey: BoardOrderKey,
  stack: Set<WorkItemID>
): CascadeNode {
  visited.add(item.id);
  if (stack.has(item.id)) return { item, children: [] };
  const nextStack = new Set(stack);
  nextStack.add(item.id);
  const children = [...(childrenByParent.get(item.id) ?? [])]
    .sort((a, b) => kindRank(a) - kindRank(b) || compareWorkItemsByOrder(orderKey, a, b))
    .map((child) => buildNode(child, childrenByParent, visited, orderKey, nextStack));
  return { item, children };
}

function parentInSet(item: WorkItem, byID: Map<WorkItemID, WorkItem>): boolean {
  return Boolean(
    item.relationships?.some(
      (rel) => rel.kind === 'parent_child' && rel.to === item.id && byID.has(rel.from)
    )
  );
}

function kindRank(item: WorkItem): number {
  if (item.kind === 'milestone') return 0;
  if (item.kind === 'epic') return 1;
  return 2;
}

function summarize(items: WorkItem[]) {
  let milestones = 0;
  let epics = 0;
  let beads = 0;
  for (const item of items) {
    if (item.kind === 'milestone') milestones += 1;
    else if (item.kind === 'epic') epics += 1;
    else beads += 1;
  }
  return { milestones, epics, beads };
}
