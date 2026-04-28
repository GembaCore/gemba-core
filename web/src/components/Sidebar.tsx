import { NavLink } from 'react-router-dom';
import {
  LayoutGrid,
  ListTodo,
  Network,
  Sparkles,
  AlertTriangle,
  Boxes,
  Footprints,
  Mail,
  Terminal,
  CircuitBoard,
  Settings,
  GitCompare,
  Users,
  CalendarRange,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { features } from '@/lib/features';
import { useProjectPicker } from './projectpicker/ProjectPickerContext';

// Item.workspaceScoped marks routes that require an active project to
// be meaningful. On cold-start (no active project) these render as
// muted spans, not interactive links — clicking them otherwise targets
// a workspace that doesn't exist (gm-root.17.12).
//
// Items not marked workspaceScoped are operational without a project:
//  - Settings: global app settings.
//  - Capability Browser: server-level adaptor inventory.
type Item = {
  to: string;
  label: string;
  Icon: LucideIcon;
  gated?: boolean;
  workspaceScoped?: boolean;
};

const items: Item[] = [
  { to: '/board', label: 'Board', Icon: LayoutGrid, workspaceScoped: true },
  { to: '/backlog', label: 'Backlog', Icon: ListTodo, workspaceScoped: true },
  { to: '/sprints', label: 'Sprints', Icon: CalendarRange, workspaceScoped: true },
  { to: '/sessions', label: 'Sessions', Icon: Terminal, workspaceScoped: true },
  { to: '/agent-groups', label: 'Agent groups', Icon: Users, workspaceScoped: true },
  { to: '/coach', label: 'Coach', Icon: CircuitBoard, workspaceScoped: true },
  { to: '/walk', label: 'Review', Icon: Footprints, workspaceScoped: true },
  { to: '/graph', label: 'Graph', Icon: Network, workspaceScoped: true },
  { to: '/insights', label: 'Insights', Icon: Sparkles, workspaceScoped: true },
  { to: '/escalations', label: 'Escalations', Icon: AlertTriangle, workspaceScoped: true },
  { to: '/capabilities', label: 'Capability Browser', Icon: Boxes },
  { to: '/drift', label: 'Drift', Icon: GitCompare, workspaceScoped: true },
  { to: '/mail', label: 'Mail', Icon: Mail, gated: true, workspaceScoped: true },
  { to: '/settings', label: 'Settings', Icon: Settings },
];

const COLD_START_TITLE = 'Available after creating or switching to a project.';

export function Sidebar() {
  const { activeProject, isLoading } = useProjectPicker();
  const visible = items.filter((i) => !i.gated || features.mail);
  // Cold-start = picker has finished its initial fetch and there is no
  // active project. Suppress the muted state during the initial fetch
  // so the sidebar doesn't flicker between muted and active.
  const coldStart = !isLoading && activeProject === null;

  return (
    <aside className="w-56 shrink-0 border-r border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950">
      <div className="flex h-12 items-center px-4 border-b border-neutral-200 dark:border-neutral-800">
        <span className="font-semibold tracking-tight">Gemba</span>
      </div>
      <nav className="flex flex-col gap-0.5 p-2" data-testid="sidebar-nav">
        {visible.map(({ to, label, Icon, workspaceScoped }) => {
          const muted = coldStart && workspaceScoped;
          if (muted) {
            return (
              <span
                key={to}
                role="link"
                aria-disabled="true"
                title={COLD_START_TITLE}
                data-testid={`sidebar-item-${to.replace(/^\//, '')}`}
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
              key={to}
              to={to}
              data-testid={`sidebar-item-${to.replace(/^\//, '')}`}
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
        })}
      </nav>
    </aside>
  );
}
