// EpicMilestoneDropdown (gm-o2k9) — edit path c. Sits in the
// EpicDrawer's Overview area: shows the epic's current milestone parent
// (or "None") and lets the operator re-parent or clear via
// updateWorkItem({ parent_id }). Mirrors MilestonePicker's pill+listbox
// styling so the two dropdowns feel like the same control.

import { useEffect, useRef, useState } from 'react';
import { ChevronDown, Flag } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useUpdateWorkItem } from '@/hooks/useWorkItems';
import { findMilestoneForEpic } from './milestoneBadge';
import { listMilestones } from './milestone';
import type { WorkItem } from '@/types/core.gen';

// NONE_VALUE is the option id for "no milestone" — clearing the parent.
// Picked so it can never collide with a real bead id.
const NONE_VALUE = '__none__';

export interface EpicMilestoneDropdownProps {
  epic: WorkItem;
  // items is the unfiltered project dataset; used to walk the epic's
  // current milestone and to enumerate the option list.
  items: WorkItem[];
}

export function EpicMilestoneDropdown({ epic, items }: EpicMilestoneDropdownProps) {
  const mutation = useUpdateWorkItem();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const current = findMilestoneForEpic(items, epic.id);
  const milestones = listMilestones(items);
  const activeLabel = current ? current.title || current.id : 'None';

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    const onClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onClick);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onClick);
    };
  }, [open]);

  const onSelect = (value: string) => {
    setOpen(false);
    // Empty string is the clear sentinel on the wire (gm-gsbj).
    const parent_id = value === NONE_VALUE ? '' : value;
    if ((current?.id ?? '') === (parent_id || '')) return;
    mutation.mutate({ id: epic.id, patch: { parent_id } });
  };

  const activeId = current?.id ?? NONE_VALUE;

  return (
    <div
      ref={containerRef}
      className="relative inline-flex items-center gap-1"
      data-testid="epic-milestone-dropdown"
    >
      <button
        type="button"
        data-testid="epic-milestone-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={current ? current.id : 'No milestone'}
        onClick={() => setOpen((v) => !v)}
        disabled={mutation.isPending}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs',
          'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100',
          'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
          current &&
            'border-violet-300 bg-violet-50 text-violet-800 dark:border-violet-800 dark:bg-violet-950 dark:text-violet-200',
          mutation.isPending && 'cursor-not-allowed opacity-60',
        )}
      >
        <Flag className="h-3 w-3" />
        <span className="text-neutral-500">Milestone</span>
        <span className="max-w-[14rem] truncate font-mono">{activeLabel}</span>
        <ChevronDown className={cn('h-3 w-3 opacity-60 transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <div
          role="listbox"
          aria-label="Milestone"
          data-testid="epic-milestone-options"
          className={cn(
            'absolute left-0 top-full z-50 mt-1 max-h-80 min-w-64 overflow-y-auto rounded-md border',
            'border-neutral-200 bg-white shadow-md dark:border-neutral-700 dark:bg-neutral-900',
          )}
        >
          <OptionRow
            value={NONE_VALUE}
            label="None"
            active={activeId === NONE_VALUE}
            onSelect={onSelect}
          />
          {milestones.map((m) => (
            <OptionRow
              key={m.id}
              value={m.id}
              label={m.title || m.id}
              hint={m.id}
              active={activeId === m.id}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

interface OptionRowProps {
  value: string;
  label: string;
  hint?: string;
  active: boolean;
  onSelect: (value: string) => void;
}

function OptionRow({ value, label, hint, active, onSelect }: OptionRowProps) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={active}
      data-testid={`epic-milestone-option-${value}`}
      onClick={() => onSelect(value)}
      className={cn(
        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs',
        'hover:bg-neutral-100 dark:hover:bg-neutral-800',
        active
          ? 'font-medium text-neutral-900 dark:text-neutral-100'
          : 'text-neutral-700 dark:text-neutral-300',
      )}
    >
      <span className="truncate">{label}</span>
      {hint && hint !== label && (
        <span className="ml-auto truncate font-mono text-[10px] text-neutral-500 dark:text-neutral-500">
          {hint}
        </span>
      )}
      {active && <span className="shrink-0 text-[10px] text-violet-600 dark:text-violet-400">active</span>}
    </button>
  );
}
