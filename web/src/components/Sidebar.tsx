import { Link, useLocation } from 'react-router-dom';
import {
  LayoutGrid,
  Network,
  Terminal,
  Footprints,
  AlertTriangle,
  Filter,
  Settings as SettingsIcon,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useProjectPicker } from './projectpicker/ProjectPickerContext';
import { useEscalations } from '@/hooks/useEscalations';
import { useCapabilities } from '@/capabilities';

// First-order panes per the 2026-04-28 ratification (gm-e12.19,
// second amendment), amended for Beads-only discoverability: Graph is
// now a visible rail item because dependency inspection is a core
// Beads-management surface, not a hidden Plan sub-route. Secondary
// surfaces — Backlog, Sprints, Coach, Agent groups, Drift, Capability
// Browser, Mail — survive as deep-link routes today and roll up under
// their pane's in-screen tabs as gm-e12.19.4-7 land.
//
// Three core panes (Board / Review / Triage) cluster at the top —
// these are the operator's daily verbs. Below a divider, Sessions
// and Settings sit as secondary / utility surfaces — the operator
// drops into them less often once the dispatcher is humming.
//
// Initial route map (until pane consolidation lands):
//   Board     → /board       status board
//   Graph     → /graph       dependency / relationship inspection
//   Review    → /walk        (Gemba walk surface)
//   Triage    → /escalations (gm-e12.19.6 grows a Drift tab)
//   Sessions  → /sessions    (gm-e12.19.5 grows tabs Sessions/Groups/Coach)
//   Settings  → /settings    (gm-e12.19.2 grows tabs for Adaptors/Agents/Mail)
//
// The user-facing label for /escalations is "Triage" — operators
// triage incoming attention. The route, the page title, and the
// EscalationRequest entity itself stay named Escalations; this is a
// chrome-only relabel (same pattern as Walk → Review).
//
// Insights pane was removed 2026-04-29 pending the Prometheus-proxy
// chain (gm-sf51 ratified Path A; gm-e9m0 backend + gm-e12.17.1
// frontend deferred). gm-flij re-enables the rail entry once those
// land. /insights routes survive in App.tsx as deep links so existing
// bookmarks don't 404 during the deferral.
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
  activePaths?: string[];
};

// Core panes — the operator's daily verbs. Refine sits between Board
// and Review (gm-3ofd) — it's the upstream planning surface for dense,
// hierarchical, and grouped work inspection, distinct from Board's
// execution/status kanban.
const items: Item[] = [
  {
    to: '/board',
    label: 'Board',
    Icon: LayoutGrid,
    workspaceScoped: true,
    activePaths: ['/board', '/grid', '/sprints'],
  },
  {
    to: '/graph',
    label: 'Graph',
    Icon: Network,
    workspaceScoped: true,
  },
  { to: '/refine', label: 'Refine', Icon: Filter, workspaceScoped: true, activePaths: ['/refine', '/backlog'] },
  { to: '/walk', label: 'Review', Icon: Footprints, workspaceScoped: true, activePaths: ['/walk', '/walks'] },
  {
    to: '/escalations',
    label: 'Triage',
    Icon: AlertTriangle,
    workspaceScoped: true,
    activePaths: ['/escalations', '/drift'],
  },
];

// Secondary / utility surfaces — bottom-anchored under a divider.
// Sessions is a runtime view rather than a daily destination; Settings
// is configuration. Both belong below the core verbs.
const secondaryItems: Item[] = [
  {
    to: '/sessions',
    label: 'Sessions',
    Icon: Terminal,
    workspaceScoped: true,
    activePaths: ['/sessions', '/agents', '/agent-groups', '/coach'],
  },
  {
    to: '/settings',
    label: 'Settings',
    Icon: SettingsIcon,
    activePaths: ['/settings', '/capabilities', '/project/config', '/health'],
  },
];

const COLD_START_TITLE = 'Available after creating or switching to a project.';

