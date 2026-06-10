// Shared in-pane tab bar (gm-e12.19.7). Converges the inline-tab
// patterns scattered across the SPA (Settings, Board view-toggle,
// future Insights / Sprints sub-tabs) onto one component.
//
// Visual: underlined tabs (matches the existing SettingsPage pattern).
// Active tab gets a solid bottom border + ink-colored text; disabled
// tabs are muted and non-interactive. Optional count chips render
// inline; an optional rightSlot floats to the far right of the row.

import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface TabSpec {
  id: string;
  label: string;
  icon?: LucideIcon;
  count?: number;
  disabled?: boolean;
}

export interface TabBarProps {
  value: string;
  onChange: (id: string) => void;
  tabs: TabSpec[];
  rightSlot?: ReactNode;
  testid?: string;
}

export function TabBar({
  value,
  onChange,
  tabs,
  rightSlot,
  testid,
}: TabBarProps): JSX.Element {
  return (
    <div
      data-testid={testid}
      role="tablist"
      className="flex items-center gap-2 border-b border-neutral-200 px-6 dark:border-neutral-800"
    >
      <div className="flex items-center gap-2">
        {tabs.map((t) => (
          <TabButton
            key={t.id}
            tab={t}
            active={t.id === value}
            onClick={() => {
              if (!t.disabled) onChange(t.id);
            }}
            testid={testid ? `${testid}-${t.id}` : undefined}
          />
        ))}
      </div>
      {rightSlot ? (
        <div
          className="ml-auto flex items-center"
          data-testid={testid ? `${testid}-right` : undefined}
        >
          {rightSlot}
        </div>
      ) : null}
    </div>
  );
}

interface TabButtonProps {
  tab: TabSpec;
  active: boolean;
  onClick: () => void;
  testid?: string;
}

function TabButton({ tab, active, onClick, testid }: TabButtonProps): JSX.Element {
  const Icon = tab.icon;
  const showCount = typeof tab.count === 'number' && tab.count > 0;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      aria-disabled={tab.disabled || undefined}
      disabled={tab.disabled}
      data-testid={testid}
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium transition-colors',
        tab.disabled
          ? 'cursor-not-allowed border-transparent text-neutral-300 dark:text-neutral-600'
          : active
            ? 'border-neutral-900 text-neutral-900 dark:border-neutral-100 dark:text-neutral-100'
            : 'border-transparent text-neutral-500 hover:text-neutral-700 dark:hover:text-neutral-300'
      )}
    >
      {Icon ? <Icon className="h-4 w-4" aria-hidden /> : null}
      <span>{tab.label}</span>
      {showCount ? (
        <span
          data-testid={testid ? `${testid}-count` : undefined}
          className={cn(
            'ml-1 inline-flex min-w-[1.25rem] items-center justify-center rounded-full px-1.5 py-0.5 text-[10px] font-mono tabular-nums',
            active
              ? 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900'
              : 'bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300'
          )}
        >
          {tab.count}
        </span>
      ) : null}
    </button>
  );
}

export default TabBar;
