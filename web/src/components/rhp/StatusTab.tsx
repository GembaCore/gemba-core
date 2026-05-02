import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  AlertTriangle,
  Activity,
  CheckCircle2,
  CircleDot,
  ExternalLink,
  Play,
  Terminal,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useRhp } from './RhpContext';
import { useRhpPinnedContent } from './RhpPinnedContent';
import { useSessions } from '@/hooks/useSessions';
import { useEscalations } from '@/hooks/useEscalations';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useCapabilities } from '@/capabilities';
import { NewSessionDialog } from '@/components/sessions/NewSessionDialog';
import { relativeTime } from '@/components/board/relativeTime';
import { cn } from '@/lib/utils';
import { encodeInteractionTarget } from '@/interactions/types';
import { INTERACTION_DETAIL_KIND } from '@/components/rhp/details/InteractionDetail';
import type { Session } from '@/api/sessions';
import type { EscalationRequest } from '@/api/escalations';
import type { WorkItem } from '@/types/core.gen';

type AdaptorStatus = {
  name: string;
  plane: 'work' | 'orchestration';
  healthy: boolean;
  reason?: string;
};

type AdaptorsResponse = {
  adaptors: AdaptorStatus[];
};

const TERMINAL_SESSION_STATUSES = new Set<Session['status']>(['completed', 'failed']);

export function StatusBody() {
  const { data: sessions = [], isLoading: sessionsLoading } = useSessions();
  const { data: escalations = [] } = useEscalations();
  const { data: workItems = [] } = useWorkItems();
  const { orchestrationPlane, workPlane } = useCapabilities();
  const { data: adaptors } = useQuery<AdaptorsResponse>({
    queryKey: ['adaptors'],
    queryFn: async () => ({ adaptors: [] }),
    enabled: false,
  });
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const navigate = useNavigate();
  const { popDetail } = useRhp();

  const activeSessions = useMemo(
    () => sessions.filter((s) => !TERMINAL_SESSION_STATUSES.has(s.status)),
    [sessions]
  );
  const openEscalations = useMemo(
    () => escalations.filter((e) => e.state === 'open'),
    [escalations]
  );
  const metrics = useMemo(
    () => buildMetrics({ sessions, activeSessions, escalations: openEscalations, workItems }),
    [sessions, activeSessions, openEscalations, workItems]
  );
  const degraded = (adaptors?.adaptors ?? []).filter((a) => !a.healthy);

  return (
    <div className="min-h-full px-4 py-4" data-testid="rhp-status-body">
      <header className="mb-4 flex items-start gap-2">
        <Activity className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600 dark:text-emerald-300" />
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">
            Status
          </h2>
          <div className="mt-1 flex flex-wrap gap-1.5 text-[11px]">
            <Pill label={workPlane?.adaptor_name ?? 'No work plane'} tone={workPlane ? 'ok' : 'warn'} />
            <Pill
              label={orchestrationPlane?.adaptor_id ?? 'No orchestration'}
              tone={orchestrationPlane ? 'ok' : 'warn'}
            />
          </div>
        </div>
      </header>

      <MetricGrid metrics={metrics} />

      <section className="mt-5 space-y-2" data-testid="rhp-status-sessions">
        <SectionHeader
          title="Sessions"
          actionLabel="New"
          actionIcon={<Play className="h-3 w-3" />}
          onAction={() => setNewSessionOpen(true)}
        />
        {sessionsLoading ? (
          <Muted>Loading…</Muted>
        ) : activeSessions.length === 0 ? (
          <Muted>No active sessions.</Muted>
        ) : (
          <ShowMoreList
            items={activeSessions}
            initialCount={4}
            renderItem={(session) => (
              <SessionRow
                key={session.id}
                session={session}
                onOpen={() =>
                  popDetail({
                    kind: INTERACTION_DETAIL_KIND,
                    id: encodeInteractionTarget({ type: 'session', id: session.id }),
                  })
                }
              />
            )}
          />
        )}
      </section>

      <section className="mt-5 space-y-2" data-testid="rhp-status-escalations">
        <SectionHeader
          title="Escalations"
          actionLabel="Inbox"
          actionIcon={<ExternalLink className="h-3 w-3" />}
          onAction={() => navigate('/escalations')}
        />
        {openEscalations.length === 0 ? (
          <Muted>No open escalations.</Muted>
        ) : (
          <ShowMoreList
            items={openEscalations}
            initialCount={3}
            renderItem={(escalation) => (
              <EscalationRow
                key={escalation.id}
                escalation={escalation}
                onOpen={() =>
                  popDetail({
                    kind: INTERACTION_DETAIL_KIND,
                    id: encodeInteractionTarget({ type: 'escalation', id: escalation.id }),
                  })
                }
              />
            )}
          />
        )}
      </section>

      <section className="mt-5 space-y-2" data-testid="rhp-status-runtime">
        <SectionHeader title="Runtime" />
        <RuntimeRow label="Work plane" value={workPlane?.adaptor_name ?? 'not configured'} />
        <RuntimeRow
          label="Orchestration"
          value={orchestrationPlane?.adaptor_id ?? 'not configured'}
        />
        {degraded.length > 0 ? (
          <div className="space-y-1">
            {degraded.map((adaptor) => (
              <div
                key={`${adaptor.plane}:${adaptor.name}`}
                className="rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-xs text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-100"
              >
                <span className="font-medium">{adaptor.name}</span>
                {adaptor.reason ? <span className="text-amber-800/80 dark:text-amber-200/80">: {adaptor.reason}</span> : null}
              </div>
            ))}
          </div>
        ) : (
          <div className="flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-300">
            <CheckCircle2 className="h-3.5 w-3.5" />
            Adaptors healthy
          </div>
        )}
      </section>

      <section className="mt-5 space-y-2" data-testid="rhp-status-work">
        <SectionHeader
          title="Workspace"
          actionLabel="Board"
          actionIcon={<ExternalLink className="h-3 w-3" />}
          onAction={() => navigate('/board')}
        />
        <RuntimeRow label="Total beads" value={String(workItems.length)} />
        <RuntimeRow label="In progress" value={String(countState(workItems, 'started'))} />
        <RuntimeRow label="Done" value={String(countState(workItems, 'completed'))} />
      </section>

      {newSessionOpen ? (
        <NewSessionDialog open={newSessionOpen} onClose={() => setNewSessionOpen(false)} />
      ) : null}
    </div>
  );
}

