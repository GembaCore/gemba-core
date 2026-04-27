import { useEffect, useState } from 'react';
import { Command, MessageSquare, Moon, Sun, User, ChevronDown } from 'lucide-react';
import { useTheme } from '@/lib/theme-context';
import { usePalette } from '@/components/palette/PaletteContext';
import { usePmPanel } from '@/components/pm/PmPanelContext';
import { GlobalInFlightCounter } from '@/components/sessions/GlobalInFlightCounter';
import { cn } from '@/lib/utils';
import { getServerConfig, type BeadsSource } from '@/api/config';

// dolt-url sources get a colored badge so operators running gemba
// against a remote/shared Dolt server can distinguish that case from a
// local --beads-dir at a glance (different blast radius for writes).
const KIND_BADGE: Record<string, string> = {
  'beads-dir':
    'border-neutral-200 bg-neutral-50 text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300',
  'dolt-url':
    'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200',
  unconfigured:
    'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-200',
};

export function Topbar() {
  const { theme, toggle } = useTheme();
  const { setOpen: setPaletteOpen } = usePalette();
  const pm = usePmPanel();
  const [beads, setBeads] = useState<BeadsSource | null>(null);
  useEffect(() => {
    let cancelled = false;
    getServerConfig()
      .then((cfg) => {
        if (!cancelled) setBeads(cfg.beads_source);
      })
      .catch(() => {
        // Best-effort: leave the placeholder label rather than alarming
        // the operator. Auth-protected fetches before login land here.
      });
    return () => {
      cancelled = true;
    };
  }, []);
  const resolved =
    theme === 'system'
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : theme;
  const label = beads?.label || (beads?.kind === 'unconfigured' ? 'no workspace' : '…');
  const tooltip = beads?.detail || beads?.kind || '';
  const badgeClass =
    KIND_BADGE[beads?.kind ?? ''] ??
    'border-neutral-200 bg-neutral-50 text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300';
  return (
    <header className="flex h-12 items-center gap-3 border-b border-neutral-200 bg-white px-4 dark:border-neutral-800 dark:bg-neutral-950">
      <button
        type="button"
        data-hotkey-target="workspace-switcher"
        data-testid="workspace-label"
        title={tooltip}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-sm hover:bg-neutral-100 dark:hover:bg-neutral-800',
          badgeClass
        )}
      >
        <span className="font-mono">{label}</span>
        <ChevronDown className="h-3.5 w-3.5 opacity-60" />
      </button>

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
