// PM panel — always-available right-side advisor surface (gm-96l).
//
// Originally shipped as a bottom drawer (gm-uipx.12). gm-96l
// flips the surface to a 320px right-edge panel with a permanent
// chevron handle that's visible whether the panel is collapsed or
// expanded — the operator never has to hunt for the entry point.
//
// Layout:
//
//   collapsed → only the ChevronHandle renders (right edge, 24px wide)
//   expanded  → ChevronHandle + 320px panel containing
//                  Header (persona dropdown, cost, close)
//                  Quick actions (view-aware row)
//                  Conversation OR Walk takeover
//                  Input
//
// Behaviour:
//   - Collapsed by default; open-state persisted across reloads.
//   - View-aware quick actions driven by react-router's pathname.
//   - Variety-aware labelling — Coach personas get 'Ask <name>',
//     Manager personas get 'Consult <name>'.
//   - Hotkeys: Mod+P toggle, Mod+Shift+K open-and-focus, Alt+P
//     cycle persona, Esc collapse. Wired through the gm-7hj
//     hotkey registry, not raw window listeners.
//   - Persistent conversation across nav (PmPanelContext +
//     localStorage gemba.pm-panel.transcript).

import { useEffect, useRef } from 'react';
import { CheckCircle2, ChevronLeft, ChevronRight, MessageSquare, X } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useHotkey } from '@/hotkeys';
import { cn } from '@/lib/utils';
import { totalCost, usePmPanel } from './PmPanelContext';
import {
  labelPrefixForVariety,
  quickActionsForPath,
  type QuickAction as QuickActionDef,
} from './quickActions';
import type { Persona, Turn } from './types';

// PANEL_WIDTH_PX is the bead-spec 320px canonical width. Surfaced
// as a constant so AppShell can offset main content when the
// workspace mode wants push-not-overlay behaviour.
export const PM_PANEL_WIDTH_PX = 320;

export function PmPanel(): JSX.Element {
  const panel = usePmPanel();

  // Cmd-Shift-P toggle (browsers reserve Cmd-P for print so the
  // canonical chord here lands as Cmd-Shift-P; see the matcher
  // canonicalisation in hotkeys/keys.ts).
  useHotkey('pm-panel-toggle', () => {
    panel.toggle();
  });
  // gm-96l: Cmd-Shift-K opens the panel AND focuses the input.
  useHotkey('pm-panel-focus-input', () => {
    panel.openAndFocus();
  });
  // gm-96l: Alt-P cycles the active persona forward.
  useHotkey('pm-panel-cycle-persona', () => {
    panel.cyclePersona(1);
  });
  // Escape collapses without losing conversation per ui-spec §2.4
  // and gm-96l. Gated on panel.open so a closed panel doesn't
  // shadow other Esc consumers (HelpOverlay, drawers).
  useHotkey('drawer-close', () => {
    if (panel.open) panel.setOpen(false);
  });

  return (
    <>
      <ChevronHandle />
      {panel.open ? <ExpandedPanel /> : null}
    </>
  );
}

