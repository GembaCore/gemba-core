import type { WorkItem } from '@/types/core.gen';
import type { CSSProperties } from 'react';
import { WorkItemCard } from './WorkItemCard';
import { useDraggable, useDroppable } from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import { sortWorkItems, type BoardOrderKey } from './boardOrder';

export interface BoardColumnProps {
  columnID: string;
  label: string;
  items: WorkItem[];
  // Forwarded to each WorkItemCard. Clicking a card fires this with the id.
  onSelect?: (id: string) => void;
  // Optional dnd-kit target id. When set, the whole column becomes a
  // drop zone for work-item restaging.
  droppableID?: string;
  // Makes each WorkItemCard a dnd-kit drag handle.
  draggable?: boolean;
  // Open-escalation count by work-item id (gm-e11.3). Threaded by the
  // page so the lookup is O(1) per card.
  escalationCounts?: Map<string, number>;
  // Optional operator-selected ordering. When unset, preserve the
  // historical priority-first column order used by the execution board.
  orderKey?: BoardOrderKey | null;
}

// Sort within a column: priority ascending (P0 first), nulls last; tie-break
// on updated_at descending so the freshest work floats to the top of its
// priority band.
function sortItems(items: WorkItem[]): WorkItem[] {
  return [...items].sort((a, b) => {
    const pa = a.priority ?? Number.POSITIVE_INFINITY;
    const pb = b.priority ?? Number.POSITIVE_INFINITY;
    if (pa !== pb) return pa - pb;
    return b.updated_at.localeCompare(a.updated_at);
  });
}

export function BoardColumn({
  columnID,
  label,
  items,
  onSelect,
  droppableID,
  draggable = false,
  escalationCounts,
  orderKey,
}: BoardColumnProps) {
  const sorted = orderKey ? sortWorkItems(items, orderKey) : sortItems(items);
  const droppable = useDroppable({
    id: droppableID ?? `disabled|${columnID}`,
    disabled: !droppableID,
  });
  return (
    <section
      ref={droppable.setNodeRef}
      data-testid={`board-column-${columnID}`}
      data-drop-over={droppable.isOver || undefined}
      className={
        'flex h-full min-w-[18rem] flex-1 flex-col rounded-md bg-neutral-50 transition-colors dark:bg-neutral-950 ' +
        (droppable.isOver
          ? 'ring-2 ring-sky-400 ring-offset-2 ring-offset-white dark:ring-offset-neutral-950'
          : '')
      }
    >
      <header className="flex items-center justify-between border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-neutral-600 dark:text-neutral-400">
          {label}
        </h2>
        <span
          data-testid={`board-column-${columnID}-count`}
          className="rounded bg-neutral-200 px-1.5 py-0.5 text-[10px] font-medium text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
        >
          {items.length}
        </span>
      </header>
      <ol className="flex-1 space-y-2 overflow-y-auto p-2">
        {sorted.map((item) => (
          <li key={item.id}>
            {draggable ? (
              <DraggableWorkItemCard
                item={item}
                onSelect={onSelect}
                escalationCount={escalationCounts?.get(item.id) ?? 0}
              />
            ) : (
              <WorkItemCard
                item={item}
                onSelect={onSelect}
                escalationCount={escalationCounts?.get(item.id) ?? 0}
              />
            )}
          </li>
        ))}
      </ol>
    </section>
  );
}

function DraggableWorkItemCard({
  item,
  onSelect,
  escalationCount,
}: {
  item: WorkItem;
  onSelect?: (id: string) => void;
  escalationCount: number;
}) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: item.id,
  });
  const style: CSSProperties = {
    transform: CSS.Translate.toString(transform),
    opacity: isDragging ? 0.85 : undefined,
    zIndex: isDragging ? 50 : undefined,
    position: isDragging ? 'relative' : undefined,
    touchAction: 'none',
  };
  return (
    <div ref={setNodeRef} style={style} {...attributes} {...listeners}>
      <WorkItemCard item={item} onSelect={onSelect} escalationCount={escalationCount} draggable />
    </div>
  );
}
