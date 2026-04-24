import type { KeyboardEvent } from 'react';
import { FileText, GitBranch, Paperclip } from 'lucide-react';
import type { StateCategory, WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';
import { relativeTime } from './relativeTime';

export interface WorkItemCardProps {
  item: WorkItem;
  // onSelect makes the card clickable (and keyboard-activatable). Wire
  // it from BoardPage to open the drill-in drawer. Omitted → static card.
  onSelect?: (id: string) => void;
}

const PRIORITY_STYLES: Record<string, string> = {
  P0: 'bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300',
  P1: 'bg-orange-100 text-orange-700 dark:bg-orange-950 dark:text-orange-300',
  P2: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
  P3: 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300',
  P4: 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400',
};

const STATE_DOT: Record<StateCategory, string> = {
  backlog: 'bg-neutral-400 dark:bg-neutral-500',
  unstarted: 'bg-sky-500',
  started: 'bg-amber-500',
  completed: 'bg-emerald-500',
  canceled: 'bg-rose-500',
};

function priorityLabel(priority: number | null | undefined): string | null {
  if (priority == null) return null;
  if (priority < 0 || priority > 4) return null;
  return `P${priority}`;
}

function assigneeName(item: WorkItem): string | null {
  return item.assignee?.name ?? item.owner?.name ?? null;
}

function hasParent(item: WorkItem): boolean {
  return !!item.relationships?.some((r) => r.kind === 'parent_child' && r.from === item.id);
}

function hasNotes(item: WorkItem): boolean {
  return !!(item.description && item.description.trim().length > 0);
}

function hasEvidence(item: WorkItem): boolean {
  return !!item.evidence && item.evidence.length > 0;
}

const MAX_VISIBLE_LABELS = 3;

export function WorkItemCard({ item, onSelect }: WorkItemCardProps) {
  const pri = priorityLabel(item.priority);
  const name = assigneeName(item);
  const labels = item.labels ?? [];
  const visibleLabels = labels.slice(0, MAX_VISIBLE_LABELS);
  const overflow = Math.max(0, labels.length - visibleLabels.length);

  const interactive = !!onSelect;
  const handleClick = onSelect ? () => onSelect(item.id) : undefined;
  // <article> can't be a native <button> (buttons reject flow content
  // like <h3>) so we use the ARIA role-button pattern: role, tabIndex,
  // and a keydown handler that treats Enter / Space as activation.
  const handleKeyDown = onSelect
    ? (e: KeyboardEvent<HTMLElement>) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect(item.id);
        }
      }
    : undefined;

  return (
    <article
      data-work-item-id={item.id}
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? `Open bead ${item.id}` : undefined}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      className={cn(
        'group rounded-md border border-neutral-200 bg-white p-3 text-sm shadow-sm',
        'hover:border-neutral-300 hover:shadow',
        'dark:border-neutral-800 dark:bg-neutral-900 dark:hover:border-neutral-700',
        interactive &&
          'cursor-pointer focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-2 focus:ring-offset-neutral-50 dark:focus:ring-offset-neutral-950'
      )}
    >
      <header className="mb-1.5 flex items-center gap-2">
        <span
          aria-hidden
          className={cn('h-2 w-2 shrink-0 rounded-full', STATE_DOT[item.state_category])}
          title={item.state_category}
        />
        <span className="font-mono text-[11px] text-neutral-500 dark:text-neutral-400">
          {item.id}
        </span>
        {pri && (
          <span
            className={cn(
              'ml-auto rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
              PRIORITY_STYLES[pri]
            )}
          >
            {pri}
          </span>
        )}
      </header>

      <h3 className="line-clamp-2 font-medium text-neutral-900 dark:text-neutral-100">
        {item.title}
      </h3>

      {visibleLabels.length > 0 && (
        <ul className="mt-2 flex flex-wrap gap-1">
          {visibleLabels.map((label) => (
            <li
              key={label}
              className="rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400"
            >
              {label}
            </li>
          ))}
          {overflow > 0 && (
            <li className="rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] text-neutral-500 dark:bg-neutral-800 dark:text-neutral-500">
              +{overflow}
            </li>
          )}
        </ul>
      )}

      <footer className="mt-3 flex items-center gap-2 text-[11px] text-neutral-500 dark:text-neutral-400">
        {name && <span className="truncate">{name}</span>}
        <div className="ml-auto flex items-center gap-1.5">
          {hasNotes(item) && (
            <FileText
              className="h-3 w-3"
              aria-label="has notes"
              data-testid="glyph-notes"
            />
          )}
          {hasEvidence(item) && (
            <Paperclip
              className="h-3 w-3"
              aria-label="has evidence"
              data-testid="glyph-evidence"
            />
          )}
          {hasParent(item) && (
            <GitBranch
              className="h-3 w-3"
              aria-label="has parent"
              data-testid="glyph-parent"
            />
          )}
          <time dateTime={item.updated_at} title={item.updated_at}>
            {relativeTime(item.updated_at)}
          </time>
        </div>
      </footer>
    </article>
  );
}
