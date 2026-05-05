// Global command palette (gm-e12.6).
//
// Cmd-K (or 'p') opens the palette. The dialog is always mounted at
// the AppShell level — open-vs-closed flips a single boolean — so the
// first invocation has no module-loading or React-mounting latency.
// cmdk's own filter handles substring matching across every item's
// search keywords, which is what the bead's DoD ('search results
// stream in') asks for: as the user types, cmdk filters the always-
// loaded list locally without a round-trip.
//
// Item categories:
//
//   - Navigate: every primary route the SPA exposes.
//   - Actions:  imperative verbs (create work item, dispatch session,
//               open help). Each action either navigates or fires the
//               existing data-hotkey-target on the relevant page so
//               we don't re-implement dialogs the page already owns.
//   - Search:   live results pulled from the react-query cache via
//               useWorkItems / useAgents / useSessions /
//               useEscalations. No new fetches — the palette is a
//               navigation surface, not a data-fetch surface, so it
//               adds zero load to the cold path.

import { useCallback } from 'react';
import { Command } from 'cmdk';
import { useNavigate } from 'react-router-dom';
import {
  ClipboardList,
  Compass,
  HelpCircle,
  ListTodo,
  Network,
  Plus,
  Radio,
  Server,
  Sparkles,
  Workflow,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { usePalette } from './PaletteContext';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useSessions } from '@/hooks/useSessions';
import { useEscalations } from '@/hooks/useEscalations';

// PALETTE_RESULT_LIMIT caps each search-style group so a workspace
// with thousands of work items doesn't render a dialog the size of
// the viewport. cmdk's substring filter narrows further as the user
// types; this limit only governs the unfiltered initial state.
const PALETTE_RESULT_LIMIT = 12;

interface NavigateItem {
  id: string;
  label: string;
  description?: string;
  keywords: string;
  icon: LucideIcon;
  to: string;
}

const NAVIGATE_ITEMS: NavigateItem[] = [
  { id: 'nav-board', label: 'Board', keywords: 'board kanban epic swimlane', icon: ListTodo, to: '/board' },
  // Backlog/deferred grooming and dense grid work now live in Refine.
  { id: 'nav-backlog', label: 'Backlog', keywords: 'backlog deferred list planning', icon: ClipboardList, to: '/refine' },
  // Grid is an operator-learned alias for Refine's dense table mode.
  { id: 'nav-grid', label: 'Grid', keywords: 'grid table rows power', icon: Workflow, to: '/refine' },
  { id: 'nav-graph', label: 'Dependency graph', keywords: 'graph dependency network', icon: Network, to: '/graph' },
  { id: 'nav-sessions', label: 'Sessions', keywords: 'sessions panes runs', icon: Radio, to: '/sessions' },
  { id: 'nav-escalations', label: 'Escalations', keywords: 'escalations elicit hitl', icon: HelpCircle, to: '/escalations' },
  { id: 'nav-capabilities', label: 'Capabilities', keywords: 'capabilities adaptors manifest', icon: Server, to: '/capabilities' },
  { id: 'nav-insights', label: 'Insights', keywords: 'insights drift dashboard', icon: Compass, to: '/insights' },
];

