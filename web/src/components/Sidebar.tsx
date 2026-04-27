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
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { features } from '@/lib/features';

type Item = { to: string; label: string; Icon: LucideIcon; gated?: boolean };

const items: Item[] = [
  { to: '/board', label: 'Board', Icon: LayoutGrid },
  { to: '/backlog', label: 'Backlog', Icon: ListTodo },
  { to: '/sessions', label: 'Sessions', Icon: Terminal },
  { to: '/agent-groups', label: 'Agent groups', Icon: Users },
  { to: '/coach', label: 'Coach', Icon: CircuitBoard },
  { to: '/walk', label: 'Gemba walk', Icon: Footprints },
  { to: '/graph', label: 'Graph', Icon: Network },
  { to: '/insights', label: 'Insights', Icon: Sparkles },
  { to: '/escalations', label: 'Escalations', Icon: AlertTriangle },
  { to: '/capabilities', label: 'Capability Browser', Icon: Boxes },
  { to: '/drift', label: 'Drift', Icon: GitCompare },
  { to: '/mail', label: 'Mail', Icon: Mail, gated: true },
  { to: '/settings', label: 'Settings', Icon: Settings },
];

export function Sidebar() {
  const visible = items.filter((i) => !i.gated || features.mail);
  return (
    <aside className="w-56 shrink-0 border-r border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950">
      <div className="flex h-12 items-center px-4 border-b border-neutral-200 dark:border-neutral-800">
        <span className="font-semibold tracking-tight">Gemba</span>
      </div>
      <nav className="flex flex-col gap-0.5 p-2">
        {visible.map(({ to, label, Icon }) => (
          <NavLink
            key={to}
            to={to}
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
        ))}
      </nav>
    </aside>
  );
}
