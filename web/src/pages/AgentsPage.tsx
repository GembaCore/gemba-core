// AgentsPage (gm-e12.4). Live tile-per-agent view backed by
// /api/agents and /api/sessions. Each tile shows:
//
//   * agent_kind icon (agent vs human — distinct visual treatment per
//     the bead's DoD)
//   * id / name / role
//   * current observable session (status badge + bead id)
//   * recent activity (last_heartbeat or session start)
//
// Liveness: tiles update within 500ms of an agent state change. We
// don't have agent.* events on the SSE stream yet, so "agent state
// change" today means a session transition that touches the agent's
// active session. Those flow through session.transition /
// session.state_reported on /events, which the SSE consumer
// (gm-e12.2) maps to invalidations on ['sessions']. The tile derives
// the active session from sessions, so the invalidation cycle —
// SSE event → cache invalidate → refetch → re-render — completes
// inside the budget on every machine we've measured.

import { useMemo, useState } from 'react';
import { Bot, User } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useAgents } from '@/hooks/useAgents';
import { useSessions } from '@/hooks/useSessions';
import { AgentDetailDrawer } from '@/components/agents/AgentDetailDrawer';
import type { AgentRef, SessionStatus } from '@/types/core.gen';
import type { Session } from '@/api/sessions';
import { cn } from '@/lib/utils';

// SESSION_STATUS_TONE maps the observable lifecycle to a tailwind chip
// tone. Mirrors the language operators use ("ready / working /
// prompting / stalled" — each gets its own color so a quick scan of
// the dashboard catches stalls without reading the label).
const SESSION_STATUS_TONE: Record<SessionStatus, string> = {
  initializing: 'bg-neutral-200 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300',
  ready: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
  working: 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300',
  prompting: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
  stalled: 'bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300',
  suspended: 'bg-neutral-200 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400',
  completed: 'bg-neutral-100 text-neutral-500 dark:bg-neutral-900 dark:text-neutral-500',
  failed: 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300',
};

// OBSERVABLE_STATUSES is the subset of SessionStatus we treat as
// "this agent is actively running something". Terminal statuses
// (completed / failed / canceled) and suspended fall outside; if an
// agent's only sessions are terminal, the tile shows "idle".
const OBSERVABLE_STATUSES: ReadonlySet<SessionStatus> = new Set<SessionStatus>([
  'initializing',
  'ready',
  'working',
  'prompting',
  'stalled',
]);

export function AgentsPage() {
  const { data: agents = [], isLoading, error } = useAgents();
  const { data: sessions = [] } = useSessions();
  const [openAgentId, setOpenAgentId] = useState<string | null>(null);

  // sessionsByAgent maps agent_id → its most recent observable session.
  // We pick the freshest started_at among observable sessions per agent;
  // a real adaptor rarely runs an agent on more than one pane at a time
  // but the data model permits it, so collapsing to the freshest keeps
  // the tile deterministic.
  const sessionsByAgent = useMemo(() => {
    const out = new Map<string, Session>();
    for (const s of sessions) {
      if (!OBSERVABLE_STATUSES.has(s.status)) continue;
      const existing = out.get(s.agent_id);
      if (!existing || (s.started_at ?? '') > (existing.started_at ?? '')) {
        out.set(s.agent_id, s);
      }
    }
    return out;
  }, [sessions]);

  // mostRecentByAgent is the freshest session of any status (including
  // terminal) — used as a fallback "recent activity" timestamp for an
  // idle agent. Without this, idle tiles would have nothing to show
  // beyond "no current session".
  const mostRecentByAgent = useMemo(() => {
    const out = new Map<string, Session>();
    for (const s of sessions) {
      const existing = out.get(s.agent_id);
      const sTime = s.last_heartbeat ?? s.ended_at ?? s.started_at ?? '';
      const eTime = existing
        ? existing.last_heartbeat ?? existing.ended_at ?? existing.started_at ?? ''
        : '';
      if (!existing || sTime > eTime) {
        out.set(s.agent_id, s);
      }
    }
    return out;
  }, [sessions]);

  const sortedAgents = useMemo(() => {
    return [...agents].sort((a, b) => {
      // Active first (sessionsByAgent has them), then by name.
      const aActive = sessionsByAgent.has(a.id) ? 0 : 1;
      const bActive = sessionsByAgent.has(b.id) ? 0 : 1;
      if (aActive !== bActive) return aActive - bActive;
      return a.name.localeCompare(b.name);
    });
  }, [agents, sessionsByAgent]);

  const openAgent = openAgentId
    ? agents.find((a) => a.id === openAgentId) ?? null
    : null;
  const openSession = openAgentId ? sessionsByAgent.get(openAgentId) ?? null : null;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold tracking-tight">Agents</h1>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Roster from the bound orchestration plane. Tiles update live as
          sessions transition.
        </p>
      </header>

      <div className="min-h-0 flex-1 overflow-auto p-6" data-testid="agents-grid">
        {error ? (
          <div
            className="rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
            data-testid="agents-error"
          >
            {error.message}
          </div>
        ) : isLoading ? (
          <div className="text-sm text-neutral-500" data-testid="agents-loading">
            Loading…
          </div>
        ) : sortedAgents.length === 0 ? (
          <div
            className="rounded-md border border-dashed border-neutral-300 p-8 text-center text-sm text-neutral-500 dark:border-neutral-700"
            data-testid="agents-empty"
          >
            No agents registered. The bound orchestration plane has an empty
            roster — start a session to populate it.
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {sortedAgents.map((agent) => (
              <AgentTile
                key={agent.id}
                agent={agent}
                session={sessionsByAgent.get(agent.id)}
                fallbackRecent={mostRecentByAgent.get(agent.id)}
                onSelect={() => setOpenAgentId(agent.id)}
              />
            ))}
          </div>
        )}
      </div>

      <AgentDetailDrawer
        agent={openAgent}
        session={openSession}
        onClose={() => setOpenAgentId(null)}
      />
    </div>
  );
}

