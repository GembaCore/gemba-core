import { NavLink } from 'react-router-dom';
import {
  LayoutGrid,
  Terminal,
  Footprints,
  AlertTriangle,
  Sparkles,
  Settings as SettingsIcon,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useProjectPicker } from './projectpicker/ProjectPickerContext';

// Six first-order panes per the 2026-04-28 ratification (gm-e12.19,
// second amendment). Secondary surfaces — Backlog, Sprints, Coach,
// Agent groups, Graph, Drift, Capability Browser, Mail — survive as
// deep-link routes today and roll up under their pane's in-screen
// tabs as gm-e12.19.4-7 land.
//
// Initial route map (until pane consolidation lands):
//   Plan            → /board     (gm-e12.19.4 grows tabs Board/List/Sprints/Graph)
//   Agent Sessions  → /sessions  (gm-e12.19.5 grows tabs Sessions/Groups/Coach)
//   Review          → /walk      (Gemba walk surface)
//   Escalations     → /escalations (gm-e12.19.6 grows a Drift tab)
//   Insights        → /insights
//   Settings        → /settings  (gm-e12.19.2 grows tabs for Adaptors/Agents/Mail)
//
// Item.workspaceScoped marks panes that require an active project to
// be meaningful. On cold-start (no active project) those render as
// muted spans, not interactive links — clicking them otherwise targets
// a workspace that doesn't exist (gm-root.17.12). Settings is
// non-workspace-scoped — global app config is reachable from a
// fresh install.
type Item = {
  to: string;
  label: string;
  Icon: LucideIcon;
  workspaceScoped?: boolean;
};

const items: Item[] = [
  { to: '/board', label: 'Plan', Icon: LayoutGrid, workspaceScoped: true },
  { to: '/sessions', label: 'Agent Sessions', Icon: Terminal, workspaceScoped: true },
  { to: '/walk', label: 'Review', Icon: Footprints, workspaceScoped: true },
  { to: '/escalations', label: 'Escalations', Icon: AlertTriangle, workspaceScoped: true },
  { to: '/insights', label: 'Insights', Icon: Sparkles, workspaceScoped: true },
];

const settingsItem: Item = { to: '/settings', label: 'Settings', Icon: SettingsIcon };

const COLD_START_TITLE = 'Available after creating or switching to a project.';

export function Sidebar() {
  const { activeProject, isLoading } = useProjectPicker();
  // Cold-start = picker has finished its initial fetch and there is no
  // active project. Suppress the muted state during the initial fetch
  // so the sidebar doesn't flicker between muted and active.
  const coldStart = !isLoading && activeProject === null;

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950">
      <div className="flex h-12 items-center px-4 border-b border-neutral-200 dark:border-neutral-800">
        <span className="font-semibold tracking-tight">Gemba</span>
      </div>
      <nav className="flex flex-col gap-0.5 p-2" data-testid="sidebar-nav">
        {items.map((item) => (
          <NavItem key={item.to} item={item} coldStart={coldStart} />
        ))}
      </nav>
      <div className="mt-auto border-t border-neutral-200 p-2 dark:border-neutral-800">
        <NavItem item={settingsItem} coldStart={coldStart} />
      </div>
    </aside>
  );
}

function NavItem({ item, coldStart }: { item: Item; coldStart: boolean }) {
  const { to, label, Icon, workspaceScoped } = item;
  const muted = coldStart && Boolean(workspaceScoped);
  const testId = `sidebar-item-${to.replace(/^\//, '')}`;

  if (muted) {
    return (
      <span
        role="link"
        aria-disabled="true"
        title={COLD_START_TITLE}
        data-testid={testId}
        data-disabled="true"
        className={cn(
          'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm',
          'cursor-not-allowed opacity-40 select-none',
          'text-neutral-600 dark:text-neutral-400'
        )}
      >
        <Icon className="h-4 w-4" aria-hidden />
        <span>{label}</span>
      </span>
    );
  }

  return (
    <NavLink
      to={to}
      data-testid={testId}
      className={({ isActive }) =>
        cn(
          'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors',
          isActive
            ? 'bg-neutral-200 text-neutral-900 dark:bg-neutral-800 dark:text-white'
            : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900 dark:text-neutral-400 dark:hover:bg-neutral-900 dark:hover:text-white'
        )
      }
    >
      <Icon className="h-4 w-4" aria-hidden />
      <span>{label}</span>
    </NavLink>
  );
}