export function StatusTab() {
  const { registerPinnedTab } = useRhp();
  const { register } = useRhpPinnedContent();

  useEffect(() => {
    return registerPinnedTab({
      id: 'status',
      icon: Activity,
      label: 'Status',
    });
  }, [registerPinnedTab]);

  useEffect(() => {
    return register('status', () => <StatusBody />);
  }, [register]);

  return null;
}

function buildMetrics({
  sessions,
  activeSessions,
  escalations,
  workItems,
}: {
  sessions: Session[];
  activeSessions: Session[];
  escalations: EscalationRequest[];
  workItems: WorkItem[];
}) {
  const tokenEstimate = sessions.reduce((sum, session) => sum + tokensFromMetadata(session.provider_metadata), 0);
  return [
    { label: 'Tokens', value: formatCompact(tokenEstimate) },
    { label: 'Active', value: String(activeSessions.length) },
    { label: 'Escalations', value: String(escalations.length) },
    { label: 'Working', value: String(activeSessions.filter((s) => s.status === 'working').length) },
    { label: 'Prompting', value: String(activeSessions.filter((s) => s.status === 'prompting').length) },
    { label: 'Done beads', value: String(countState(workItems, 'completed')) },
  ];
}

function MetricGrid({ metrics }: { metrics: Array<{ label: string; value: string }> }) {
  return (
    <div className="grid grid-cols-3 gap-2" data-testid="rhp-status-metrics">
      {metrics.map((metric) => (
        <div
          key={metric.label}
          className="rounded-md border border-neutral-200 bg-white px-2 py-2 dark:border-neutral-800 dark:bg-neutral-950"
        >
          <div className="truncate text-[10px] uppercase tracking-wide text-neutral-500">
            {metric.label}
          </div>
          <div className="mt-1 truncate text-sm font-semibold text-neutral-900 dark:text-neutral-100">
            {metric.value}
          </div>
        </div>
      ))}
    </div>
  );
}

