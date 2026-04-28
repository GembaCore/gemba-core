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

  // Gemba walk navigation/lifecycle (gm-0tl). The four 'w / n / d /
  // Enter' chords from gemba-walk.md §UI live in two scopes:
  //
  //   - 'walk-toggle-panel' (`w`) is global — pressing it from any
  //     route navigates to /walk; pressing again from /walk returns
  //     to the prior route. AppHotkeys owns the handler.
  //   - 'walk-next' (`n`), 'walk-defer' (`d`), and 'walk-suggested-apply'
  //     (Enter) live under the 'walk-active' scope, which AppWalkBindings
  //     pushes whenever walk.active is true. That keeps the chords inert
  //     outside an active walk so they don't shadow the global 'new'
  //     placeholder + Enter focus semantics.
  //
  // Registration order matters: 'walk-next' MUST come before 'new'
  // (and 'walk-defer' before any other `d` chord) because the matcher
  // returns the first scope-active hotkey via `find`. When walk-active
  // is on, 'n' / 'd' / Enter resolve to the walk verbs; when it's off,
  // they fall through to 'new' / Enter graph navigation / etc.
  {
    id: 'walk-toggle-panel',
    keys: ['w'],
    description: 'Toggle Gemba walk panel',
    category: 'walk',
  },
  {
    id: 'walk-next',
    keys: ['n'],
    description: 'Walk: Next agenda item',
    category: 'walk',
    scope: 'walk-active',
  },
  {
    id: 'walk-suggested-apply',
    keys: ['Enter'],
    description: 'Walk: Apply focused suggested action',
    category: 'walk',
    scope: 'walk-active',
  },

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
  // scope='graph' on mount), so ArrowUp/Down/Enter on Board or
  // Grid keep their existing semantics.
  //
  // Vertical keys, not horizontal: the layout is top-down (roots at
  // the top, descendants below — graphLayout.ts §axes). ArrowDown
  // steps toward a successor (downstream); ArrowUp steps back to a
  // predecessor (upstream). Forward stepping is only enabled when
  // the focused node has exactly one outgoing structural edge — when
  // ambiguous the binding is a no-op so the operator falls back to
  // clicking. Back stepping is always enabled when at least one
  // predecessor exists; an ambiguous case prefers the most recently
  // visited predecessor.
  { id: 'graph-next', keys: ['ArrowDown'], description: 'Graph: step down to next node (when unambiguous)', category: 'navigation', scope: 'graph' },
  { id: 'graph-back', keys: ['ArrowUp'], description: 'Graph: step up to previous node', category: 'navigation', scope: 'graph' },
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
  // gm-96l: open-and-focus is a separate id from toggle so the
  // operator can summon a fresh prompt input without first
  // collapsing an already-open panel. Cmd+K is reserved for the
  // command palette (PaletteContext owns it); the bead spec asks
  // for Cmd+K → focus PM input but the palette precedent wins
  // here. The new chord is Cmd+Shift+K — close enough to muscle
  // memory that an operator typing 'PM panel' lands on it without
  // a docs lookup.
  {
    id: 'pm-panel-focus-input',
    keys: ['Mod+Shift+K'],
    description: 'PM panel: open and focus input',
    category: 'drawer',
  },
  // gm-96l: cycle the active persona forward (palette-style). The
  // bead spec asks for Cmd+P; that chord already toggles the panel
  // (and the existing test pins it). Use Alt+P so the cycle never
  // shadows toggle — both can ship without one having to become a
  // chord prefix.
  {
    id: 'pm-panel-cycle-persona',
    keys: ['Alt+p'],
    description: 'PM panel: cycle persona',
    category: 'drawer',
  },
  // PM panel collapse rides on the existing 'drawer-close' id rather
  // than a separate registration. The matcher fires the FIRST chord
  // match — registering two ids for the same chord would mean only
  // one ever fires. Multiple subscribers to a single id all run, so
  // PM panel + HelpOverlay + WorkItem drawer can all listen for
  // drawer-close and gate on their own open state.

  // Gemba walk (gm-uipx.2). Cmd-G is the global toggle; the four
  // R/M/X/D verbs are scoped so they only fire while the walk
  // surface is mounted (WalkPage pushes scope='walk'). Browsers
  // reserve Cmd-G for "find next" — when the OS swallows the
  // keystroke the operator falls back to the sidebar entry or
  // the resume button on the active-walk banner.
  {
    id: 'walk-toggle',
    keys: ['Mod+G'],
    description: 'Toggle Gemba walk',
    category: 'navigation',
  },
  {
    id: 'walk-ratify',
    keys: ['r'],
    description: 'Walk: Ratify active item',
    category: 'walk',
    scope: 'walk',
  },
  {
    id: 'walk-modify',
    keys: ['m'],
    description: 'Walk: Modify active item',
    category: 'walk',
    scope: 'walk',
  },
  {
    id: 'walk-reject',
    keys: ['x'],
    description: 'Walk: Reject active item',
    category: 'walk',
    scope: 'walk',
  },
  // walk-defer matches the R/M/X/D toolbar verbs at scope 'walk'.
  // gm-0tl initially moved this to 'walk-active' to enable defer-
  // from-anywhere; that change broke the page-level binding's
  // mount-vs-effect timing in tests (the AppWalkBindings scope push
  // is async-relative-to-mount, while WalkPage's scope push is
  // synchronous). Reverted here for parity with R/M/X. A separate
  // chord like 'walk-defer-global' can land in scope 'walk-active'
  // when the defer-from-anywhere use case has its own bead.
  {
    id: 'walk-defer',
    keys: ['d'],
    description: 'Walk: Defer active item',
    category: 'walk',
    scope: 'walk',
  },

  // Help
  { id: 'help', keys: ['?'], description: 'Show all shortcuts', category: 'help' },
];

export function registerDefaults(registry: HotkeyRegistry): () => void {
  const disposers = DEFAULT_HOTKEYS.map((hk) => registry.register(hk));
  return () => disposers.forEach((d) => d());
}
