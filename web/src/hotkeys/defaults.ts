import type { Hotkey } from './types';
import type { HotkeyRegistry } from './registry';

// Centralized default registrations. Keep this list as the single source of
// truth for the in-app help overlay + user-facing docs (gm-e14.4).
//
// Conflict rules:
//   - Never shadow browser/OS defaults (Cmd+W, Cmd+T, Cmd+R, F5, etc.).
//   - `Mod+k` stays reserved for the palette (Topbar already announces it).
//   - `?` (Shift+/) is the help discoverability key — always global.
export const DEFAULT_HOTKEYS: Hotkey[] = [
  // Navigation
  { id: 'row-down', keys: ['j'], description: 'Next row', category: 'navigation' },
  { id: 'row-up', keys: ['k'], description: 'Previous row', category: 'navigation' },
  { id: 'goto-top', keys: ['g', 'g'], description: 'Jump to top', category: 'navigation' },
  { id: 'goto-bottom', keys: ['G'], description: 'Jump to bottom', category: 'navigation' },
  { id: 'focus-search', keys: ['/'], description: 'Focus search', category: 'navigation' },
  { id: 'open-palette', keys: ['p'], description: 'Open command palette', category: 'navigation' },

  // Selection
  { id: 'select-toggle', keys: ['Space'], description: 'Toggle selection', category: 'selection' },
  { id: 'select-all', keys: ['*', 'a'], description: 'Select all', category: 'selection' },
  { id: 'select-invert', keys: ['*', 'i'], description: 'Invert selection', category: 'selection' },

  // Bulk
  { id: 'bulk-edit', keys: ['*', 'e'], description: 'Bulk edit', category: 'bulk' },
  { id: 'bulk-done', keys: ['*', 'd'], description: 'Bulk mark done', category: 'bulk' },
  { id: 'bulk-delete', keys: ['*', 'x'], description: 'Bulk delete', category: 'bulk' },
  { id: 'bulk-label', keys: ['*', 'l'], description: 'Bulk label', category: 'bulk' },

  // Creation
  { id: 'new', keys: ['n'], description: 'New item', category: 'creation' },
  { id: 'clone', keys: ['c'], description: 'Clone item', category: 'creation' },

  // Views (1-5 numeric quick switch + named view jumps)
  { id: 'view-1', keys: ['1'], description: 'Board view', category: 'views' },
  { id: 'view-2', keys: ['2'], description: 'Backlog view', category: 'views' },
  { id: 'view-3', keys: ['3'], description: 'Graph view', category: 'views' },
  { id: 'view-4', keys: ['4'], description: 'Insights view', category: 'views' },
  { id: 'view-5', keys: ['5'], description: 'Escalations view', category: 'views' },
  { id: 'workspace-switch', keys: ['W'], description: 'Switch workspace', category: 'views' },
  { id: 'capability-browser', keys: ['C'], description: 'Capability browser', category: 'views' },
  { id: 'drift-view', keys: ['D'], description: 'Drift view', category: 'views' },
  // Board: toggle Epic-primary (default) ↔ WorkItem-flat (alt) per
  // ui-spec L293. Note: most browsers reserve Cmd-Shift-W for "close
  // all windows"; when the OS swallows the keystroke the shortcut is a
  // no-op and operators must use the in-page toggle instead.
  {
    id: 'view-toggle-board',
    keys: ['Mod+W'],
    description: 'Toggle board view (Epic / Work item)',
    category: 'views',
  },

  // Drawer / panel
  { id: 'drawer-open', keys: ['o'], description: 'Open detail drawer', category: 'drawer' },
  {
    id: 'drawer-close',
    keys: ['Escape'],
    description: 'Close drawer / overlay',
    category: 'drawer',
  },
  { id: 'fold', keys: ['f'], description: 'Fold / collapse pane', category: 'drawer' },

  // Help
  { id: 'help', keys: ['?'], description: 'Show all shortcuts', category: 'help' },
];

export function registerDefaults(registry: HotkeyRegistry): () => void {
  const disposers = DEFAULT_HOTKEYS.map((hk) => registry.register(hk));
  return () => disposers.forEach((d) => d());
}
