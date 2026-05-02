import { Bot, CheckCircle2, CircleDot, FileText, GitBranch, MessagesSquare, Play } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { InteractionSession } from '@/interactions/types';

const STATUS_LABELS: Record<InteractionSession['status'], string> = {
  drafting: 'Drafting',
  waiting_on_operator: 'Waiting',
  running: 'Running',
  applying: 'Applying',
  done: 'Done',
  canceled: 'Canceled',
  failed: 'Failed',
};

export function InteractionPanel({
  session,
  onAction,
}: {
  session: InteractionSession;
  onAction?: (actionId: string) => void;
}) {
  return (
    <div className="flex min-h-full flex-col" data-testid="interaction-panel">
      <header className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
        <div className="mb-1 flex flex-wrap items-center gap-1 text-[11px] text-neutral-500">
          {(session.scope.breadcrumb ?? []).map((crumb, idx) => (
            <span key={`${crumb.type}:${crumb.id}`} className="inline-flex items-center gap-1">
              {idx > 0 ? <span aria-hidden>›</span> : null}
              <span>{crumb.label}</span>
            </span>
          ))}
        </div>
        <div className="flex min-w-0 items-start gap-2">
          <MessagesSquare className="mt-0.5 h-4 w-4 shrink-0 text-sky-600 dark:text-sky-300" />
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-sm font-semibold text-neutral-900 dark:text-neutral-100">
              {session.scope.title ?? session.scope.id}
            </h2>
            <div className="mt-1 flex flex-wrap gap-1.5">
              <Pill icon={<CircleDot className="h-3 w-3" />} label={STATUS_LABELS[session.status]} />
              <Pill icon={<Bot className="h-3 w-3" />} label={session.runtimeLabel} />
              <Pill icon={<GitBranch className="h-3 w-3" />} label={session.scope.type} />
            </div>
          </div>
        </div>
      </header>

      <div className="flex-1 space-y-5 overflow-y-auto px-4 py-4">
        <Section title="Conversation">
          <div className="space-y-3" data-testid="interaction-transcript">
            {session.messages.map((message) => (
              <article
                key={message.id}
                className={cn(
                  'rounded-md border px-3 py-2',
                  message.role === 'operator'
                    ? 'border-sky-200 bg-sky-50 text-sky-950 dark:border-sky-900/50 dark:bg-sky-950/30 dark:text-sky-100'
                    : 'border-neutral-200 bg-white text-neutral-800 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-200'
                )}
              >
                <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
                  {message.role}
                </div>
                <p className="whitespace-pre-wrap text-sm leading-5">{message.body}</p>
              </article>
            ))}
          </div>
        </Section>

        {session.draft ? (
          <Section title={session.draft.title}>
            <div
              className="rounded-md border border-neutral-200 bg-neutral-50 p-3 dark:border-neutral-800 dark:bg-neutral-900/60"
              data-testid="interaction-draft"
            >
              <p className="text-sm text-neutral-800 dark:text-neutral-200">
                {session.draft.summary}
              </p>
              {session.draft.bullets && session.draft.bullets.length > 0 ? (
                <ul className="mt-2 list-disc space-y-1 pl-4 text-xs text-neutral-600 dark:text-neutral-400">
                  {session.draft.bullets.map((bullet) => (
                    <li key={bullet}>{bullet}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          </Section>
        ) : null}

        <Section title="Suggested Actions">
          <div className="space-y-2" data-testid="interaction-actions">
            {session.suggestedActions.map((action) => (
              <button
                key={action.id}
                type="button"
                disabled={!!action.disabledReason}
                title={action.disabledReason ?? action.description}
                onClick={() => onAction?.(action.id)}
                className={cn(
                  'flex w-full items-start gap-2 rounded-md border px-3 py-2 text-left',
                  action.disabledReason
                    ? 'cursor-not-allowed border-neutral-200 bg-neutral-50 text-neutral-400 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-600'
                    : 'border-neutral-300 bg-white text-neutral-800 hover:bg-neutral-50 dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-200 dark:hover:bg-neutral-900'
                )}
              >
                <Play className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                <span className="min-w-0">
                  <span className="block text-xs font-semibold">{action.label}</span>
                  <span className="block text-xs text-neutral-500">{action.description}</span>
                </span>
              </button>
            ))}
          </div>
        </Section>

        {session.evidence && session.evidence.length > 0 ? (
          <Section title="Evidence">
            <ul className="space-y-1 text-xs" data-testid="interaction-evidence">
              {session.evidence.map((item) => (
                <li key={item.id} className="flex items-center gap-2 text-neutral-600 dark:text-neutral-400">
                  <FileText className="h-3 w-3" />
                  {item.href ? <a href={item.href}>{item.label}</a> : <span>{item.label}</span>}
                </li>
              ))}
            </ul>
          </Section>
        ) : null}

        {session.decisionLog && session.decisionLog.length > 0 ? (
          <Section title="Decisions">
            <ul className="space-y-2 text-xs" data-testid="interaction-decisions">
              {session.decisionLog.map((decision) => (
                <li
                  key={decision.id}
                  className="rounded-md border border-neutral-200 bg-white p-2 dark:border-neutral-800 dark:bg-neutral-950"
                >
                  <div className="font-medium text-neutral-800 dark:text-neutral-200">
                    {decision.summary}
                  </div>
                  <div className="mt-0.5 font-mono text-[11px] text-neutral-500">
                    {decision.outcome}
                  </div>
                  {decision.rationale ? (
                    <div className="mt-1 text-neutral-500">{decision.rationale}</div>
                  ) : null}
                </li>
              ))}
            </ul>
          </Section>
        ) : null}

        <Section title="Capabilities">
          <div className="flex flex-wrap gap-1.5" data-testid="interaction-capabilities">
            {session.capabilities.map((capability) => (
              <span
                key={capability}
                className="inline-flex items-center gap-1 rounded bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200"
              >
                <CheckCircle2 className="h-3 w-3" />
                {capability}
              </span>
            ))}
          </div>
        </Section>
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        {title}
      </h3>
      {children}
    </section>
  );
}

function Pill({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded bg-neutral-100 px-2 py-0.5 text-[11px] text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
      {icon}
      {label}
    </span>
  );
}
