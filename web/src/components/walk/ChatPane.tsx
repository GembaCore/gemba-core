// Chat pane (gm-uipx.2). Right side of /walk: framed active item
// at the top, PM framing + Ratify/Modify/Reject/Defer actions below.
//
// The "consulted personas" inset and the volunteered-perspectives
// sub-turns from ui-spec §5.4 are LLM-driven content; v1 ships the
// surface so operators can decide on agenda items without wiring
// the conversation backbone (which lives behind the PmPanel chat
// input — gm-uipx.12).

import { Check, MessageSquareDashed, Pause, Square, X } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { useHotkey } from '@/hotkeys';
import { cn } from '@/lib/utils';
import { useWalk } from './WalkContext';
import type { AgendaItem, Decision } from './types';

export function ChatPane(): JSX.Element {
  const walk = useWalk();
  const active = walk.agenda[walk.activeIndex];

  // Promote the first queued item if no item is active when the
  // walk page first paints. Quietly idempotent — promoteNext()
  // bails if anything is already active.
  useEffect(() => {
    walk.promoteNext();
  }, [walk]);

  return (
    <section
      data-testid="walk-chat-pane"
      className="flex h-full min-w-0 flex-1 flex-col bg-white dark:bg-neutral-950"
    >
      {active ? (
        <ActiveItemFrame item={active} index={walk.activeIndex} />
      ) : (
        <EmptyState />
      )}
      <DecisionToolbar active={active} />
    </section>
  );
}

function ActiveItemFrame({ item, index }: { item: AgendaItem; index: number }): JSX.Element {
  return (
    <header
      data-testid="walk-chat-active-frame"
      className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800"
    >
      <div className="text-[11px] uppercase tracking-wide text-neutral-500">
        Agenda #{index + 1} · {item.source.replace('_', ' ')}
      </div>
      <h2 className="mt-1 text-base font-semibold">{item.title}</h2>
      <p className="mt-2 text-sm text-neutral-600 dark:text-neutral-400">
        {/* The PM's framing arrives via the chat backbone in a
            future bead. v1 ships a placeholder so the surface is
            non-empty without prescribing the eventual content. */}
        Discuss this item in the PM panel below. Decide with R / M / X / D
        when you're ready to move on.
      </p>
    </header>
  );
}

function EmptyState(): JSX.Element {
  return (
    <div
      data-testid="walk-chat-empty"
      className="flex flex-1 items-center justify-center text-sm text-neutral-500"
    >
      <div className="text-center">
        <MessageSquareDashed className="mx-auto mb-2 h-6 w-6 text-neutral-400" aria-hidden />
        <div>No agenda item is active. Click an item in the agenda to start discussing it.</div>
      </div>
    </div>
  );
}

function DecisionToolbar({ active }: { active: AgendaItem | undefined }): JSX.Element {
  const walk = useWalk();
  const lastFocus = useRef<Decision | null>(null);
  // R / M / X / D hotkeys per ui-spec §6.6. Scoped so they only
  // fire while the walk surface is mounted; the global App-level
  // bindings stay quiet on Board / Grid / Sessions.
  useHotkey('walk-ratify', () => {
    lastFocus.current = 'ratify';
    if (active) walk.decide(active.id, 'ratify');
  });
  useHotkey('walk-modify', () => {
    lastFocus.current = 'modify';
    if (active) walk.decide(active.id, 'modify');
  });
  useHotkey('walk-reject', () => {
    lastFocus.current = 'reject';
    if (active) walk.decide(active.id, 'reject');
  });
  useHotkey('walk-defer', () => {
    lastFocus.current = 'defer';
    if (active) walk.defer(active.id);
  });

  const disabled = !active;
  return (
    <footer
      data-testid="walk-chat-toolbar"
      className="flex items-center gap-2 border-t border-neutral-200 px-3 py-2 dark:border-neutral-800"
    >
      <DecisionButton
        decision="ratify"
        disabled={disabled}
        Icon={Check}
        label="Ratify"
        kbd="R"
        onClick={() => active && walk.decide(active.id, 'ratify')}
      />
      <DecisionButton
        decision="modify"
        disabled={disabled}
        Icon={Pause}
        label="Modify"
        kbd="M"
        onClick={() => active && walk.decide(active.id, 'modify')}
      />
      <DecisionButton
        decision="reject"
        disabled={disabled}
        Icon={X}
        label="Reject"
        kbd="X"
        onClick={() => active && walk.decide(active.id, 'reject')}
      />
      <DecisionButton
        decision="defer"
        disabled={disabled}
        Icon={Square}
        label="Defer"
        kbd="D"
        onClick={() => active && walk.defer(active.id)}
      />
    </footer>
  );
}

function DecisionButton({
  decision,
  Icon,
  label,
  kbd,
  disabled,
  onClick,
}: {
  decision: Decision;
  Icon: typeof Check;
  label: string;
  kbd: string;
  disabled: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      disabled={disabled}
      data-testid={`walk-decision-${decision}`}
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded border px-2 py-1 text-xs',
        'border-neutral-300 bg-white text-neutral-700 hover:bg-neutral-100 disabled:opacity-50',
        'dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800'
      )}
    >
      <Icon className="h-3.5 w-3.5" aria-hidden />
      <span>{label}</span>
      <kbd className="ml-1 rounded bg-neutral-100 px-1 py-0 font-mono text-[10px] text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400">
        {kbd}
      </kbd>
    </button>
  );
}
