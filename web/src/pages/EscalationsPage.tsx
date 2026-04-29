// EscalationsPage (gm-e11.8.1 / gm-e11.8.3 / gm-e11.8.4).
//
// gm-e11.8.1 shipped the severity-grouped inbox with per-card Resolve CTA.
// gm-e11.8.3 adds multi-select + bulk triage:
//   - Per-card checkbox (top-left, controlled via page-local Set<string>).
//   - Sticky selection toolbar (above severity sections) when selection >= 1.
//   - Bulk Dismiss: calls useRespondEscalation with kind='defer' for each
//     selected id; shows "working..." state; surfaces per-item errors.
//   - Bulk Move to Gemba walk: pushes each escalation onto the active walk's
//     agenda via WalkContext.addItem; hidden when no walk is active.
//   - Section-level select-all / select-none toggle above each severity header.
// gm-e11.8.4 adds per-card Hand-off secondary action:
//   - Opens a mini-modal with a persona picker (sourced from DEFAULT_PERSONAS).
//   - On Confirm: POST /api/consults with the selected persona + escalation ctx.
//   - Hand-off does NOT resolve the escalation — it stays open in the inbox.
//   - Modal closes on success with a transient confirmation banner.
//
// NOTE: /api/personas does not exist yet (follow-up: gm-uipx parent epic).
// NOTE: skill_id 'escalation_handoff' is a stub — the dispatcher skill does
//       not exist yet. Filed as gm-e11.8.4-stubs. Update HANDOFF_SKILL_ID
//       when the skill lands.
//
// Sidebar badge and filters are gm-e11.8.2/.5.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
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
import { createConsult, freshNonce } from '@/api/consults';
import type { ConsultSummary } from '@/api/consults';
import { DEFAULT_PERSONAS } from '@/components/pm/PmPanelContext';
import { relativeTime } from '@/components/board/relativeTime';
import { useWalk } from '@/components/walk/WalkContext';

// TODO(gm-e11.8.4-stubs): replace with the real skill id once the
// escalation_handoff dispatcher skill is registered on the server.
const HANDOFF_SKILL_ID = 'escalation_handoff';
// Workspace default mirrors SprintsPage.tsx: when the escalation carries
// no repository context we fall back to 'default'.
const HANDOFF_WORKSPACE = 'default';

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

// ── BulkDismissResult — aggregates outcome of the dismiss loop ──────────────

interface BulkDismissResult {
  succeeded: number;
  failed: { id: string; reason: string }[];
}

