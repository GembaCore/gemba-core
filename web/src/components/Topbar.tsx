// gm-root.18: the old static workspace-label button is replaced by the
// ProjectPicker dropdown. The BeadsSource fetch (getServerConfig) is
// kept only as a tooltip on the picker pill via the context — the label
// is now the active project name, not the beads-dir basename.
import { Command, MessageSquare, Moon, Sun, User } from 'lucide-react';
import { useTheme } from '@/lib/theme-context';
import { usePalette } from '@/components/palette/PaletteContext';
import { usePmPanel } from '@/components/pm/PmPanelContext';
import { GlobalInFlightCounter } from '@/components/sessions/GlobalInFlightCounter';
// gm-root.26 item 2: live-session count badge sits next to the
// project picker so first-time users discover /sessions even when
// the spawned agent runs in a separate pane window.
import { LiveSessionsBadge } from '@/components/sessions/LiveSessionsBadge';
import { cn } from '@/lib/utils';
import { ProjectPicker } from '@/components/projectpicker/ProjectPicker';
import { NewProjectAffordance } from '@/components/projectpicker/NewProjectAffordance';

export function Topbar() {
  const { theme, toggle } = useTheme();
  const { setOpen: setPaletteOpen } = usePalette();
  const pm = usePmPanel();
  const resolved =
    theme === 'system'
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : theme;
  return (
    <header className="flex h-12 items-center gap-3 border-b border-neutral-200 bg-white px-4 dark:border-neutral-800 dark:bg-neutral-950">
      {/* gm-root.17.2: "+" New project affordance — sibling chrome element
          immediately to the LEFT of the picker. Navigates to /new. */}
      <NewProjectAffordance />
      <ProjectPicker />
      <LiveSessionsBadge />

      <button
        type="button"
        data-hotkey-target="command-palette"
        onClick={() => setPaletteOpen(true)}
        className={cn(
          'ml-2 inline-flex items-center gap-2 rounded-md border border-neutral-200 px-2 py-1 text-sm text-neutral-500',
          'hover:bg-neutral-100 dark:border-neutral-800 dark:text-neutral-400 dark:hover:bg-neutral-900'
        )}
        aria-label="Open command palette"
      >
        <Command className="h-3.5 w-3.5" />
        <span>Search or command…</span>
        <kbd className="ml-2 rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] font-mono text-neutral-700 dark:bg-neutral-800 dark:text-neutral-200">
          ⌘K
        </kbd>
      </button>

      <div className="ml-auto flex items-center gap-2">
        <GlobalInFlightCounter />
        <button
          type="button"
          onClick={pm.toggle}
          aria-label="Toggle PM panel"
          aria-pressed={pm.open}
          data-testid="pm-panel-toggle"
          data-hotkey-target="pm-panel-toggle"
          className={cn(
            'inline-flex h-8 w-8 items-center justify-center rounded-md text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-900',
            pm.open && 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300'
          )}
        >
          <MessageSquare className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={toggle}
          aria-label="Toggle theme"
          className="inline-flex h-8 w-8 items-center justify-center rounded-md text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-900"
        >
          {resolved === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </button>
        <button
          type="button"
          aria-label="User menu"
          className="inline-flex h-8 w-8 items-center justify-center rounded-md text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-900"
        >
          <User className="h-4 w-4" />
        </button>
      </div>
    </header>
  );
}
