// ConversationPane (gm-root.17.3 — see docs/design/newproject.md).
//
// Left pane of the /onboard route. Shows the message transcript with the
// newproject skill and the operator's input box. Submitting a turn
// posts to /api/v1/newproject/:id/turn; the host owns the network
// call so this component is presentational + one onSend callback.

import { useEffect, useRef, useState } from 'react';
import type { ConversationMessage } from '@/api/newproject';

export interface ConversationPaneProps {
  transcript: ConversationMessage[];
  onSend: (message: string) => void;
  // Disabled while a turn is in flight or the session is committing.
  disabled: boolean;
  // Cleared by the host after every successful turn so the input
  // resets without the host owning the controlled value.
  resetToken: number;
}

export function ConversationPane({
  transcript,
  onSend,
  disabled,
  resetToken,
}: ConversationPaneProps): JSX.Element {
  const [draft, setDraft] = useState('');
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Auto-scroll the transcript to the bottom when a new message
  // lands. Refresh / disconnect resets the whole route, so we don't
  // need to be clever about scroll restoration.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [transcript.length]);

  // Reset the input after every successful turn. The host bumps
  // resetToken so this effect fires.
  useEffect(() => {
    setDraft('');
  }, [resetToken]);

  const submit = () => {
    const trimmed = draft.trim();
    if (!trimmed || disabled) return;
    onSend(trimmed);
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter submits, Shift+Enter inserts a newline. Matches the
    // codebase's other multiline inputs (PmPanel, Coach).
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  return (
    <section
      data-testid="newproject-conversation-pane"
      className="flex min-h-0 min-w-0 flex-1 flex-col border-r border-neutral-200 dark:border-neutral-800"
    >
      <header className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
        <h2 className="text-sm font-semibold">Conversation</h2>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Describe the project. The Onboarder proposes milestones, epics, and beads as you talk.
        </p>
      </header>

      <div
        ref={scrollRef}
        data-testid="newproject-transcript"
        className="flex-1 overflow-y-auto px-4 py-3"
      >
        {transcript.length === 0 ? (
          <p
            data-testid="newproject-transcript-empty"
            className="text-xs italic text-neutral-500 dark:text-neutral-400"
          >
            Say hello to start the conversation.
          </p>
        ) : (
          <ol className="space-y-3">
            {transcript.map((m) => (
              <li
                key={m.id}
                data-testid={`newproject-message-${m.id}`}
                data-role={m.role}
                className={
                  m.role === 'user'
                    ? 'rounded-md bg-sky-50 px-3 py-2 text-xs dark:bg-sky-950/40'
                    : 'rounded-md bg-neutral-100 px-3 py-2 text-xs dark:bg-neutral-900'
                }
              >
                <div className="mb-0.5 text-[10px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
                  {m.role === 'user' ? 'You' : 'Onboarder'}
                </div>
                <div className="whitespace-pre-wrap">{m.content}</div>
              </li>
            ))}
          </ol>
        )}
      </div>

      <footer className="border-t border-neutral-200 px-4 py-3 dark:border-neutral-800">
        <textarea
          data-testid="newproject-input"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={disabled}
          rows={3}
          placeholder={
            disabled
              ? 'Waiting for the Onboarder…'
              : "Describe the project, or revise an item ('change milestone 2 to OSS-ready')…"
          }
          className="w-full resize-none rounded-md border border-neutral-300 bg-white px-3 py-2 text-xs disabled:cursor-not-allowed disabled:opacity-60 dark:border-neutral-700 dark:bg-neutral-950"
        />
        <div className="mt-2 flex items-center justify-between">
          <p className="text-[10px] text-neutral-500 dark:text-neutral-400">
            Enter to send · Shift+Enter for newline · refresh discards the session.
          </p>
          <button
            type="button"
            data-testid="newproject-send"
            onClick={submit}
            disabled={disabled || draft.trim() === ''}
            className="rounded bg-sky-600 px-3 py-1 text-xs font-semibold text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            Send
          </button>
        </div>
      </footer>
    </section>
  );
}