// EscalationBadge renders a numeric pill to the right of the label.
// Visible only when the query has resolved without error and the count
// is > 0. For counts >= 100, shows "99+".
function EscalationBadge() {
  const query = useEscalations();
  if (!query.isSuccess || query.isError) return null;
  const count = (query.data ?? []).length;
  if (count === 0) return null;
  const label = count >= 100 ? '99+' : String(count);
  return (
    <span
      data-testid="sidebar-escalations-badge"
      className="ml-auto rounded-full px-1.5 py-0.5 text-xs font-medium leading-none bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-200"
    >
      {label}
    </span>
  );
}

export function Sidebar() {
  const { activeProject, isLoading } = useProjectPicker();
  const { beadsOnly } = useCapabilities();
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
        {items.filter((item) => visibleInMode(item, beadsOnly)).map((item) => (
          <NavItem key={item.to} item={item} coldStart={coldStart} beadsOnly={beadsOnly} />
        ))}
      </nav>
      <div className="mt-auto flex flex-col gap-0.5 border-t border-neutral-200 p-2 dark:border-neutral-800">
        {secondaryItems.filter((item) => visibleInMode(item, beadsOnly)).map((item) => (
          <NavItem key={item.to} item={item} coldStart={coldStart} beadsOnly={beadsOnly} />
        ))}
      </div>
    </aside>
  );
}

function visibleInMode(item: Item, beadsOnly: boolean): boolean {
  if (!beadsOnly) return true;
  return item.to === '/board' || item.to === '/graph' || item.to === '/refine' || item.to === '/settings';
}

function NavItem({ item, coldStart, beadsOnly }: { item: Item; coldStart: boolean; beadsOnly: boolean }) {
  const { to, label, Icon, workspaceScoped } = item;
  const { pathname } = useLocation();
  const muted = !beadsOnly && coldStart && Boolean(workspaceScoped);
  const testId = `sidebar-item-${to.replace(/^\//, '')}`;
  const isEscalations = to === '/escalations';
  const active = isActivePath(pathname, item);
  const activeClass =
    'border border-cyan-400/70 border-l-4 bg-cyan-50 font-semibold text-cyan-950 shadow-sm dark:border-cyan-400/70 dark:bg-cyan-950/60 dark:text-cyan-50';

  if (muted) {
    // gm-root.17.12: muted styling indicates "operational on a workspace
    // only". Use neutral-500 (light) / neutral-500 (dark) directly
    // instead of the original neutral-600 with opacity-40 — opacity
    // dropped foreground to ~#a3a3a3 against #fafafa (2.41:1) which
    // failed WCAG 2 AA color-contrast. neutral-500 hits ~5.74:1 in
    // light mode and ~5.95:1 in dark mode while still reading as
    // visibly de-emphasized vs the active text (neutral-900 / white).
    // Badge is intentionally absent from the muted variant — there is
    // no active workspace to scope the count against (gm-e11.8.2).
    return (
      <span
        role="link"
        aria-disabled="true"
        aria-current={active ? 'page' : undefined}
        title={COLD_START_TITLE}
        data-testid={testId}
        data-disabled="true"
        data-active={active ? 'true' : undefined}
        className={cn(
          'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm',
          'cursor-not-allowed select-none',
          active ? activeClass : 'italic text-neutral-500 dark:text-neutral-500'
        )}
      >
        <Icon className="h-4 w-4" aria-hidden />
        <span>{label}</span>
      </span>
    );
  }

  return (
    <Link
      to={to}
      data-testid={testId}
      aria-current={active ? 'page' : undefined}
      data-active={active ? 'true' : undefined}
      className={cn(
        'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors',
        active
          ? activeClass
          : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900 dark:text-neutral-400 dark:hover:bg-neutral-900 dark:hover:text-white'
      )}
    >
      <Icon className="h-4 w-4" aria-hidden />
      <span>{label}</span>
      {isEscalations && <EscalationBadge />}
    </Link>
  );
}

function isActivePath(pathname: string, item: Item): boolean {
  const candidates = item.activePaths ?? [item.to];
  return candidates.some((path) => pathname === path || pathname.startsWith(`${path}/`));
}
