// RecommendOrderDrawer (gm-twp2). The /plan-side surface for the
// PM persona's epic_order skill: assembles an EpicOrderInput from
// the current ready beads, POSTs to /api/consults, polls the
// resulting consult for ValidatedLines as they trickle in, and
// surfaces per-recommendation Apply buttons that hit POST
// /api/consults/{id}/apply/{idx}.
//
// Lives as a controlled overlay rather than a route so the operator
// can summon it from CoachPage without losing their grid scroll.
// The bead's spec calls for "incremental as MCP-tool emissions
// land" — today the SPA polls /api/consults/{id} every 1.5s; SSE
// push lands in a follow-up bead when the events.SkillOutputEmitted
// fan-out reaches the SPA.

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Sparkles, X, AlertTriangle, CheckCircle2, Loader2 } from 'lucide-react';
import {
  applyConsult,
  createConsult,
  freshNonce,
  getConsult,
  type ApplyConsultResponse,
  type ConsultDetail,
  type ConsultSummary,
} from '@/api/consults';
import { ApiError } from '@/api/client';
import type { PlannerReadyBead } from '@/api/planner';
import { cn } from '@/lib/utils';

// EpicOrderInput shape lives server-side in
// internal/skills/epic_order/types.go. Mirroring just the slots
// the SPA populates; missing fields default per-skill (the
// dispatcher's Skill.ValidateInput is the authoritative gate).
interface EpicOrderInput {
  workspace: string;
  workspace_name: string;
  as_of: string;
  candidate_epics: Array<{
    epic_id: string;
    title: string;
    ui_state: string;
    summary?: string;
  }>;
  constraints: Record<string, never>;
  guidance?: string;
}

// PM persona id + epic_order skill id are the v1 default; the
// drawer is bound to this pair until a skill picker lands.
const PERSONA_ID = 'project-manager';
const SKILL_ID = 'epic_order';
const POLL_MS = 1500;

export interface RecommendOrderDrawerProps {
  open: boolean;
  onClose: () => void;
  workspace: string;
  workspaceName?: string;
  readyBeads: PlannerReadyBead[];
}

export function RecommendOrderDrawer(props: RecommendOrderDrawerProps) {
  const { open, onClose, workspace, workspaceName, readyBeads } = props;

  // Consult lifecycle: null until the operator clicks Submit, then
  // the freshly-created summary, then we poll detail.
  const [consultID, setConsultID] = useState<string | null>(null);
  const [submitErr, setSubmitErr] = useState<string | null>(null);
  const queryClient = useQueryClient();

  // Reset consult state every time the drawer closes so a re-open
  // starts fresh. The /api/consults/{id} record persists in the
  // dispatcher; the operator can re-find it via /insights/personas.
  useEffect(() => {
    if (!open) {
      setConsultID(null);
      setSubmitErr(null);
    }
  }, [open]);

  const submitMut = useMutation<ConsultSummary, Error, void>({
    mutationFn: async () => {
      const input: EpicOrderInput = {
        workspace,
        workspace_name: workspaceName ?? workspace,
        as_of: new Date().toISOString(),
        candidate_epics: readyBeads.map((b) => ({
          epic_id: b.id,
          title: b.title,
          ui_state: b.state_category,
        })),
        constraints: {},
      };
      return createConsult(
        {
          persona_id: PERSONA_ID,
          skill_id: SKILL_ID,
          workspace,
          raw_input: input,
        },
        freshNonce(),
      );
    },
    onSuccess: (summary) => {
      setSubmitErr(null);
      setConsultID(summary.id);
      // Surface in /insights/personas without waiting for its 5s poll.
      queryClient.invalidateQueries({ queryKey: ['consults'] });
    },
    onError: (err) => {
      setSubmitErr(err.message);
    },
  });

  const detailQ = useQuery<ConsultDetail>({
    queryKey: ['consult', consultID],
    queryFn: () => getConsult(consultID!),
    enabled: open && consultID !== null,
    refetchInterval: (q) => {
      // Stop polling once the consult lands in a terminal state.
      // The bridge tail might still drop a late line into a
      // technically-running consult, but the POST/Finish flow caps
      // it; once status flips to completed/failed we're done.
      const data = q.state.data;
      if (!data) return POLL_MS;
      return data.status === 'running' ? POLL_MS : false;
    },
  });

  const detail = detailQ.data;
  const lines = detail?.validated_lines ?? [];
  const appliedSet = useMemo(() => new Set(detail?.applied_idx ?? []), [detail?.applied_idx]);

  if (!open) return null;

  const canSubmit = !submitMut.isPending && consultID === null && readyBeads.length > 0;

  return (
    <div
      className="fixed inset-0 z-40 flex justify-end bg-black/30"
      data-testid="recommend-order-drawer"
      onClick={onClose}
    >
      <div
        className="flex h-full w-[min(640px,90vw)] flex-col bg-white shadow-xl dark:bg-neutral-950"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="flex items-center justify-between border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
          <div>
            <h2 className="flex items-center gap-2 text-lg font-semibold tracking-tight">
              <Sparkles className="h-4 w-4" aria-hidden />
              Recommend order
            </h2>
            <p className="text-xs text-neutral-500 dark:text-neutral-400">
              Project Manager · epic_order
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            data-testid="recommend-order-close"
            className="rounded p-1 text-neutral-500 hover:bg-neutral-100 dark:hover:bg-neutral-800"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="min-h-0 flex-1 overflow-auto px-6 py-4">
          {consultID === null ? (
            <SubmitPanel
              canSubmit={canSubmit}
              busy={submitMut.isPending}
              readyBeadCount={readyBeads.length}
              err={submitErr}
              onSubmit={() => submitMut.mutate()}
            />
          ) : (
            <ResultsPanel
              detail={detail}
              lines={lines}
              consultID={consultID}
              appliedSet={appliedSet}
              error={detailQ.error}
            />
          )}
        </div>
      </div>
    </div>
  );
}