// ChevronHandle is the always-visible right-edge entry point.
// Click toggles the panel. Anchored at top-right under the topbar
// so it doesn't fight the workspace pill or theme toggle.
function ChevronHandle(): JSX.Element {
  const panel = usePmPanel();
  return (
    <button
      type="button"
      data-testid="pm-panel-chevron"
      data-hotkey-target="pm-panel-chevron"
      aria-label={panel.open ? 'Collapse PM panel' : 'Expand PM panel'}
      aria-expanded={panel.open}
      onClick={panel.toggle}
      className={cn(
        'fixed right-0 top-1/2 z-30 flex -translate-y-1/2 items-center justify-center',
        'h-16 w-6 rounded-l-md border border-r-0 border-neutral-200 bg-white shadow-sm',
        'text-neutral-500 hover:bg-sky-50 hover:text-sky-700',
        'dark:border-neutral-800 dark:bg-neutral-950 dark:hover:bg-sky-950 dark:hover:text-sky-300',
        // When the panel is open the handle slides left so it sits
        // flush with the panel's left edge — same chevron, same
        // hit target, just translated.
        panel.open && 'right-[320px]'
      )}
      style={panel.open ? { right: PM_PANEL_WIDTH_PX } : undefined}
    >
      {panel.open ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
    </button>
  );
}

function ExpandedPanel(): JSX.Element {
  const panel = usePmPanel();
  return (
    <section
      role="complementary"
      aria-label="Project Manager panel"
      data-testid="pm-panel"
      style={{ width: PM_PANEL_WIDTH_PX }}
      className={cn(
        'fixed right-0 top-0 z-30 flex h-full flex-col border-l border-neutral-200 bg-white shadow-2xl',
        'dark:border-neutral-800 dark:bg-neutral-950'
      )}
    >
      <PmPanelHeader />
      <QuickActionRow />
      {panel.walkActive ? <WalkTakeover /> : <ConversationView />}
      <PmPanelInput />
    </section>
  );
}

function PmPanelHeader(): JSX.Element {
  const panel = usePmPanel();
  return (
    <header
      data-testid="pm-panel-header"
      className="flex items-center gap-3 border-b border-neutral-200 px-3 py-2 text-xs dark:border-neutral-800"
    >
      <PersonaDropdown />
      <CostDisplay />
      <button
        type="button"
        aria-label="Collapse PM panel"
        data-testid="pm-panel-close"
        onClick={() => panel.setOpen(false)}
        className="ml-auto rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-white"
      >
        <X className="h-4 w-4" />
      </button>
    </header>
  );
}

function PersonaDropdown(): JSX.Element {
  const panel = usePmPanel();
  return (
    <label className="inline-flex items-center gap-1.5">
      <span className="text-neutral-500">Persona</span>
      <select
        data-testid="pm-panel-persona"
        value={panel.personaId}
        onChange={(e) => panel.setPersonaId(e.target.value)}
        className="rounded border border-neutral-300 bg-white px-1.5 py-0.5 text-xs dark:border-neutral-700 dark:bg-neutral-900"
      >
        {panel.personas.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
    </label>
  );
}

function CostDisplay(): JSX.Element {
  const panel = usePmPanel();
  const total = totalCost(panel.turns);
  return (
    <span
      data-testid="pm-panel-cost"
      className="font-mono text-[11px] text-neutral-500"
      title={`${panel.turns.length} turn${panel.turns.length === 1 ? '' : 's'} this session`}
    >
      ${total.toFixed(2)} this session
    </span>
  );
}

// QuickActionRow — view-aware buttons at the top of the panel
// body. Driven by quickActionsForPath; variety-aware via the
// active persona's kind. Empty arrays render nothing (e.g. on
// /walk where the takeover owns the body).
function QuickActionRow(): JSX.Element | null {
  const panel = usePmPanel();
  // useLocation throws when there's no Router in the tree (e.g.
  // unit tests that mount PmPanel bare). Guard via the hook's
  // documented contract: react-router exposes useLocation only
  // inside a Router. We swallow via try/catch on the hook value
  // shape — the simpler path is to read location.pathname under
  // a defensive default when absent.
  const pathname = useSafePathname();
  const navigate = useNavigate();
  const actions = quickActionsForPath(pathname);
  if (actions.length === 0) return null;

  const active = panel.personas.find((p) => p.id === panel.personaId);
  const prefix = labelPrefixForVariety(active?.kind);
  const personaName = active?.name ?? 'PM';

  return (
    <div
      data-testid="pm-panel-quick-actions"
      className="flex flex-wrap gap-1.5 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800"
    >
      {actions.map((a) => (
        <QuickAction
          key={a.id}
          action={a}
          variety={prefix}
          personaName={personaName}
          onClick={() => {
            // gm-szz0.1: actions with routeTo navigate to the
            // canonical surface for that verb instead of seeding
            // the panel input (e.g. recommend-order → /sprints).
            // The panel itself stays in whatever open/closed state
            // the operator chose so navigation never steals focus
            // from a partial draft.
            if (a.routeTo) {
              navigate(a.routeTo);
              return;
            }
            panel.openAndFocus(a.prompt);
          }}
        />
      ))}
    </div>
  );
}

function useSafePathname(): string {
  // PmPanel always mounts inside <AppShell>, which is rendered
  // inside react-router's <Routes>. Tests that mount PmPanel bare
  // wrap it in a <MemoryRouter> — see PmPanel.test.tsx's `wrap`
  // helper. Either way useLocation has a Router above it.
  return useLocation().pathname;
}

interface QuickActionProps {
  action: QuickActionDef;
  variety: 'Ask' | 'Consult';
  personaName: string;
  onClick: () => void;
}

function QuickAction({ action, variety, personaName, onClick }: QuickActionProps): JSX.Element {
  // 'Ask PM' / 'Consult PM' style label. The action.id 'ask-pm'
  // is the generic fallback; for specific verbs the verb is the
  // label and the persona name is implied by the dropdown above.
  const isGeneric = action.id === 'ask-pm';
  const label = isGeneric ? `${variety} ${personaName}` : action.label;
  return (
    <button
      type="button"
      data-testid={`pm-panel-quick-${action.id}`}
      data-action-id={action.id}
      onClick={onClick}
      className="rounded border border-neutral-300 bg-white px-2 py-1 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
    >
      {label}
    </button>
  );
}

function ConversationView(): JSX.Element {
  const panel = usePmPanel();
  if (panel.turns.length === 0) {
    return (
      <div
        className="flex-1 overflow-y-auto px-4 py-6 text-sm text-neutral-500"
        data-testid="pm-panel-empty"
      >
        <div className="mb-3 flex items-center gap-2">
          <MessageSquare className="h-4 w-4" aria-hidden />
          <span>Ask a Coach or consult a Manager…</span>
        </div>
        {/* Legacy v1 placeholders — kept so existing tests that pin
            'pm-panel-quick-plan-next-sprint' still resolve. The
            view-aware QuickActionRow above carries the gm-96l
            spec; this row is a back-compat tail. */}
        <div className="flex flex-wrap gap-2 text-xs">
          <LegacyQuickAction label="Plan next sprint" />
          <LegacyQuickAction label="Audit risk register" />
          <LegacyQuickAction label="What changed today?" />
        </div>
      </div>
    );
  }
  return (
    <ol
      data-testid="pm-panel-conversation"
      className="flex-1 space-y-3 overflow-y-auto px-4 py-3 font-mono text-xs"
    >
      {panel.turns.map((turn) => (
        <TurnView key={turn.id} turn={turn} />
      ))}
    </ol>
  );
}

function LegacyQuickAction({ label }: { label: string }): JSX.Element {
  return (
    <button
      type="button"
      data-testid={`pm-panel-quick-${label.replace(/\s+/g, '-').toLowerCase()}`}
      className="rounded border border-neutral-300 bg-white px-2 py-1 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
    >
      {label}
    </button>
  );
}

function TurnView({ turn }: { turn: Turn }): JSX.Element {
  const isOperator = turn.speaker === 'operator';
  const speakerName = isOperator ? 'you' : (turn.speaker as Persona).name;
  return (
    <li
      data-testid={`pm-panel-turn-${turn.id}`}
      data-speaker={isOperator ? 'operator' : 'persona'}
      className={cn(
        'rounded px-2 py-1.5',
        isOperator
          ? 'ml-8 bg-sky-50 dark:bg-sky-950/40'
          : 'mr-8 bg-neutral-50 dark:bg-neutral-900/60'
      )}
      title={`Cost: $${(turn.costUSD ?? 0).toFixed(3)}`}
    >
      <div className="mb-1 flex items-baseline gap-2">
        <span className="font-semibold uppercase tracking-wide text-[10px] text-neutral-500">
          {speakerName}
        </span>
        <span className="font-mono text-[10px] text-neutral-400">
          ${(turn.costUSD ?? 0).toFixed(3)}
        </span>
      </div>
      <p className="whitespace-pre-wrap text-xs text-neutral-800 dark:text-neutral-200">
        {turn.text}
      </p>
      {turn.suggested && turn.suggested.length > 0 ? (
        <SuggestedActionsView turnID={turn.id} actions={turn.suggested} />
      ) : null}
      {turn.executed && turn.executed.length > 0 ? (
        <ExecutedActionsView actions={turn.executed} />
      ) : null}
    </li>
  );
}

function SuggestedActionsView({
  turnID,
  actions,
}: {
  turnID: string;
  actions: NonNullable<Turn['suggested']>;
}): JSX.Element {
  const panel = usePmPanel();
  return (
    <div
      data-testid={`pm-panel-suggested-${turnID}`}
      className="mt-2 flex flex-wrap gap-1.5 border-t border-neutral-200 pt-2 dark:border-neutral-800"
    >
      <span className="self-center text-[10px] uppercase tracking-wide text-neutral-500">
        Suggested
      </span>
      {actions.map((a) => (
        <span
          key={a.id}
          data-testid={`pm-panel-suggested-${turnID}-${a.id}`}
          className="inline-flex items-center gap-1 rounded border border-amber-300 bg-amber-50 px-1.5 py-0.5 text-[11px] text-amber-800 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200"
          title={a.detail ?? a.label}
        >
          <span>{a.label}</span>
          <button
            type="button"
            data-testid={`pm-panel-suggested-${turnID}-${a.id}-apply`}
            onClick={() => panel.applySuggested(turnID, a)}
            className="rounded bg-amber-600 px-1 text-white hover:bg-amber-700"
          >
            apply
          </button>
          <button
            type="button"
            aria-label={`Dismiss ${a.label}`}
            data-testid={`pm-panel-suggested-${turnID}-${a.id}-dismiss`}
            onClick={() => panel.dismissSuggested(turnID, a.id)}
            className="rounded p-0.5 text-amber-700 hover:bg-amber-200 dark:text-amber-300 dark:hover:bg-amber-900"
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
    </div>
  );
}

function ExecutedActionsView({
  actions,
}: {
  actions: NonNullable<Turn['executed']>;
}): JSX.Element {
  return (
    <div
      data-testid="pm-panel-executed"
      className="mt-2 flex flex-wrap gap-1.5 border-t border-neutral-200 pt-2 dark:border-neutral-800"
    >
      <span className="self-center text-[10px] uppercase tracking-wide text-neutral-500">
        Applied
      </span>
      {actions.map((a) => (
        <span
          key={a.id}
          data-testid={`pm-panel-executed-${a.id}`}
          className="inline-flex items-center gap-1 rounded border border-emerald-300 bg-emerald-50 px-1.5 py-0.5 text-[11px] text-emerald-800 dark:border-emerald-700 dark:bg-emerald-950 dark:text-emerald-200"
        >
          <CheckCircle2 className="h-3 w-3" />
          <span>{a.label}</span>
        </span>
      ))}
    </div>
  );
}

function WalkTakeover(): JSX.Element {
  return (
    <div
      data-testid="pm-panel-walk"
      className="flex-1 overflow-y-auto bg-amber-50 px-4 py-6 text-sm dark:bg-amber-950/40"
    >
      <div className="font-semibold text-amber-900 dark:text-amber-200">
        Gemba walk in progress
      </div>
      <p className="mt-1 text-xs text-amber-800/80 dark:text-amber-300/80">
        The PM panel is the walk's chat surface while the walk is
        active. Exiting the walk restores your previous conversation.
      </p>
    </div>
  );
}

function PmPanelInput(): JSX.Element {
  const panel = usePmPanel();
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  // gm-96l: openAndFocus bumps focusSeq. We only claim focus on
  // the FIRST render when seq>0 (the panel was opened via
  // openAndFocus while the input was unmounted) and on every
  // subsequent bump. A first render with seq=0 (the open-by-
  // default path) does NOT steal focus from whatever surface
  // the operator was already typing into.
  const lastFocusedSeqRef = useRef<number>(0);
  useEffect(() => {
    if (panel.focusSeq === 0) return;
    if (panel.focusSeq === lastFocusedSeqRef.current) return;
    lastFocusedSeqRef.current = panel.focusSeq;
    if (!inputRef.current) return;
    inputRef.current.focus();
    const len = inputRef.current.value.length;
    inputRef.current.setSelectionRange(len, len);
  }, [panel.focusSeq]);

  return (
    <footer className="border-t border-neutral-200 px-3 py-2 dark:border-neutral-800">
      <textarea
        ref={inputRef}
        data-testid="pm-panel-input"
        rows={2}
        placeholder="Ask a Coach or consult a Manager…"
        value={panel.draft}
        onChange={(e) => panel.setDraft(e.target.value)}
        className="w-full resize-none rounded border border-neutral-300 bg-white px-2 py-1 text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
      />
    </footer>
  );
}
