import { useEffect, useRef } from 'react';
import { cn } from '@/lib/utils';
import type { InteractionMessage } from '@/interactions/types';

const ROLE_LABELS: Record<InteractionMessage['role'], string> = {
  operator: 'You',
  assistant: 'Assistant',
  system: 'System',
  tool: 'Tool',
};

export interface TranscriptPaneProps {
  messages: InteractionMessage[];
  emptyLabel: string;
  assistantLabel?: string;
  testid?: string;
  messageTestId?: (message: InteractionMessage) => string;
  className?: string;
}

export function TranscriptPane({
  messages,
  emptyLabel,
  assistantLabel = 'Assistant',
  testid = 'interaction-transcript',
  messageTestId,
  className,
}: TranscriptPaneProps): JSX.Element {
  const scrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [messages.length]);

  return (
    <div ref={scrollRef} data-testid={testid} className={cn('overflow-y-auto', className)}>
      {messages.length === 0 ? (
        <p className="text-xs italic text-neutral-500 dark:text-neutral-400">{emptyLabel}</p>
      ) : (
        <ol className="space-y-3">
          {messages.map((message) => {
            const label =
              message.role === 'assistant' ? assistantLabel : ROLE_LABELS[message.role];
            return (
              <li
                key={message.id}
                data-testid={messageTestId?.(message)}
                data-role={message.role}
                className={cn(
                  'rounded-md border px-3 py-2 text-xs',
                  message.role === 'operator'
                    ? 'border-sky-200 bg-sky-50 text-sky-950 dark:border-sky-900/50 dark:bg-sky-950/30 dark:text-sky-100'
                    : 'border-neutral-200 bg-white text-neutral-800 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-200',
                  message.role === 'system' &&
                    'border-neutral-200 bg-neutral-50 text-neutral-600 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-400'
                )}
              >
                <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
                  {label}
                </div>
                <div className="whitespace-pre-wrap leading-5">{message.body}</div>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