interface SubmitPanelProps {
  canSubmit: boolean;
  busy: boolean;
  readyBeadCount: number;
  err: string | null;
  onSubmit: () => void;
}

function SubmitPanel({ canSubmit, busy, readyBeadCount, err, onSubmit }: SubmitPanelProps) {
  return (
    <div className="space-y-4">
      <p className="text-sm text-neutral-700 dark:text-neutral-200">
        Asks the PM persona to rank the {readyBeadCount} ready bead{readyBeadCount === 1 ? '' : 's'} from the planner's coach view.
      </p>
      <p className="text-xs text-neutral-500 dark:text-neutral-400">
        A Claude Code session is spawned with the consult's composed prompt
        in CLAUDE.md. As the model emits structured recommendations they
        appear below; click <em>Apply</em> on a row to record your
        confirmation in the audit log.
      </p>
      {err && (
        <div
          className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
          data-testid="recommend-order-submit-error"
        >
          {err}
        </div>
      )}
      {readyBeadCount === 0 && (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
          No ready beads to rank. Add one (or remove a soft-block) and
          re-open this drawer.
        </div>
      )}
      <button
        type="button"
        onClick={onSubmit}
        disabled={!canSubmit}
        data-testid="recommend-order-submit"
        className={cn(
          'inline-flex items-center gap-2 rounded-md bg-sky-600 px-4 py-2 text-sm font-medium text-white',
          'disabled:cursor-not-allowed disabled:opacity-40 hover:bg-sky-700',
        )}
      >
        {busy && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
        Submit consult
      </button>
    </div>
  );
}

interface ResultsPanelProps {
  detail: ConsultDetail | undefined;
  lines: unknown[];
  consultID: string;
  appliedSet: Set<number>;
  error: unknown;
}

function ResultsPanel({ detail, lines, consultID, appliedSet, error }: ResultsPanelProps) {
  return (
    <div className="space-y-4" data-testid="recommend-order-results">
      <div className="flex items-center justify-between text-xs text-neutral-500">
        <span className="font-mono">{consultID}</span>
        {detail && <StatusPill status={detail.status} />}
      </div>
      {error ? (
        <div className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
          {(error as Error).message}
        </div>
      ) : null}
      {lines.length === 0 ? (
        <div
          className="rounded-md border border-dashed border-neutral-300 px-3 py-6 text-center text-sm text-neutral-500 dark:border-neutral-700"
          data-testid="recommend-order-waiting"
        >
          Waiting for the persona to emit its first line…
        </div>
      ) : (
        <ol className="space-y-2">
          {lines.map((line, i) => (
            <LineRow
              key={i}
              line={line}
              idx={i}
              consultID={consultID}
              alreadyApplied={appliedSet.has(i)}
            />
          ))}
        </ol>
      )}
    </div>
  );
}

interface LineRowProps {
  line: unknown;
  idx: number;
  consultID: string;
  alreadyApplied: boolean;
}

function LineRow({ line, idx, consultID, alreadyApplied }: LineRowProps) {
  const [err, setErr] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const applyMut = useMutation<ApplyConsultResponse, Error, void>({
    mutationFn: () => applyConsult(consultID, idx, freshNonce()),
    onSuccess: () => {
      setErr(null);
      // Re-fetch the consult detail so applied_idx + the row's
      // "applied" badge update without waiting for the next poll.
      queryClient.invalidateQueries({ queryKey: ['consult', consultID] });
    },
    onError: (e) => {
      // 409 from the dispatcher = already applied (e.g. operator
      // double-clicked). Surface the message so the operator knows
      // the apply state is preserved either way.
      if (e instanceof ApiError && e.status === 409) {
        setErr('already applied');
      } else {
        setErr(e.message);
      }
    },
  });

  const lineKind = typeOf(line);
  const isRecommendation = lineKind === 'recommendation';
  const summary = summarize(line);

  return (
    <li
      className={cn(
        'rounded-md border px-3 py-2',
        alreadyApplied
          ? 'border-emerald-300 bg-emerald-50 dark:border-emerald-900/60 dark:bg-emerald-950/20'
          : 'border-neutral-200 dark:border-neutral-800',
      )}
      data-testid={`recommend-order-line-${idx}`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-xs">
            <span className="font-mono text-neutral-500">#{idx}</span>
            <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
              {lineKind}
            </span>
            {alreadyApplied && (
              <span
                className="flex items-center gap-1 rounded bg-emerald-100 px-1.5 py-0.5 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300"
                data-testid="recommend-order-applied-badge"
              >
                <CheckCircle2 className="h-3 w-3" aria-hidden />
                applied
              </span>
            )}
          </div>
          <p className="mt-1 truncate text-sm text-neutral-800 dark:text-neutral-200">{summary}</p>
        </div>
        {isRecommendation && (
          <button
            type="button"
            onClick={() => applyMut.mutate()}
            disabled={alreadyApplied || applyMut.isPending}
            data-testid={`recommend-order-apply-${idx}`}
            className={cn(
              'shrink-0 rounded-md border border-sky-600 px-3 py-1 text-xs font-medium text-sky-700 hover:bg-sky-50',
              'disabled:cursor-not-allowed disabled:border-neutral-300 disabled:text-neutral-400 disabled:hover:bg-transparent',
              'dark:border-sky-500 dark:text-sky-400 dark:hover:bg-sky-950/30',
            )}
          >
            {applyMut.isPending ? '…' : 'Apply'}
          </button>
        )}
      </div>
      {err && (
        <p
          className="mt-1 flex items-center gap-1 text-xs text-rose-600 dark:text-rose-400"
          data-testid={`recommend-order-error-${idx}`}
        >
          <AlertTriangle className="h-3 w-3" aria-hidden />
          {err}
        </p>
      )}
    </li>
  );
}

function StatusPill({ status }: { status: ConsultDetail['status'] }) {
  const cls =
    status === 'running'
      ? 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300'
      : status === 'failed'
        ? 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
        : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
  return <span className={cn('rounded px-1.5 py-0.5 text-xs', cls)}>{status}</span>;
}

// typeOf reads the `type` discriminator off a validated line. The
// epic_order skill stamps "strategy" / "recommendation" / "warning"
// / "deferred" / "summary"; other skills set their own. Falls back
// to "?" when the line lacks the field (defensive; the dispatcher's
// Skill.ValidateOutputLine MUST require it).
function typeOf(line: unknown): string {
  if (line && typeof line === 'object' && 'type' in line) {
    const t = (line as Record<string, unknown>).type;
    if (typeof t === 'string') return t;
  }
  return '?';
}

// summarize picks the most-readable single-line label per known
// shape. Uses field names common across the epic_order line types
// (rationale > reasoning > detail > the raw stringified body).
function summarize(line: unknown): string {
  if (!line || typeof line !== 'object') return JSON.stringify(line);
  const obj = line as Record<string, unknown>;
  for (const key of ['rationale', 'reasoning', 'detail', 'note']) {
    if (typeof obj[key] === 'string' && (obj[key] as string).length > 0) {
      return obj[key] as string;
    }
  }
  return JSON.stringify(line).slice(0, 200);
}