export function Palette() {
  const { open, setOpen } = usePalette();
  const navigate = useNavigate();

  const close = useCallback(() => setOpen(false), [setOpen]);

  const goTo = useCallback(
    (path: string) => () => {
      navigate(path);
      close();
    },
    [navigate, close]
  );

  // clickAfterNav navigates to a page and then dispatches a click on
  // the named hotkey target so the palette can reuse the page's own
  // dialog rather than re-implement it. setTimeout(0) lets the route
  // mount before the query runs.
  const clickAfterNav = useCallback(
    (path: string, target: string) => () => {
      navigate(path);
      setTimeout(() => {
        const el = document.querySelector<HTMLElement>(`[data-hotkey-target="${target}"]`);
        // board-new-workitem is the testid the BoardPage actually
        // exposes today; gm-e12.6 follow-up could promote it to a
        // proper data-hotkey-target.
        const fallback = document.querySelector<HTMLElement>(`[data-testid="${target}"]`);
        (el ?? fallback)?.click();
      }, 0);
      close();
    },
    [navigate, close]
  );

  // Source data — react-query already has these warm for any
  // navigated page. Disable retry-blocking with empty arrays so an
  // adaptor-degraded backend doesn't break the palette.
  const { data: workItems = [] } = useWorkItems();
  const { data: sessions = [] } = useSessions();
  const { data: escalations = [] } = useEscalations();

  return (
    <Command.Dialog
      open={open}
      onOpenChange={setOpen}
      label="Command palette"
      data-testid="command-palette-dialog"
      shouldFilter
      // Reset the search box every time the palette opens so the next
      // invocation always starts from a clean state — operators
      // hitting Cmd+K again expect a fresh search, not yesterday's
      // filter still active.
      key={open ? 'open' : 'closed'}
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 px-4 pt-[10vh]"
    >
      <div className="w-full max-w-xl overflow-hidden rounded-lg border border-neutral-200 bg-white shadow-xl dark:border-neutral-800 dark:bg-neutral-950">
        <Command.Input
          placeholder="Search or run a command…"
          className="w-full border-b border-neutral-200 bg-transparent px-4 py-3 text-sm outline-none placeholder:text-neutral-400 dark:border-neutral-800"
          data-testid="command-palette-input"
        />
        <Command.List className="max-h-80 overflow-auto p-1 text-sm">
          <Command.Empty className="px-3 py-2 text-sm text-neutral-500">
            No results.
          </Command.Empty>

          <Command.Group heading="Navigate" className="text-xs uppercase tracking-wide text-neutral-500">
            {NAVIGATE_ITEMS.map((it) => (
              <PaletteRow
                key={it.id}
                value={it.id}
                keywords={[it.label, it.keywords]}
                icon={it.icon}
                label={it.label}
                onSelect={goTo(it.to)}
              />
            ))}
          </Command.Group>

          <Command.Group heading="Actions" className="text-xs uppercase tracking-wide text-neutral-500">
            <PaletteRow
              value="action-new-work-item"
              keywords={['Create', 'New', 'Work item', 'Bead']}
              icon={Plus}
              label="Create work item"
              hint="Open the New work-item dialog on the board"
              onSelect={clickAfterNav('/board', 'board-new-workitem')}
            />
            <PaletteRow
              value="action-new-session"
              keywords={['Dispatch', 'Spawn', 'Session', 'Agent']}
              icon={Sparkles}
              label="Dispatch session"
              hint="Open the New session dialog on Sessions"
              onSelect={clickAfterNav('/sessions', 'new-session')}
            />
            <PaletteRow
              value="action-help"
              keywords={['Help', 'Shortcuts', 'Keyboard']}
              icon={HelpCircle}
              label="Show keyboard shortcuts"
              onSelect={() => {
                close();
                // Defer one tick so the dialog's own keystroke
                // doesn't conflict with the help-overlay binding.
                setTimeout(() => {
                  document.dispatchEvent(new KeyboardEvent('keydown', { key: '?' }));
                }, 0);
              }}
            />
          </Command.Group>

          {workItems.length > 0 ? (
            <Command.Group heading="Work items" className="text-xs uppercase tracking-wide text-neutral-500">
              {workItems.slice(0, PALETTE_RESULT_LIMIT).map((wi) => {
                const shortID = String(wi.id).split('/').pop() ?? wi.id;
                return (
                  <PaletteRow
                    key={`wi-${wi.id}`}
                    value={`wi-${wi.id}`}
                    keywords={[shortID, wi.title, wi.kind, wi.state_category]}
                    icon={ListTodo}
                    label={wi.title || String(shortID)}
                    hint={`${shortID} • ${wi.state_category}`}
                    onSelect={() => {
                      navigate(`/board?bead=${encodeURIComponent(String(wi.id))}`);
                      close();
                    }}
                  />
                );
              })}
            </Command.Group>
          ) : null}

          {sessions.length > 0 ? (
            <Command.Group heading="Sessions" className="text-xs uppercase tracking-wide text-neutral-500">
              {sessions.slice(0, PALETTE_RESULT_LIMIT).map((s) => (
                <PaletteRow
                  key={`se-${s.id}`}
                  value={`se-${s.id}`}
                  keywords={[s.id, s.assignment_id, s.status, String(s.agent_id ?? '')]}
                  icon={Radio}
                  label={s.assignment_id || s.id}
                  hint={`${s.status} • ${s.agent_id ?? '—'}`}
                  onSelect={goTo('/sessions')}
                />
              ))}
            </Command.Group>
          ) : null}

          {escalations.length > 0 ? (
            <Command.Group heading="Escalations" className="text-xs uppercase tracking-wide text-neutral-500">
              {escalations.slice(0, PALETTE_RESULT_LIMIT).map((e) => (
                <PaletteRow
                  key={`es-${e.id}`}
                  value={`es-${e.id}`}
                  keywords={[e.id, e.source, e.title, e.assignment_id ?? '']}
                  icon={HelpCircle}
                  label={e.title || e.source}
                  hint={e.assignment_id ? `assignment ${e.assignment_id}` : e.source}
                  onSelect={goTo('/escalations')}
                />
              ))}
            </Command.Group>
          ) : null}
        </Command.List>
      </div>
    </Command.Dialog>
  );
}

interface PaletteRowProps {
  value: string;
  keywords: string[];
  icon: LucideIcon;
  label: string;
  hint?: string;
  onSelect: () => void;
}

function PaletteRow({ value, keywords, icon: Icon, label, hint, onSelect }: PaletteRowProps) {
  return (
    <Command.Item
      value={value}
      keywords={keywords}
      onSelect={onSelect}
      className="flex cursor-pointer items-center gap-2 rounded px-3 py-2 text-sm text-neutral-700 aria-selected:bg-neutral-100 dark:text-neutral-200 dark:aria-selected:bg-neutral-800"
      data-testid={`palette-item-${value}`}
    >
      <Icon className="h-4 w-4 shrink-0 text-neutral-500" aria-hidden />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {hint ? (
        <span className="ml-2 truncate text-xs text-neutral-400">{hint}</span>
      ) : null}
    </Command.Item>
  );
}