export function EscalationsPage(): JSX.Element {
  const { data: escalations = [], isLoading, error } = useEscalations();
  const respond = useRespondEscalation();
  const { active: walkActive, addItem: addWalkItem } = useWalk();

  // ── Selection state ──────────────────────────────────────────────────────
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [bulkWorking, setBulkWorking] = useState(false);
  const [bulkResult, setBulkResult] = useState<BulkDismissResult | null>(null);

  const toggleSelect = useCallback((id: string) => {
    setSelection((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const clearSelection = useCallback(() => {
    setSelection(new Set());
    setBulkResult(null);
  }, []);

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

  // ── Bulk dismiss ─────────────────────────────────────────────────────────
  // Calls useRespondEscalation for each selected ID with kind='defer'.
  // Uses a ref-captured version of respond.mutateAsync to avoid stale
  // closures across the sequential await loop.
  const respondRef = useRef(respond);
  respondRef.current = respond;

  const handleBulkDismiss = useCallback(async () => {
    if (bulkWorking || selection.size === 0) return;
    setBulkWorking(true);
    setBulkResult(null);

    const ids = Array.from(selection);
    const failed: BulkDismissResult['failed'] = [];
    let succeeded = 0;

    for (const id of ids) {
      try {
        await respondRef.current.mutateAsync({ id, body: { kind: 'defer' } });
        succeeded++;
      } catch (err) {
        failed.push({
          id,
          reason: err instanceof Error ? err.message : String(err),
        });
      }
    }

    setBulkWorking(false);
    setBulkResult({ succeeded, failed });
    // Clear selection; failed items stay in the inbox naturally.
    setSelection(new Set());
  }, [bulkWorking, selection]);

  // ── Bulk move to walk ────────────────────────────────────────────────────
  // Pushes each selected escalation onto the active walk's agenda as a
  // queued item with source.kind='escalation'. Does not resolve them.
  const handleBulkMoveToWalk = useCallback(async () => {
    if (bulkWorking || selection.size === 0 || !walkActive) return;
    setBulkWorking(true);
    setBulkResult(null);

    const ids = Array.from(selection);
    // Build a map for quick lookup
    const escById = new Map(escalations.map((e) => [e.id, e]));

    for (const id of ids) {
      const esc = escById.get(id);
      if (!esc) continue;
      try {
        await addWalkItem({
          topic: esc.title,
          source: { kind: 'escalation', ref: id },
          priority: 50,
        });
      } catch {
        // Errors are soft — the item may still have been queued. Do not
        // block the rest of the batch.
      }
    }

    setBulkWorking(false);
    setSelection(new Set());
  }, [bulkWorking, selection, walkActive, escalations, addWalkItem]);

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

        {/* ── Sticky selection toolbar ──────────────────────────────────── */}
        {selection.size >= 1 && (
          <BulkToolbar
            count={selection.size}
            working={bulkWorking}
            walkActive={walkActive}
            result={bulkResult}
            onDismiss={handleBulkDismiss}
            onMoveToWalk={handleBulkMoveToWalk}
            onClear={clearSelection}
          />
        )}

        {isLoading && <LoadingSkeleton />}

        {!isLoading && totalOpen === 0 && !error && <EmptyState />}

        {!isLoading && totalOpen > 0 && (
          <div className="flex flex-col gap-8">
            {SEVERITY_ORDER.map((sev) => {
              const items = sections[sev];
              if (items.length === 0) return null;
              const sectionIds = items.map((e) => e.id);
              const selectedInSection = sectionIds.filter((id) => selection.has(id));
              const allSelected = selectedInSection.length === sectionIds.length;

              const toggleSection = () => {
                setSelection((prev) => {
                  const next = new Set(prev);
                  if (allSelected) {
                    sectionIds.forEach((id) => next.delete(id));
                  } else {
                    sectionIds.forEach((id) => next.add(id));
                  }
                  return next;
                });
              };

              return (
                <section
                  key={sev}
                  data-testid={`escalations-section-${sev}`}
                  data-severity={sev}
                >
                  {/* Section select-all / select-none chrome */}
                  <div className="mb-1 flex items-center justify-between gap-2">
                    <header className="flex items-center gap-2">
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
                    <button
                      type="button"
                      data-testid={`escalations-section-${sev}-select-toggle`}
                      onClick={toggleSection}
                      className="text-xs text-neutral-400 hover:text-neutral-700 dark:hover:text-neutral-200"
                    >
                      {allSelected ? (
                        <span>
                          <span className="font-medium text-neutral-600 dark:text-neutral-300">
                            {selectedInSection.length} / {items.length}
                          </span>{' '}
                          selected &mdash; select none
                        </span>
                      ) : (
                        <span>
                          {selectedInSection.length > 0 && (
                            <span className="font-medium text-neutral-600 dark:text-neutral-300">
                              {selectedInSection.length} / {items.length}
                            </span>
                          )}{' '}
                          select all
                        </span>
                      )}
                    </button>
                  </div>
                  <ul className="flex flex-col gap-2">
                    {items.map((esc) => (
                      <li key={esc.id}>
                        <EscalationCard
                          escalation={esc}
                          selected={selection.has(esc.id)}
                          onToggleSelect={toggleSelect}
                        />
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

// ── BulkToolbar ──────────────────────────────────────────────────────────────

interface BulkToolbarProps {
  count: number;
  working: boolean;
  walkActive: boolean;
  result: BulkDismissResult | null;
  onDismiss: () => void;
  onMoveToWalk: () => void;
  onClear: () => void;
}

function BulkToolbar({
  count,
  working,
  walkActive,
  result,
  onDismiss,
  onMoveToWalk,
  onClear,
}: BulkToolbarProps): JSX.Element {
  return (
    <div
      data-testid="escalations-bulk-toolbar"
      className="sticky top-0 z-10 mb-4 flex flex-wrap items-center gap-3 rounded-md border border-neutral-200 bg-white px-4 py-2 shadow-sm dark:border-neutral-700 dark:bg-neutral-900"
    >
      <span
        data-testid="escalations-bulk-count"
        className="text-sm font-medium text-neutral-700 dark:text-neutral-200"
      >
        {count} selected
      </span>
      <button
        type="button"
        data-testid="escalations-bulk-clear"
        onClick={onClear}
        disabled={working}
        className="text-xs text-neutral-500 underline hover:text-neutral-800 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:text-neutral-200"
      >
        Clear
      </button>
      <div className="ml-auto flex items-center gap-2">
        <button
          type="button"
          data-testid="escalations-bulk-dismiss"
          onClick={onDismiss}
          disabled={working}
          className="rounded-md bg-neutral-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-neutral-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
        >
          {working ? 'Working\u2026' : 'Dismiss'}
        </button>
        {walkActive && (
          <button
            type="button"
            data-testid="escalations-bulk-move-to-walk"
            onClick={onMoveToWalk}
            disabled={working}
            className="rounded-md border border-neutral-300 px-3 py-1.5 text-xs font-medium text-neutral-800 hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-neutral-600 dark:text-neutral-200 dark:hover:bg-neutral-800"
          >
            Move to Gemba walk
          </button>
        )}
        {!walkActive && (
          <button
            type="button"
            data-testid="escalations-bulk-move-to-walk"
            disabled
            title="Start a Gemba walk to move escalations to the agenda"
            className="cursor-not-allowed rounded-md border border-neutral-200 px-3 py-1.5 text-xs font-medium text-neutral-400 opacity-50 dark:border-neutral-700"
          >
            Move to Gemba walk
          </button>
        )}
      </div>

      {/* Bulk operation result summary */}
      {result && (
        <div
          data-testid="escalations-bulk-result"
          className="w-full text-xs"
        >
          {result.failed.length === 0 ? (
            <span className="text-emerald-700 dark:text-emerald-400">
              {result.succeeded} dismissed.
            </span>
          ) : (
            <span className="text-rose-700 dark:text-rose-400">
              {result.succeeded} dismissed, {result.failed.length} failed:{' '}
              {result.failed.map((f) => f.reason).join('; ')}
            </span>
          )}
        </div>
      )}
    </div>
  );
}

// ── EscalationCard ───────────────────────────────────────────────────────────

interface EscalationCardProps {
  escalation: EscalationRequest;
  selected: boolean;
  onToggleSelect: (id: string) => void;
}

function EscalationCard({ escalation, selected, onToggleSelect }: EscalationCardProps) {
  const [resolveOpen, setResolveOpen] = useState(false);
  const [handoffOpen, setHandoffOpen] = useState(false);
  const Icon = KIND_ICON[escalation.source] ?? AlertTriangle;
  const sev = severityFor(escalation);
  const sevMeta = SEVERITY_META[sev];

  return (
    <div
      data-testid={`escalation-card-${escalation.id}`}
      data-kind={escalation.source}
      data-severity={sev}
      className={
        'rounded-md border p-4 shadow-sm ' +
        (selected
          ? 'border-neutral-500 bg-neutral-50 dark:border-neutral-400 dark:bg-neutral-900'
          : 'border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-950')
      }
    >
      <div className="mb-2 flex items-start justify-between gap-3">
        {/* Checkbox occupies top-left; flex-shrink-0 prevents layout shift */}
        <div className="flex min-w-0 items-center gap-2">
          <input
            type="checkbox"
            aria-label={`Select escalation: ${escalation.title}`}
            data-testid={`escalation-card-${escalation.id}-checkbox`}
            checked={selected}
            onChange={() => onToggleSelect(escalation.id)}
            className="h-4 w-4 shrink-0 cursor-pointer accent-neutral-800 dark:accent-neutral-200"
          />
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
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setHandoffOpen(true)}
            data-testid={`escalation-card-${escalation.id}-handoff`}
            className="rounded-md border border-neutral-300 px-3 py-1.5 text-xs font-medium text-neutral-700 hover:bg-neutral-100 dark:border-neutral-600 dark:text-neutral-300 dark:hover:bg-neutral-800"
          >
            Hand-off
          </button>
          <button
            type="button"
            onClick={() => setResolveOpen(true)}
            data-testid={`escalation-card-${escalation.id}-resolve`}
            className="rounded-md bg-neutral-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
          >
            Resolve
          </button>
        </div>
      </div>

      {resolveOpen && (
        <ResolveModal
          escalation={escalation}
          onClose={() => setResolveOpen(false)}
        />
      )}

      {handoffOpen && (
        <HandoffModal
          escalation={escalation}
          onClose={() => setHandoffOpen(false)}
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

// ── HandoffModal ─────────────────────────────────────────────────────────────
//
// Opens a persona picker. On Confirm, POSTs to /api/consults with:
//   - persona_id: the chosen persona
//   - skill_id: HANDOFF_SKILL_ID (stubbed — see NOTE above)
//   - workspace: HANDOFF_WORKSPACE
//   - raw_input: { title, prompt, assignment_id } from the escalation
//   - context_chunks: a synthesized context string
//
// The escalation is NOT resolved — the card stays in the inbox until the
// operator comes back and hits Resolve once the consult yields a decision.

interface HandoffModalProps {
  escalation: EscalationRequest;
  onClose: () => void;
}

function HandoffModal({ escalation, onClose }: HandoffModalProps): JSX.Element {
  const personas = DEFAULT_PERSONAS;
  const [personaId, setPersonaId] = useState(personas[0]?.id ?? '');
  const [succeeded, setSucceeded] = useState(false);

  const handoff = useMutation<ConsultSummary, Error, string>({
    mutationFn: (pid) => {
      const context = [
        escalation.title,
        escalation.prompt ? escalation.prompt.trim() : '',
      ]
        .filter(Boolean)
        .join('\n\n');
      return createConsult(
        {
          persona_id: pid,
          skill_id: HANDOFF_SKILL_ID,
          workspace: HANDOFF_WORKSPACE,
          raw_input: {
            title: escalation.title,
            prompt: escalation.prompt ?? '',
            assignment_id: escalation.assignment_id ?? null,
            escalation_id: escalation.id,
          },
          context_chunks: context ? [context] : [],
          // spawn=true: the operator wants a real consult session, not
          // just a prompt-composition dry-run.
          spawn: true,
        },
        freshNonce(),
      );
    },
    onSuccess: () => {
      setSucceeded(true);
    },
  });

  // Close-on-Escape; mirrors ResolveModal. Don't close mid-flight.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !handoff.isPending) onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, handoff.isPending]);

  const submit = () => {
    if (!personaId) return;
    handoff.mutate(personaId);
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      data-testid="escalation-handoff-modal"
      data-escalation-id={escalation.id}
      className="fixed inset-0 z-50 flex items-center justify-center bg-neutral-900/40 p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget && !handoff.isPending) onClose();
      }}
    >
      <div className="flex w-full max-w-sm flex-col rounded-lg border border-neutral-200 bg-white shadow-xl dark:border-neutral-800 dark:bg-neutral-950">
        <header className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <h2 className="text-sm font-semibold">Hand off to persona</h2>
          <p className="truncate text-xs text-neutral-500 dark:text-neutral-400">
            {escalation.title}
          </p>
        </header>

        {succeeded ? (
          <div className="px-4 py-4">
            <p
              data-testid="escalation-handoff-success"
              className="rounded-md border border-emerald-300 bg-emerald-50 px-3 py-2 text-xs text-emerald-800 dark:border-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-200"
            >
              Handed off. The escalation stays open until you Resolve it.
            </p>
            <div className="mt-3 flex justify-end">
              <button
                type="button"
                data-testid="escalation-handoff-close"
                onClick={onClose}
                className="rounded border border-neutral-300 px-3 py-1 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
              >
                Close
              </button>
            </div>
          </div>
        ) : (
          <>
            <div className="px-4 py-3">
              <label
                htmlFor="escalation-handoff-persona"
                className="mb-1 block text-[11px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400"
              >
                Persona
              </label>
              <select
                id="escalation-handoff-persona"
                data-testid="escalation-handoff-persona"
                value={personaId}
                onChange={(e) => setPersonaId(e.target.value)}
                disabled={handoff.isPending}
                className="w-full rounded-md border border-neutral-300 px-2 py-1.5 text-xs dark:border-neutral-700 dark:bg-neutral-900"
              >
                {personas.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            {handoff.error && (
              <div
                data-testid="escalation-handoff-error"
                className="mx-4 mb-3 rounded-md border border-rose-300 bg-rose-50 px-3 py-1.5 text-[11px] text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
              >
                {handoff.error.message}
              </div>
            )}

            <footer className="flex items-center justify-end gap-2 border-t border-neutral-200 px-4 py-3 dark:border-neutral-800">
              <button
                type="button"
                data-testid="escalation-handoff-cancel"
                onClick={onClose}
                disabled={handoff.isPending}
                className="rounded border border-neutral-300 px-3 py-1 text-xs hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-900"
              >
                Cancel
              </button>
              <button
                type="button"
                data-testid="escalation-handoff-confirm"
                onClick={submit}
                disabled={handoff.isPending || !personaId}
                className="rounded bg-sky-600 px-3 py-1 text-xs font-semibold text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {handoff.isPending ? 'Handing off\u2026' : 'Confirm'}
              </button>
            </footer>
          </>
        )}
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
