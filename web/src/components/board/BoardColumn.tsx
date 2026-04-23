import type { StateCategory, WorkItem } from '@/types/core.gen';
import { BeadCard } from './BeadCard';

export interface BoardColumnProps {
  category: StateCategory;
  label: string;
  items: WorkItem[];
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

export function BoardColumn({ category, label, items }: BoardColumnProps) {
  const sorted = sortItems(items);
  return (
    <section
      data-testid={`board-column-${category}`}
      className="flex h-full min-w-[18rem] flex-1 flex-col rounded-md bg-neutral-50 dark:bg-neutral-950"
    >
      <header className="flex items-center justify-between border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <h2 className="text-xs font-semibold uppercase tracking-wide text-neutral-600 dark:text-neutral-400">
          {label}
        </h2>
        <span className="rounded bg-neutral-200 px-1.5 py-0.5 text-[10px] font-medium text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
          {items.length}
        </span>
      </header>
      <ol className="flex-1 space-y-2 overflow-y-auto p-2">
        {sorted.map((item) => (
          <li key={item.id}>
            <BeadCard item={item} />
          </li>
        ))}
      </ol>
    </section>
  );
}
