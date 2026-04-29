// EscalationsPage (gm-e11.8.1). Operator-attention inbox at /escalations.
//
// Replaces the 5-line placeholder with the real surface contract from
// docs/ui-spec.md §5.8: severity-grouped sections (critical → high →
// medium → low → info), per-card primary "Resolve" CTA opening a
// nonce-gated modal with approve / deny / modify / defer.
//
// Sidebar badge, bulk multi-select, hand-off mini-modal, and filters
// are explicit follow-ups (gm-e11.8.2/.3/.4/.5) — out of scope here.

import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  Database,
  HelpCircle,
  Inbox,
  ListChecks,
  MessageSquareWarning,
  PauseCircle,
  ShieldCheck,
  ShieldQuestion,
  Sparkles,
  XOctagon,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useEscalations, useRespondEscalation } from '@/hooks/useEscalations';
import type {
  EscalationKind,
  EscalationRequest,
  EscalationResolutionKind,
} from '@/api/escalations';
import { relativeTime } from '@/components/board/relativeTime';

type Severity = 'critical' | 'high' | 'medium' | 'low' | 'info';

const SEVERITY_ORDER: Severity[] = ['critical', 'high', 'medium', 'low', 'info'];

interface SeverityMeta {
  label: string;
  // Tailwind utility tokens for the section divider chip and per-card pill.
  pillClass: string;
}

const SEVERITY_META: Record<Severity, SeverityMeta> = {
  // Color choices target WCAG 2 AA against the page background:
  // - red-700 / amber-50 text: 5.8:1 light, > 4.5:1 dark
  // - orange-600 / white: ~4.6:1 in both themes
  // - amber-500 / neutral-900: ~5.1:1 (dark text on amber)
  // - slate-500 / white: ~4.5:1
  critical: {
    label: 'Critical',
    pillClass: 'bg-red-700 text-red-50',
  },
  high: {
    label: 'High',
    pillClass: 'bg-orange-600 text-white',
  },
  medium: {
    label: 'Medium',
    pillClass: 'bg-amber-500 text-neutral-900',
  },
  low: {
    label: 'Low',
    pillClass: 'bg-slate-500 text-white',
  },
  info: {
    label: 'Info',
    pillClass: 'bg-neutral-300 text-neutral-900 dark:bg-neutral-700 dark:text-neutral-100',
  },
};

// severityFor — derive the operator-facing severity tier from
// (kind, urgency). The three cases that escalate to "critical" are the
// ones that block forward progress under any mode:
//   - beads_degraded: the workplane itself is half-down
//   - permission_prompt / hitl_approval @ blocking: agent is stalled
//     waiting for the operator
// Everything else falls to high/medium/low based on urgency. No real
// data emits "info" today; the bucket is reserved.
export function severityFor(esc: EscalationRequest): Severity {
  if (esc.source === 'beads_degraded') return 'critical';
  if (esc.urgency === 'blocking') {
    if (esc.source === 'permission_prompt' || esc.source === 'hitl_approval') {
      return 'critical';
    }
    return 'high';
  }
  if (esc.source === 'witness_finding' || esc.source === 'refinery_rejection') {
    return 'high';
  }
  if (esc.urgency === 'advisory') return 'medium';
  return 'low';
}

// Per-kind icon picks. Lucide names chosen to match the operator's
// intuition at a glance — escalation-style glyphs across the board.
const KIND_ICON: Record<EscalationKind, LucideIcon> = {
  mcp_elicitation: HelpCircle,
  a2a_input_required: HelpCircle,
  permission_prompt: ShieldQuestion,
  hitl_approval: ShieldCheck,
  orchestrator_pause: PauseCircle,
  blocker: XOctagon,
  question: MessageSquareWarning,
  witness_finding: Sparkles,
  refinery_rejection: ListChecks,
  beads_degraded: Database,
};