function SessionRow({ session, onOpen }: { session: Session; onOpen: () => void }) {
  const agent =
    typeof session.provider_metadata?.agent_type === 'string'
      ? session.provider_metadata.agent_type
      : session.agent_id;
  const bead =
    typeof session.provider_metadata?.bead_id === 'string'
      ? session.provider_metadata.bead_id
      : session.assignment_id;
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full items-start gap-2 rounded-md border border-neutral-200 bg-white px-2 py-2 text-left hover:bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950 dark:hover:bg-neutral-900"
    >
      <CircleDot className={cn('mt-0.5 h-3.5 w-3.5 shrink-0', statusTone(session.status))} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1 text-xs font-medium text-neutral-800 dark:text-neutral-200">
          <Terminal className="h-3 w-3" />
          <span className="truncate">{agent}</span>
        </span>
        <span className="mt-0.5 block truncate font-mono text-[11px] text-neutral-500">{bead}</span>
      </span>
      <span className="shrink-0 rounded bg-neutral-100 px-1.5 py-0.5 text-[10px] text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
        {session.status}
      </span>
    </button>
  );
}

function EscalationRow({
  escalation,
  onOpen,
}: {
  escalation: EscalationRequest;
  onOpen: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-2 py-2 text-left hover:bg-amber-100 dark:border-amber-900/60 dark:bg-amber-950/30 dark:hover:bg-amber-950/50"
    >
      <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-700 dark:text-amber-300" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium text-amber-950 dark:text-amber-100">
          {escalation.title}
        </span>
        <span className="mt-0.5 block truncate text-[11px] text-amber-800/80 dark:text-amber-200/80">
          {escalation.source} · {relativeTime(escalation.created_at)}
        </span>
      </span>
    </button>
  );
}

function SectionHeader({
  title,
  actionLabel,
  actionIcon,
  onAction,
}: {
  title: string;
  actionLabel?: string;
  actionIcon?: React.ReactNode;
  onAction?: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-neutral-500">{title}</h3>
      {onAction && actionLabel ? (
        <button
          type="button"
          onClick={onAction}
          className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        >
          {actionIcon}
          {actionLabel}
        </button>
      ) : null}
    </div>
  );
}

function ShowMoreList<T>({
  items,
  initialCount,
  renderItem,
}: {
  items: T[];
  initialCount: number;
  renderItem: (item: T) => React.ReactNode;
}) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? items : items.slice(0, initialCount);
  return (
    <div className="space-y-2">
      {visible.map(renderItem)}
      {items.length > initialCount ? (
        <button
          type="button"
          onClick={() => setExpanded((next) => !next)}
          className="text-xs text-neutral-500 hover:text-neutral-800 dark:hover:text-neutral-200"
        >
          {expanded ? 'Show less' : `Show ${items.length - initialCount} more`}
        </button>
      ) : null}
    </div>
  );
}

function RuntimeRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2 text-xs">
      <span className="text-neutral-500">{label}</span>
      <span className="truncate font-mono text-neutral-800 dark:text-neutral-200">{value}</span>
    </div>
  );
}

function Pill({ label, tone }: { label: string; tone: 'ok' | 'warn' }) {
  return (
    <span
      className={cn(
        'rounded px-1.5 py-0.5',
        tone === 'ok'
          ? 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200'
          : 'bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-200'
      )}
    >
      {label}
    </span>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <div className="text-xs text-neutral-500">{children}</div>;
}

function countState(items: WorkItem[], state: WorkItem['state_category']): number {
  return items.filter((item) => item.state_category === state).length;
}

function statusTone(status: Session['status']): string {
  if (status === 'working') return 'text-emerald-500';
  if (status === 'prompting' || status === 'stalled') return 'text-amber-500';
  return 'text-sky-500';
}

function tokensFromMetadata(value: unknown): number {
  if (typeof value !== 'object' || value === null) return 0;
  let sum = 0;
  for (const [key, child] of Object.entries(value)) {
    if (typeof child === 'number' && Number.isFinite(child) && key.toLowerCase().includes('token')) {
      sum += child;
    } else if (typeof child === 'object' && child !== null) {
      sum += tokensFromMetadata(child);
    }
  }
  return sum;
}

function formatCompact(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}