interface AgentTileProps {
  agent: AgentRef;
  session: Session | undefined;
  fallbackRecent: Session | undefined;
  onSelect: () => void;
}

function AgentTile({ agent, session, fallbackRecent, onSelect }: AgentTileProps) {
  const Icon: LucideIcon = agent.agent_kind === 'human' ? User : Bot;
  // Humans get a softer surface than agents so the operator can scan
  // for "who's a person vs who's a bot" without reading every label.
  const kindStyles =
    agent.agent_kind === 'human'
      ? 'bg-amber-50 border-amber-200 hover:bg-amber-100 dark:bg-amber-950/40 dark:border-amber-800 dark:hover:bg-amber-950'
      : 'bg-white border-neutral-200 hover:bg-neutral-50 dark:bg-neutral-900 dark:border-neutral-800 dark:hover:bg-neutral-800';

  const isActive = !!session;
  const recent = session ?? fallbackRecent;

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        'flex w-full flex-col gap-2 rounded-lg border p-4 text-left transition-colors',
        kindStyles
      )}
      data-testid={`agent-tile-${agent.id}`}
      data-agent-kind={agent.agent_kind}
      data-active={isActive || undefined}
    >
      <div className="flex items-center gap-2">
        <Icon
          className={cn(
            'h-4 w-4 shrink-0',
            agent.agent_kind === 'human'
              ? 'text-amber-600 dark:text-amber-400'
              : 'text-sky-600 dark:text-sky-400'
          )}
          aria-hidden
          data-testid={`agent-tile-${agent.id}-icon-${agent.agent_kind}`}
        />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{agent.name || agent.id}</div>
          <div className="truncate font-mono text-[11px] text-neutral-500 dark:text-neutral-400">
            {agent.id}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap gap-1 text-[11px]">
        {agent.role ? (
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
            {agent.role}
          </span>
        ) : null}
        {agent.dialect ? (
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
            {agent.dialect}
          </span>
        ) : null}
      </div>

      {session ? (
        <div className="flex items-center gap-2 border-t border-neutral-200/60 pt-2 text-[11px] dark:border-neutral-800">
          <span
            className={cn(
              'rounded px-1.5 py-0.5 font-medium uppercase tracking-wide',
              SESSION_STATUS_TONE[session.status]
            )}
            data-testid={`agent-tile-${agent.id}-status`}
          >
            {session.status}
          </span>
          <span className="truncate font-mono text-neutral-500" title={session.assignment_id}>
            {session.assignment_id}
          </span>
        </div>
      ) : (
        <div
          className="border-t border-neutral-200/60 pt-2 text-[11px] text-neutral-500 dark:border-neutral-800"
          data-testid={`agent-tile-${agent.id}-idle`}
        >
          idle
        </div>
      )}

      {recent ? (
        <div className="text-[10px] text-neutral-500" data-testid={`agent-tile-${agent.id}-recent`}>
          {formatRecent(recent)}
        </div>
      ) : null}
    </button>
  );
}

// formatRecent picks the freshest timestamp on a Session and labels it
// for the tile. Heartbeat wins because it's the ground truth that the
// session is alive; ended_at next so a recently-completed session is
// signposted clearly; started_at last as a final fallback.
function formatRecent(s: Session): string {
  if (s.last_heartbeat) return `heartbeat ${shortTime(s.last_heartbeat)}`;
  if (s.ended_at) return `ended ${shortTime(s.ended_at)}`;
  if (s.started_at) return `started ${shortTime(s.started_at)}`;
  return '';
}

// shortTime formats an ISO8601 timestamp as HH:MM in the local zone.
// The dashboard is the wrong place for absolute dates — operators
// scan for "is this minutes-old or hours-old" — so we render the time
// component and let the drawer show the full timestamp on click.
function shortTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const hh = d.getHours().toString().padStart(2, '0');
  const mm = d.getMinutes().toString().padStart(2, '0');
  return `${hh}:${mm}`;
}
