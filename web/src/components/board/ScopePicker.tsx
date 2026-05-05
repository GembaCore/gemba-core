// ScopePicker (gm-uekk) — single pill that names the active Epic
// scope, with a dropdown to pick one of: All / a root epic / a
// direct child epic. Replaces the unbounded RootEpicBanner strip and
// the swimlane-mode dropdown. URL state is owned by BoardPage; this
// component is a controlled trigger + dropdown.

import { useEffect, useRef, useState } from 'react';
import { ChevronDown, FolderTree } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  SCOPE_ALL,
  buildScopeOptions,
  findScopeOption,
  type ScopeID,
  type ScopeOption,
} from './scope';
import type { WorkItem } from '@/types/core.gen';

export interface ScopePickerProps {
  // items is the full unfiltered project dataset. The picker uses it
  // to build its option list; BoardPage applies the actual filter.
  items: WorkItem[];
  value: ScopeID;
  onChange: (next: ScopeID) => void;
}

export function ScopePicker({ items, value, onChange }: ScopePickerProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const options = buildScopeOptions(items);
  const active = findScopeOption(options, value) ?? options[0];

  // Close on outside click / Escape — same pattern as ProjectPicker.
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

  const onSelect = (next: ScopeID) => {
    setOpen(false);
    onChange(next);
  };

  // Trigger label: "All" or the scope's title; never overflow the pill.
  const triggerLabel = active.label;

  return (
    <div ref={containerRef} className="relative" data-testid="board-scope-picker">
      <button
        type="button"
        data-testid="board-scope-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={active.id === SCOPE_ALL ? 'All work in this project' : active.id}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs',
          'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100',
          'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800',
          active.id !== SCOPE_ALL && 'border-sky-300 bg-sky-50 text-sky-800 dark:border-sky-800 dark:bg-sky-950 dark:text-sky-200',
        )}
      >
        <FolderTree className="h-3 w-3" />
        <span className="text-neutral-500">Epic</span>
        <span className="max-w-[12rem] truncate font-mono">{triggerLabel}</span>
        <ChevronDown className={cn('h-3 w-3 opacity-60 transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <div
          role="listbox"
          aria-label="Epic"
          data-testid="board-scope-dropdown"
          className={cn(
            'absolute left-0 top-full z-50 mt-1 max-h-80 min-w-64 overflow-y-auto rounded-md border',
            'border-neutral-200 bg-white shadow-md dark:border-neutral-700 dark:bg-neutral-900',
          )}
        >
          {options.map((opt) => (
            <ScopeOptionRow
              key={opt.id}
              option={opt}
              active={opt.id === value}
              onSelect={onSelect}
            />
          ))}
          {options.length === 1 && (
            <div className="px-3 py-2 text-[11px] italic text-neutral-500 dark:text-neutral-400">
              No epics yet — board shows every work item.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

interface ScopeOptionRowProps {
  option: ScopeOption;
  active: boolean;
  onSelect: (id: ScopeID) => void;
}

function ScopeOptionRow({ option, active, onSelect }: ScopeOptionRowProps) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={active}
      data-testid={`board-scope-option-${option.id}`}
      onClick={() => onSelect(option.id)}
      className={cn(
        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs',
        'hover:bg-neutral-100 dark:hover:bg-neutral-800',
        option.depth === 1 && 'pl-7',
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
      {active && (
        <span className="shrink-0 text-[10px] text-sky-600 dark:text-sky-400">active</span>
      )}
    </button>
  );
}
