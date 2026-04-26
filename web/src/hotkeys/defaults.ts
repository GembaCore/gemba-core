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
  { id: 'view-2', keys: ['2'], description: 'Backlog (Board list mode)', category: 'views' },
  { id: 'view-3', keys: ['3'], description: 'Graph view', category: 'views' },
  { id: 'view-4', keys: ['4'], description: 'Insights view', category: 'views' },
  { id: 'view-5', keys: ['5'], description: 'Escalations view', category: 'views' },
  { id: 'workspace-switch', keys: ['W'], description: 'Switch workspace', category: 'views' },
  { id: 'capability-browser', keys: ['C'], description: 'Capability browser', category: 'views' },
  { id: 'drift-view', keys: ['D'], description: 'Drift view', category: 'views' },
  { id: 'sessions-view', keys: ['S'], description: 'Sessions view', category: 'views' },
  // gm-native.15: global "new session" dispatcher. ⌘⇧S navigates to
  // /sessions and clicks the New-Session button so the operator can
  // spin a pane from anywhere in the SPA.
  {
    id: 'new-session',
    keys: ['Mod+Shift+S'],
    description: 'New session',
    category: 'creation',
  },
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
  // Board: toggle list mode (former Backlog surface) ↔ kanban per
  // ui-spec §4.10 (gm-e12.19.1).
  {
    id: 'view-toggle-list',
    keys: ['Mod+Shift+L'],
    description: 'Toggle board list mode (Kanban / List)',
    category: 'views',
  },

  // Graph traversal (gm-e12.20). Scoped to the GraphPage — these
  // bindings only fire while /graph is mounted (the page pushes
  // scope='graph' on mount), so ArrowLeft/Right/Enter on Board or
  // Grid keep their existing semantics.
  //
  // Forward stepping is only enabled when the focused node has
  // exactly one outgoing structural edge — when ambiguous the
  // binding is a no-op so the operator falls back to clicking. Back
  // stepping is always enabled when at least one predecessor exists;
  // an ambiguous case prefers the most recently visited predecessor.
  { id: 'graph-next', keys: ['ArrowRight'], description: 'Graph: step to next node (when unambiguous)', category: 'navigation', scope: 'graph' },
  { id: 'graph-back', keys: ['ArrowLeft'], description: 'Graph: step back to previous node', category: 'navigation', scope: 'graph' },
  { id: 'graph-open', keys: ['Enter'], description: 'Graph: open focused node detail', category: 'drawer', scope: 'graph' },

  // Drawer / panel
  { id: 'drawer-open', keys: ['o'], description: 'Open detail drawer', category: 'drawer' },
  {
    id: 'drawer-close',
    keys: ['Escape'],
    description: 'Close drawer / overlay',
    category: 'drawer',
  },
  { id: 'fold', keys: ['f'], description: 'Fold / collapse pane', category: 'drawer' },
  // PM panel (gm-uipx.12) — bottom drawer toggle. ui-spec §2.4 calls
  // for Cmd-P / Ctrl-P, but the browser swallows that for the print
  // dialog. The runtime chord is Cmd-Shift-P (registered as 'Mod+P'
  // because the matcher canonicalises uppercase letter + Mod into
  // the form e.key emits when Shift is held). The Topbar toggle
  // button is the authoritative path; the hotkey is a convenience.
  // Per ui-spec §2.4 the panel is global — every route binds this
  // hotkey so the operator can summon it from anywhere.
  {
    id: 'pm-panel-toggle',
    keys: ['Mod+P'],
    description: 'Toggle PM panel',
    category: 'drawer',
  },
  // PM panel collapse rides on the existing 'drawer-close' id rather
  // than a separate registration. The matcher fires the FIRST chord
  // match — registering two ids for the same chord would mean only
  // one ever fires. Multiple subscribers to a single id all run, so
  // PM panel + HelpOverlay + WorkItem drawer can all listen for
  // drawer-close and gate on their own open state.

  // Help
  { id: 'help', keys: ['?'], description: 'Show all shortcuts', category: 'help' },
];

export function registerDefaults(registry: HotkeyRegistry): () => void {
  const disposers = DEFAULT_HOTKEYS.map((hk) => registry.register(hk));
  return () => disposers.forEach((d) => d());
}
