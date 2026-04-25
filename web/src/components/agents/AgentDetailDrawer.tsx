// AgentDetailDrawer (gm-e12.4). Right-side drawer that pops from a
// tile click on AgentsPage. Renders the full AgentRef metadata (id,
// kind, role, dialect, workspace, parent_id) and — when a session is
// active — its lifecycle fields with absolute timestamps the tile
// abbreviates.
//
// Provider-aware drill-in (per-Workspace.kind switch) is gm-e12.15;
// this drawer stays kind-agnostic so the upstream bead can extend it
// without rewriting the surface.

import { useEffect, useLayoutEffect, useRef } from 'react';
import { Bot, User, X } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { AgentRef } from '@/types/core.gen';
import type { Session } from '@/api/sessions';
import { useSessionPeek } from '@/hooks/useSessions';
import { useCapabilities } from '@/capabilities/context-internal';
import { ApiError } from '@/api/client';
import { cn } from '@/lib/utils';

export interface AgentDetailDrawerProps {
  agent: AgentRef | null;
  session: Session | null;
  onClose: () => void;
}

export function AgentDetailDrawer({ agent, session, onClose }: AgentDetailDrawerProps) {
  // Esc closes the drawer. Listening on document so the handler still
  // fires when focus is somewhere weird (e.g. an embedded textarea
  // doesn't have keyup bubbling to a stable parent).
  useEffect(() => {
    if (!agent) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [agent, onClose]);

  if (!agent) return null;

  const Icon: LucideIcon = agent.agent_kind === 'human' ? User : Bot;
  const accent =
    agent.agent_kind === 'human'
      ? 'text-amber-600 dark:text-amber-400'
      : 'text-sky-600 dark:text-sky-400';

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Agent ${agent.id}`}
      className="fixed inset-0 z-40 flex justify-end bg-black/40"
      onClick={onClose}
      data-testid="agent-drawer"
    >
      <div
        className="flex h-full w-full max-w-md flex-col overflow-y-auto bg-white shadow-xl dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
        data-testid={`agent-drawer-body-${agent.id}`}
      >
        <header className="flex items-start justify-between border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
          <div className="flex items-start gap-2 min-w-0">
            <Icon className={cn('mt-0.5 h-5 w-5 shrink-0', accent)} aria-hidden />
            <div className="min-w-0">
              <h2 className="truncate text-lg font-semibold tracking-tight">
                {agent.name || agent.id}
              </h2>
              <p className="truncate font-mono text-xs text-neutral-500 dark:text-neutral-400">
                {agent.id}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1 text-neutral-500 hover:bg-neutral-100 dark:hover:bg-neutral-800"
            aria-label="Close agent drawer"
            data-testid="agent-drawer-close"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <section className="space-y-3 px-6 py-4 text-sm">
          <DetailRow label="Kind">
            <span className="capitalize">{agent.agent_kind}</span>
          </DetailRow>
          {agent.role ? <DetailRow label="Role">{agent.role}</DetailRow> : null}
          {agent.dialect ? (
            <DetailRow label="Dialect">
              <span className="font-mono text-xs">{agent.dialect}</span>
            </DetailRow>
          ) : null}
          {agent.workspace ? (
            <DetailRow label="Workspace">
              <span className="font-mono text-xs">{agent.workspace}</span>
            </DetailRow>
          ) : null}
          {agent.parent_id ? (
            <DetailRow label="Parent">
              <span className="font-mono text-xs">{agent.parent_id}</span>
            </DetailRow>
          ) : null}
        </section>

        {session ? (
          <section
            className="border-t border-neutral-200 px-6 py-4 dark:border-neutral-800"
            data-testid="agent-drawer-session"
          >
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
              Active session
            </h3>
            <div className="space-y-2 text-sm">
              <DetailRow label="Status">{session.status}</DetailRow>
              <DetailRow label="Session id">
                <span className="font-mono text-xs">{session.id}</span>
              </DetailRow>
              <DetailRow label="Assignment">
                <span className="font-mono text-xs">{session.assignment_id}</span>
              </DetailRow>
              <DetailRow label="Started">
                <span className="font-mono text-xs">{session.started_at}</span>
              </DetailRow>
              {session.last_heartbeat ? (
                <DetailRow label="Heartbeat">
                  <span className="font-mono text-xs">{session.last_heartbeat}</span>
                </DetailRow>
              ) : null}
            </div>
            <TranscriptPane sessionID={session.id} />
          </section>
        ) : (
          <section
            className="border-t border-neutral-200 px-6 py-4 text-sm text-neutral-500 dark:border-neutral-800"
            data-testid="agent-drawer-idle"
          >
            No active session.
          </section>
        )}
      </div>
    </div>
  );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <span className="text-xs uppercase tracking-wide text-neutral-500">{label}</span>
      <span className="min-w-0 truncate text-right">{children}</span>
    </div>
  );
}

// TranscriptPane is the live-peek surface (gm-5uqu). Pulls the last
// captured transcript via /api/sessions/{id}/peek and re-fetches on
// every ['sessions'] cache invalidation (the SSE pipeline fires on
// session.transition / session.state_reported), so the operator sees
// what their agent is currently saying without alt-tabbing to the
// terminal.
//
// Capability gate: hides itself entirely when the bound
// OrchestrationPlane manifest doesn't declare the 'transcript' peek
// mode — for an adaptor that can't peek, "transcript unavailable" is
// noise, not signal.
//
// Error gate: 404 / 503 / 501 / Unsupported render a single steady-
// state line ("transcript unavailable") rather than the API's typed
// envelope. The drawer is supervisory, not diagnostic.
function TranscriptPane({ sessionID }: { sessionID: string }) {
  const { orchestrationPlane } = useCapabilities();
  const supportsTranscript =
    !!orchestrationPlane?.peek_modes?.includes('transcript');

  const { data, error, isLoading } = useSessionPeek(
    supportsTranscript ? sessionID : null
  );

  // Auto-scroll to the bottom whenever new transcript content arrives —
  // a tail like the operator's terminal would show.
  const scrollerRef = useRef<HTMLPreElement | null>(null);
  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [data?.transcript]);

  if (!supportsTranscript) return null;

  return (
    <div
      className="mt-3 rounded-md border border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950"
      data-testid="agent-drawer-transcript"
    >
      <header className="flex items-baseline justify-between border-b border-neutral-200 px-3 py-1.5 text-xs dark:border-neutral-800">
        <span className="font-semibold uppercase tracking-wide text-neutral-500">
          Transcript
        </span>
        {data?.captured_at ? (
          <span
            className="font-mono text-[10px] text-neutral-400"
            data-testid="agent-drawer-transcript-captured-at"
          >
            captured {data.captured_at}
          </span>
        ) : null}
      </header>
      <pre
        ref={scrollerRef}
        className="m-0 max-h-64 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-xs leading-relaxed text-neutral-800 dark:text-neutral-200"
      >
        {transcriptBody({ isLoading, error, transcript: data?.transcript })}
      </pre>
    </div>
  );
}

function transcriptBody({
  isLoading,
  error,
  transcript,
}: {
  isLoading: boolean;
  error: ApiError | null | undefined;
  transcript: string | undefined;
}): string {
  if (error) {
    // Treat every error as steady-state "no transcript" — the drawer
    // is supervisory, not a debugger. ApiError covers the 404 / 503 /
    // unsupported family; anything else falls through to the same
    // string.
    return 'Transcript unavailable.';
  }
  if (isLoading) return 'Loading…';
  const text = transcript?.trimEnd();
  if (!text) return 'No transcript content yet.';
  return text;
}
