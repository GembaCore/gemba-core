import { Command, Moon, Sun, User, ChevronDown } from 'lucide-react';
import { useTheme } from '@/lib/theme-context';
import { usePalette } from '@/components/palette/PaletteContext';
import { cn } from '@/lib/utils';

export function Topbar() {
  const { theme, toggle } = useTheme();
  const { setOpen: setPaletteOpen } = usePalette();
  const resolved =
    theme === 'system'
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : theme;
  return (
    <header className="flex h-12 items-center gap-3 border-b border-neutral-200 bg-white px-4 dark:border-neutral-800 dark:bg-neutral-950">
      <button
        type="button"
        data-hotkey-target="workspace-switcher"
        className="inline-flex items-center gap-1.5 rounded-md border border-neutral-200 bg-neutral-50 px-2 py-1 text-sm text-neutral-700 hover:bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800"
      >
        <span>default</span>
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

      <div className="ml-auto flex items-center gap-1">
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
