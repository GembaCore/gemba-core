// Chat pane (gm-i65). Right side of /walk: framed active item at the
// top, transcript in the middle, R/M/X/D decision toolbar + user-turn
// input at the bottom.
//
// SuggestedAction buttons (gm-4p6 / gm-uf7) are rendered when the
// persona's turn payload includes them; absent for v1.

import { Check, MessageSquareDashed, Pause, SendHorizontal, Square, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useHotkey } from '@/hotkeys';
import { cn } from '@/lib/utils';
import { useWalk } from './WalkContext';
import type { AgendaItem, Decision, WalkTurn } from './types';

export function ChatPane(): JSX.Element {
  const walk = useWalk();
  const active = walk.agenda[walk.activeIndex];

  // Promote the first queued item if no item is active when the
  // walk page first paints. Idempotent — promoteNext bails if
  // anything is already active.
  useEffect(() => {
    if (walk.walk && !walk.walk.agenda.some((i) => i.status === 'active')) {
      void walk.promoteNext();
    }
  }, [walk.walk?.id]); // eslint-disable-line react-hooks/exhaustive-deps

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
      <Transcript turns={walk.walk?.transcript ?? []} activeID={active?.id} />
      <TurnComposer agendaItemID={active?.id} disabled={!walk.active || !active} />
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
        Agenda #{index + 1} · {item.source.kind.replace('_', ' ')}
      </div>
      <h2 className="mt-1 text-base font-semibold">{item.topic}</h2>
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

function Transcript({
  turns,
  activeID,
}: {
  turns: WalkTurn[];
  activeID: string | undefined;
}): JSX.Element {
  // Filter to turns for the active item plus meta turns (no
  // agenda_item_id). Persona response generation belongs to gm-4p6;
  // until then the operator's user turns are what shows here.
  const visible = turns.filter(
    (t) => !t.agenda_item_id || (activeID && t.agenda_item_id === activeID)
  );
  if (visible.length === 0) {
    return (
      <div
        data-testid="walk-chat-transcript-empty"
        className="flex-1 overflow-y-auto px-4 py-3 text-xs italic text-neutral-400"
      >
        No discussion yet. Type below to add a turn — the PM persona will respond once gm-4p6 is wired.
      </div>
    );
  }
  return (
    <ol
      data-testid="walk-chat-transcript"
      className="flex-1 space-y-2 overflow-y-auto px-4 py-3 text-sm"
    >
      {visible.map((turn) => (
        <li key={turn.id} data-testid={`walk-chat-turn-${turn.id}`}>
          <div className="text-[10px] uppercase tracking-wide text-neutral-500">
            {speakerLabel(turn)}
          </div>
          <div className="whitespace-pre-wrap rounded bg-neutral-50 px-2 py-1 text-neutral-800 dark:bg-neutral-900 dark:text-neutral-200">
            {turn.content}
          </div>
        </li>
      ))}
    </ol>
  );
}

function speakerLabel(turn: WalkTurn): string {
  if (turn.speaker === 'persona') return turn.persona_id ?? 'persona';
  if (turn.speaker === 'system') return 'system';
  return turn.user_id ?? 'operator';
}

function TurnComposer({
  agendaItemID,
  disabled,
}: {
  agendaItemID: string | undefined;
  disabled: boolean;
}): JSX.Element {
  const walk = useWalk();
  const [draft, setDraft] = useState('');
  const [pending, setPending] = useState(false);

  const submit = async () => {
    const content = draft.trim();
    if (!content || disabled) return;
    setPending(true);
    try {
      await walk.sendTurn(content, agendaItemID);
      setDraft('');
    } finally {
      setPending(false);
    }
  };

  return (
    <form
      data-testid="walk-chat-composer"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      className="flex items-center gap-2 border-t border-neutral-200 px-3 py-2 dark:border-neutral-800"
    >
      <input
        data-testid="walk-chat-composer-input"
        type="text"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        placeholder={
          disabled
            ? 'Pick an active agenda item to start the conversation'
            : 'Reply to the PM…'
        }
        disabled={disabled || pending}
        className="flex-1 rounded border border-neutral-300 bg-white px-2 py-1 text-sm placeholder:text-neutral-400 disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
      />
      <button
        type="submit"
        data-testid="walk-chat-composer-submit"
        disabled={disabled || pending || draft.trim().length === 0}
        className="inline-flex items-center gap-1 rounded border border-neutral-300 bg-white px-2 py-1 text-xs hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
      >
        <SendHorizontal className="h-3.5 w-3.5" aria-hidden />
        <span>Send</span>
      </button>
    </form>
  );
}

function DecisionToolbar({ active }: { active: AgendaItem | undefined }): JSX.Element {
  const walk = useWalk();
  const lastFocus = useRef<Decision | null>(null);
  // R / M / X / D hotkeys per ui-spec §6.6 — scoped to /walk.
  useHotkey('walk-ratify', () => {
    lastFocus.current = 'ratify';
    if (active) void walk.decide(active.id, 'ratify');
  });
  useHotkey('walk-modify', () => {
    lastFocus.current = 'modify';
    if (active) void walk.decide(active.id, 'modify');
  });
  useHotkey('walk-reject', () => {
    lastFocus.current = 'reject';
    if (active) void walk.decide(active.id, 'reject');
  });
  useHotkey('walk-defer', () => {
    lastFocus.current = 'defer';
    if (active) void walk.defer(active.id);
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
        onClick={() => active && void walk.decide(active.id, 'ratify')}
      />
      <DecisionButton
        decision="modify"
        disabled={disabled}
        Icon={Pause}
        label="Modify"
        kbd="M"
        onClick={() => active && void walk.decide(active.id, 'modify')}
      />
      <DecisionButton
        decision="reject"
        disabled={disabled}
        Icon={X}
        label="Reject"
        kbd="X"
        onClick={() => active && void walk.decide(active.id, 'reject')}
      />
      <DecisionButton
        decision="defer"
        disabled={disabled}
        Icon={Square}
        label="Defer"
        kbd="D"
        onClick={() => active && void walk.defer(active.id)}
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
