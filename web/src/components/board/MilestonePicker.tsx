// MilestonePicker (gm-l7hy) — separate pill from ScopePicker. Single-
// select: All / M1 / M2 / ... A "Show" button next to the trigger
// opens the active milestone's bead in the WorkItemDrawer. Mirrors
// ScopePicker's layout and outside-click handling.

import { useEffect, useRef, useState } from 'react';
import { ChevronDown, Eye, Flag } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  MILESTONE_ALL,
  buildMilestoneOptions,
  findMilestoneOption,
  type MilestoneID,
  type MilestoneOption,
} from './milestone';
import type { WorkItem } from '@/types/core.gen';

export interface MilestonePickerProps {
  // items is the unfiltered project dataset; the picker uses it to
  // enumerate every milestone regardless of which one is active.
  items: WorkItem[];
  value: MilestoneID;
  onChange: (next: MilestoneID) => void;
  // onShow opens the active milestone bead in a drawer. The Show
  // affordance is hidden when value === MILESTONE_ALL because there
  // is no specific bead to render.
  onShow?: (id: string) => void;
}

export function MilestonePicker({ items, value, onChange, onShow }: MilestonePickerProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const options = buildMilestoneOptions(items);
  const active = findMilestoneOption(options, value) ?? options[0];

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

  // Hide the picker entirely when no milestones exist — there's
  // nothing to pick. Operators see it again the moment a milestone
  // lands in the dataset.
  if (options.length === 1) return null;

  const onSelect = (next: MilestoneID) => {
    setOpen(false);
    onChange(next);
  };

  return (
    <div ref={containerRef} className="relative flex items-center gap-1" data-testid="board-milestone-picker">
      <button
        type="button"
        data-testid="board-milestone-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={active.id === MILESTONE_ALL ? 'All milestones' : active.id}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs',
          'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100',
          'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
          active.id !== MILESTONE_ALL &&
            'border-violet-300 bg-violet-50 text-violet-800 dark:border-violet-800 dark:bg-violet-950 dark:text-violet-200',
        )}
      >
        <Flag className="h-3 w-3" />
        <span className="text-neutral-500">Milestone</span>
        <span className="max-w-[12rem] truncate font-mono">{active.label}</span>
        <ChevronDown className={cn('h-3 w-3 opacity-60 transition-transform', open && 'rotate-180')} />
      </button>

      {active.id !== MILESTONE_ALL && onShow && (
        <button
          type="button"
          data-testid="board-milestone-show"
          aria-label="Show milestone details"
          title="Show milestone details"
          onClick={() => onShow(active.id)}
          className={cn(
            'inline-flex items-center rounded-md border px-1.5 py-0.5 text-xs',
            'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100',
            'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
          )}
        >
          <Eye className="h-3 w-3" />
        </button>
      )}

      {open && (
        <div
          role="listbox"
          aria-label="Milestone"
          data-testid="board-milestone-dropdown"
          className={cn(
            'absolute left-0 top-full z-50 mt-1 max-h-80 min-w-64 overflow-y-auto rounded-md border',
            'border-neutral-200 bg-white shadow-md dark:border-neutral-700 dark:bg-neutral-900',
          )}
        >
          {options.map((opt) => (
            <MilestoneOptionRow
              key={opt.id}
              option={opt}
              active={opt.id === value}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

interface MilestoneOptionRowProps {
  option: MilestoneOption;
  active: boolean;
  onSelect: (id: MilestoneID) => void;
}

function MilestoneOptionRow({ option, active, onSelect }: MilestoneOptionRowProps) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={active}
      data-testid={`board-milestone-option-${option.id}`}
      onClick={() => onSelect(option.id)}
      className={cn(
        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs',
        'hover:bg-neutral-100 dark:hover:bg-neutral-800',
        active
          ? 'font-medium text-neutral-900 dark:text-neutral-100'
          : 'text-neutral-700 dark:text-neutral-300',
      )}
    >
      <span className="truncate">{option.label}</span>
      {option.hint && option.hint !== option.label && (
        <span className="ml-auto truncate font-mono text-[10px] text-neutral-500 dark:text-neutral-500">
          {option.hint}
        </span>
      )}
      {active && <span className="shrink-0 text-[10px] text-violet-600 dark:text-violet-400">active</span>}
    </button>
  );
}
