// PM panel (gm-uipx.12) — bottom drawer per ui-spec §2.4.
//
// Collapsed: nothing renders in the layout flow; the trigger is the
// Topbar PM-toggle button. Expanded: a fixed-bottom drawer that
// overlays content (operator can still scroll the page underneath
// because the panel has its own height and z-index, not a layout
// shift).
//
// State lives in PmPanelContext; this file is the pure render layer.

import { useEffect, useRef } from 'react';
import { CheckCircle2, MessageSquare, X } from 'lucide-react';
import { useHotkey } from '@/hotkeys';
import { cn } from '@/lib/utils';
import { totalCost, usePmPanel } from './PmPanelContext';
import type { Turn } from './types';

// Drag handle dimensions. The handle is the top edge of the drawer;
// drag-up grows the panel (within 25%-75% of viewport).
const DRAG_HANDLE_PX = 4;

export function PmPanel(): JSX.Element | null {
  const panel = usePmPanel();

  // Cmd-P / Ctrl-P toggle. Browsers reserve Cmd-P for print so this
  // is best-effort like Cmd-Shift-W and Cmd-Shift-L; the in-page
  // toggle button (Topbar) is the authoritative path.
  useHotkey('pm-panel-toggle', () => {
    panel.toggle();
  });
  // Escape collapses without losing conversation per ui-spec §2.4.
  // Rides on the shared 'drawer-close' id; gate on panel.open so we
  // don't compete with other Esc consumers (HelpOverlay, drawers).
  useHotkey('drawer-close', () => {
    if (panel.open) panel.setOpen(false);
  });

  if (!panel.open) return null;

  return (
    <section
      role="complementary"
      aria-label="Project Manager panel"
      data-testid="pm-panel"
      style={{ height: panel.heightPx }}
      className={cn(
        'fixed inset-x-0 bottom-0 z-30 flex flex-col border-t border-neutral-200 bg-white shadow-2xl',
        'dark:border-neutral-800 dark:bg-neutral-950'
      )}
    >
      <DragHandle />
      <PmPanelHeader />
      {panel.walkActive ? <WalkTakeover /> : <ConversationView />}
      <PmPanelInput />
    </section>
  );
}

function DragHandle(): JSX.Element {
  const panel = usePmPanel();
  const startY = useRef<number | null>(null);
  const startH = useRef<number>(0);

  useEffect(() => {
    if (startY.current == null) return;
    const onMove = (e: PointerEvent) => {
      if (startY.current == null) return;
      const dy = startY.current - e.clientY; // up = positive
      panel.setHeightPx(startH.current + dy);
    };
    const onUp = () => {
      startY.current = null;
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    return () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };
  }, [panel]);

  return (
    <div
      role="separator"
      aria-orientation="horizontal"
      aria-label="Resize PM panel"
      data-testid="pm-panel-drag"
      style={{ height: DRAG_HANDLE_PX }}
      className="cursor-ns-resize border-b border-transparent bg-neutral-100 hover:bg-sky-300 dark:bg-neutral-800 dark:hover:bg-sky-700"
      onPointerDown={(e) => {
        e.preventDefault();
        startY.current = e.clientY;
        startH.current = panel.heightPx;
      }}
    />
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
  // Per-turn cost on hover surfaces in TurnView via title attribute.
  // Header just shows the running session total.
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
        {/* Quick-action buttons would be context-sensitive (ui-spec
            §2.4); v1 ships a static placeholder so the surface is
            non-empty without prescribing the eventual content. */}
        <div className="flex flex-wrap gap-2 text-xs">
          <QuickAction label="Plan next sprint" />
          <QuickAction label="Audit risk register" />
          <QuickAction label="What changed today?" />
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

function QuickAction({ label }: { label: string }): JSX.Element {
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
  const speakerName = isOperator ? 'you' : (turn.speaker as { name: string }).name;
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
  // ui-spec §5.4 — Gemba walk surface ships in gm-uipx.2. Until
  // then this is a placeholder that proves the takeover wires
  // through (the e2e spec asserts the data-testid is present
  // when walkActive=true, regardless of content).
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
  return (
    <footer className="border-t border-neutral-200 px-3 py-2 dark:border-neutral-800">
      <textarea
        data-testid="pm-panel-input"
        rows={2}
        placeholder="Ask a Coach or consult a Manager…"
        className="w-full resize-none rounded border border-neutral-300 bg-white px-2 py-1 text-xs dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100"
      />
    </footer>
  );
}