const KIND_LABEL: Record<EscalationKind, string> = {
  mcp_elicitation: 'MCP elicitation',
  a2a_input_required: 'A2A input required',
  permission_prompt: 'Permission prompt',
  hitl_approval: 'HITL approval',
  orchestrator_pause: 'Orchestrator paused',
  blocker: 'Blocker',
  question: 'Question',
  witness_finding: 'Witness finding',
  refinery_rejection: 'Refinery rejection',
  beads_degraded: 'Beads degraded',
};

export function EscalationsPage(): JSX.Element {
  const { data: escalations = [], isLoading, error } = useEscalations();

  // Open-only — resolved/canceled/expired don't belong in an inbox.
  // Group by derived severity, sort within each group by created_at desc.
  const sections = useMemo(() => {
    const buckets: Record<Severity, EscalationRequest[]> = {
      critical: [],
      high: [],
      medium: [],
      low: [],
      info: [],
    };
    for (const esc of escalations) {
      if (esc.state !== 'open') continue;
      buckets[severityFor(esc)].push(esc);
    }
    for (const sev of SEVERITY_ORDER) {
      buckets[sev].sort((a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''));
    }
    return buckets;
  }, [escalations]);

  const totalOpen = SEVERITY_ORDER.reduce((acc, s) => acc + sections[s].length, 0);

  return (
    <div className="flex h-full flex-col" data-testid="escalations-page">
      <header className="border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <h1 className="text-xl font-semibold tracking-tight">Escalations</h1>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Operator-attention queue from the bound OrchestrationPlane.
        </p>
      </header>

      <div className="flex-1 overflow-auto px-8 py-6">
        {error && (
          <div
            data-testid="escalations-error"
            role="alert"
            className="mb-4 rounded-md border border-red-200 bg-red-50 px-4 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200"
          >
            {error.message}
          </div>
        )}

        {isLoading && <LoadingSkeleton />}

        {!isLoading && totalOpen === 0 && !error && <EmptyState />}

        {!isLoading && totalOpen > 0 && (
          <div className="flex flex-col gap-8">
            {SEVERITY_ORDER.map((sev) => {
              const items = sections[sev];
              if (items.length === 0) return null;
              return (
                <section
                  key={sev}
                  data-testid={`escalations-section-${sev}`}
                  data-severity={sev}
                >
                  <header className="mb-2 flex items-center gap-2">
                    <span
                      className={
                        'rounded-md px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide ' +
                        SEVERITY_META[sev].pillClass
                      }
                    >
                      {SEVERITY_META[sev].label}
                    </span>
                    <span className="text-xs text-neutral-500 dark:text-neutral-400">
                      {items.length} open
                    </span>
                  </header>
                  <ul className="flex flex-col gap-2">
                    {items.map((esc) => (
                      <li key={esc.id}>
                        <EscalationCard escalation={esc} />
                      </li>
                    ))}
                  </ul>
                </section>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

interface EscalationCardProps {
  escalation: EscalationRequest;
}

function EscalationCard({ escalation }: EscalationCardProps) {
  const [resolveOpen, setResolveOpen] = useState(false);
  const Icon = KIND_ICON[escalation.source] ?? AlertTriangle;
  const sev = severityFor(escalation);
  const sevMeta = SEVERITY_META[sev];

  return (
    <div
      data-testid={`escalation-card-${escalation.id}`}
      data-kind={escalation.source}
      data-severity={sev}
      className="rounded-md border border-neutral-200 bg-white p-4 shadow-sm dark:border-neutral-800 dark:bg-neutral-950"
    >
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className={
              'inline-flex items-center rounded-md px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ' +
              sevMeta.pillClass
            }
            data-testid={`escalation-card-${escalation.id}-severity`}
          >
            {sevMeta.label}
          </span>
          <span className="inline-flex items-center gap-1 rounded-md bg-neutral-100 px-2 py-0.5 text-xs font-medium text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
            <Icon className="h-3.5 w-3.5" aria-hidden />
            {KIND_LABEL[escalation.source] ?? escalation.source}
          </span>
        </div>
        <span
          className="shrink-0 text-xs text-neutral-500 dark:text-neutral-400"
          title={escalation.created_at}
          data-testid={`escalation-card-${escalation.id}-age`}
        >
          {relativeTime(escalation.created_at)} ago
        </span>
      </div>

      <h3
        className="mb-1 truncate text-sm font-medium"
        title={escalation.title}
        data-testid={`escalation-card-${escalation.id}-title`}
      >
        {escalation.title}
      </h3>

      {escalation.prompt && (
        <p
          className="mb-3 line-clamp-3 whitespace-pre-wrap text-xs text-neutral-600 dark:text-neutral-400"
          data-testid={`escalation-card-${escalation.id}-prompt`}
        >
          {escalation.prompt}
        </p>
      )}

      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {escalation.assignment_id && (
            <Link
              to={`/sessions/${encodeURIComponent(escalation.assignment_id)}`}
              data-testid={`escalation-card-${escalation.id}-agent`}
              className="inline-flex items-center gap-1 rounded-md border border-neutral-200 px-2 py-0.5 text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              <span className="text-[10px] uppercase tracking-wide text-neutral-500">
                agent
              </span>
              <span className="font-mono">{escalation.agent_id ?? escalation.assignment_id}</span>
            </Link>
          )}
          {escalation.work_item_id && (
            <Link
              to={`/board?bead=${encodeURIComponent(escalation.work_item_id)}`}
              data-testid={`escalation-card-${escalation.id}-workitem`}
              className="inline-flex items-center gap-1 rounded-md border border-neutral-200 px-2 py-0.5 text-neutral-700 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              <span className="text-[10px] uppercase tracking-wide text-neutral-500">
                bead
              </span>
              <span className="font-mono">{escalation.work_item_id}</span>
            </Link>
          )}
        </div>
        <button
          type="button"
          onClick={() => setResolveOpen(true)}
          data-testid={`escalation-card-${escalation.id}-resolve`}
          className="rounded-md bg-neutral-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          Resolve
        </button>
      </div>

      {resolveOpen && (
        <ResolveModal
          escalation={escalation}
          onClose={() => setResolveOpen(false)}
        />
      )}
    </div>
  );
}

interface ResolveModalProps {
  escalation: EscalationRequest;
  onClose: () => void;
}

const RESOLUTION_KINDS: { kind: EscalationResolutionKind; label: string; help: string }[] = [
  { kind: 'approve', label: 'Approve', help: 'Confirm the agent\u2019s requested action.' },
  { kind: 'deny', label: 'Deny', help: 'Block the action; the agent is told no.' },
  { kind: 'modify', label: 'Modify', help: 'Reply with a different value or instructions.' },
  { kind: 'defer', label: 'Defer', help: 'Mark as not-yet-decided; stays answerable later.' },
];

function ResolveModal({ escalation, onClose }: ResolveModalProps): JSX.Element {
  const respond = useRespondEscalation();
  const [kind, setKind] = useState<EscalationResolutionKind>('approve');
  const [value, setValue] = useState('');

  // Close-on-escape; mirrors RatifyModal's behavior. Don't close while
  // a mutation is in flight — the operator already committed.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !respond.isPending) onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, respond.isPending]);

  const submit = () => {
    const body = kind === 'modify' ? { kind, value } : { kind };
    respond.mutate(
      { id: escalation.id, body },
      {
        onSuccess: () => {
          onClose();
        },
      }
    );
  };

  const modifyEmpty = kind === 'modify' && value.trim() === '';
  const disabled = respond.isPending || modifyEmpty;

  return (
    <div
      role="dialog"
      aria-modal="true"
      data-testid="escalation-resolve-modal"
      data-escalation-id={escalation.id}
      className="fixed inset-0 z-50 flex items-center justify-center bg-neutral-900/40 p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget && !respond.isPending) onClose();
      }}
    >
      <div className="flex w-full max-w-md flex-col rounded-lg border border-neutral-200 bg-white shadow-xl dark:border-neutral-800 dark:bg-neutral-950">
        <header className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <h2 className="text-sm font-semibold">Resolve escalation</h2>
          <p className="truncate text-xs text-neutral-500 dark:text-neutral-400">
            {escalation.title}
          </p>
        </header>

        <fieldset className="flex flex-col gap-2 px-4 py-3">
          <legend className="sr-only">Pick a resolution kind</legend>
          {RESOLUTION_KINDS.map((opt) => (
            <label
              key={opt.kind}
              data-testid={`escalation-resolve-option-${opt.kind}`}
              className={
                'flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2 text-xs ' +
                (kind === opt.kind
                  ? 'border-neutral-900 bg-neutral-50 dark:border-neutral-100 dark:bg-neutral-900'
                  : 'border-neutral-200 hover:bg-neutral-50 dark:border-neutral-800 dark:hover:bg-neutral-900')
              }
            >
              <input
                type="radio"
                name="resolution-kind"
                value={opt.kind}
                checked={kind === opt.kind}
                onChange={() => setKind(opt.kind)}
                disabled={respond.isPending}
                className="mt-0.5"
              />
              <span className="flex flex-col">
                <span className="font-medium">{opt.label}</span>
                <span className="text-[11px] text-neutral-500 dark:text-neutral-400">
                  {opt.help}
                </span>
              </span>
            </label>
          ))}
        </fieldset>

        {kind === 'modify' && (
          <div className="px-4 pb-3">
            <label
              htmlFor="escalation-resolve-modify-value"
              className="mb-1 block text-[11px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400"
            >
              Reply
            </label>
            <textarea
              id="escalation-resolve-modify-value"
              data-testid="escalation-resolve-modify-value"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              rows={3}
              placeholder="What should the agent do instead?"
              className="w-full rounded-md border border-neutral-300 px-3 py-2 text-xs dark:border-neutral-700 dark:bg-neutral-900"
            />
          </div>
        )}

        {respond.error && (
          <div
            data-testid="escalation-resolve-error"
            className="mx-4 mb-3 rounded-md border border-rose-300 bg-rose-50 px-3 py-1.5 text-[11px] text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
          >
            {respond.error.message}
          </div>
        )}

        <footer className="flex items-center justify-end gap-2 border-t border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <button
            type="button"
            data-testid="escalation-resolve-cancel"
            onClick={onClose}
            disabled={respond.isPending}
            className="rounded border border-neutral-300 px-3 py-1 text-xs hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="escalation-resolve-confirm"
            onClick={submit}
            disabled={disabled}
            className="rounded bg-emerald-600 px-3 py-1 text-xs font-semibold text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {respond.isPending ? 'Resolving\u2026' : 'Confirm'}
          </button>
        </footer>
      </div>
    </div>
  );
}

function EmptyState(): JSX.Element {
  return (
    <div
      data-testid="escalations-empty"
      className="mx-auto mt-8 max-w-md rounded-md border border-dashed border-neutral-300 bg-white px-6 py-10 text-center dark:border-neutral-700 dark:bg-neutral-950"
    >
      <Inbox className="mx-auto mb-2 h-6 w-6 text-neutral-400" aria-hidden />
      <h2 className="mb-1 text-sm font-medium">No open escalations</h2>
      <p className="text-xs text-neutral-500 dark:text-neutral-400">
        Escalations stream in from the bound OrchestrationPlane when an
        agent needs operator input. Bind an orchestration plane (or wait
        for one to ask) to see items here.
      </p>
    </div>
  );
}

function LoadingSkeleton(): JSX.Element {
  return (
    <div
      data-testid="escalations-loading"
      aria-busy="true"
      aria-label="Loading escalations"
      className="flex flex-col gap-3"
    >
      {[0, 1, 2].map((i) => (
        <div
          key={i}
          className="h-24 animate-pulse rounded-md border border-neutral-200 bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-900"
        />
      ))}
    </div>
  );
}
