// RecommendOrderDetail (gm-root.22.7) — RHP detail tab replacing
// RecommendOrderDrawer (gm-twp2). Registers kind 'consult:recommend_order'
// with the RHP detail-content registry on mount. Content mirrors the
// drawer's body: submit panel → polling results panel with per-line
// Apply buttons.
//
// Granular kind ('consult:recommend_order') is chosen over the shared
// 'persona-consult' alternative because different consult flavors should
// stack as separate tabs (design doc § "Kind-replace / kind-stack rule").
//
// The drawer's overlay chrome (fixed inset, close button, header) is
// stripped — the RHP tab rail owns that surface now. The content area
// below assumes it is already inside an overflow-auto scroll container
// (RhpShell provides this).
//
// The 'id' passed to render() is the workspace identifier. SprintsPage
// supplies it from planner.data.ready_beads[0].repository so the same
// identifier the drawer used today is preserved.

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Sparkles, AlertTriangle, CheckCircle2, Loader2 } from 'lucide-react';
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
import { getCoach } from '@/api/planner';
import type { PlannerReadyBead, PlannerCoachResponse } from '@/api/planner';
import { cn } from '@/lib/utils';
import { useRegisterDetailContent } from '../RhpDetail';

// EpicOrderInput mirrors the server-side shape the drawer used.
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

const PERSONA_ID = 'project-manager';
const SKILL_ID = 'epic_order';
const POLL_MS = 1500;
const PLANNER_POLL_MS = 30_000;

export const RECOMMEND_ORDER_KIND = 'consult:recommend_order';

// Module-scope render function so useRegisterDetailContent's dependency
// on reg.render is stable across re-renders — prevents the registration
// useEffect from re-firing (and causing a bumpDetailReg loop) every time
// RecommendOrderDetailRegistration's parent re-renders.
function renderRecommendOrderDetail(id: string) {
  return <RecommendOrderDetailBody workspace={id} />;
}

// ── Registration component ────────────────────────────────────────────
//
// Mount once (e.g. in AppShell or SprintsPage) to wire the kind into
// the RHP registry so popDetail({kind: RECOMMEND_ORDER_KIND, …}) can
// resolve content + icon.

export function RecommendOrderDetailRegistration() {
  useRegisterDetailContent({
    kind: RECOMMEND_ORDER_KIND,
    icon: Sparkles,
    label: 'Recommend order',
    render: renderRecommendOrderDetail,
  });
  return null;
}

// ── Body ────────────────────────────────────────────────────────────

interface RecommendOrderDetailBodyProps {
  workspace: string;
}

// Exported for direct testing. The RHP registry calls this via the
// registration's render callback; tests can render it standalone.
export function RecommendOrderDetailBody({ workspace }: RecommendOrderDetailBodyProps) {
  const planner = useQuery<PlannerCoachResponse>({
    queryKey: ['planner', 'coach'],
    queryFn: getCoach,
    refetchInterval: PLANNER_POLL_MS,
    staleTime: PLANNER_POLL_MS / 2,
  });

  const readyBeads: PlannerReadyBead[] = planner.data?.ready_beads ?? [];
  const workspaceName = workspace;

  const [consultID, setConsultID] = useState<string | null>(null);
  const [submitErr, setSubmitErr] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const submitMut = useMutation<ConsultSummary, Error, void>({
    mutationFn: async () => {
      const input: EpicOrderInput = {
        workspace,
        workspace_name: workspaceName,
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
      queryClient.invalidateQueries({ queryKey: ['consults'] });
    },
    onError: (err) => {
      setSubmitErr(err.message);
    },
  });

  const detailQ = useQuery<ConsultDetail>({
    queryKey: ['consult', consultID],
    queryFn: () => getConsult(consultID!),
    enabled: consultID !== null,
    refetchInterval: (q) => {
      const data = q.state.data;
      if (!data) return POLL_MS;
      return data.status === 'running' ? POLL_MS : false;
    },
  });

  const detail = detailQ.data;
  const lines = detail?.validated_lines ?? [];
  const appliedSet = useMemo(() => new Set(detail?.applied_idx ?? []), [detail?.applied_idx]);

  const canSubmit = !submitMut.isPending && consultID === null && readyBeads.length > 0;

  if (planner.isLoading) {
    return (
      <div
        className="px-4 py-6 text-sm text-neutral-500"
        data-testid="recommend-order-detail-planner-loading"
      >
        Loading planner snapshot…
      </div>
    );
  }

  if (planner.isError) {
    return (
      <div
        className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
        data-testid="recommend-order-detail-planner-error"
      >
        Planner unavailable: {(planner.error as Error).message}
      </div>
    );
  }

  return (
    <div className="space-y-4 px-4 py-4" data-testid="recommend-order-detail">
      <div>
        <h3 className="flex items-center gap-2 text-sm font-semibold tracking-tight">
          <Sparkles className="h-4 w-4" aria-hidden />
          Recommend order
        </h3>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Project Manager · epic_order
        </p>
      </div>

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
        Asks the PM persona to rank the {readyBeadCount} ready bead
        {readyBeadCount === 1 ? '' : 's'} from the planner's coach view.
      </p>
      <p className="text-xs text-neutral-500 dark:text-neutral-400">
        A Claude Code session is spawned with the consult's composed prompt in
        CLAUDE.md. As the model emits structured recommendations they appear
        below; click <em>Apply</em> on a row to record your confirmation in the
        audit log.
      </p>
      {err && (
        <div
          className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
          data-testid="recommend-order-detail-submit-error"
        >
          {err}
        </div>
      )}
      {readyBeadCount === 0 && (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
          No ready beads to rank. Add one (or remove a soft-block) and re-open
          this panel.
        </div>
      )}
      <button
        type="button"
        onClick={onSubmit}
        disabled={!canSubmit}
        data-testid="recommend-order-detail-submit"
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
    <div className="space-y-4" data-testid="recommend-order-detail-results">
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
          data-testid="recommend-order-detail-waiting"
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
      queryClient.invalidateQueries({ queryKey: ['consult', consultID] });
    },
    onError: (e) => {
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
      data-testid={`recommend-order-detail-line-${idx}`}
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
                data-testid="recommend-order-detail-applied-badge"
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
            data-testid={`recommend-order-detail-apply-${idx}`}
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
          data-testid={`recommend-order-detail-error-${idx}`}
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

function typeOf(line: unknown): string {
  if (line && typeof line === 'object' && 'type' in line) {
    const t = (line as Record<string, unknown>).type;
    if (typeof t === 'string') return t;
  }
  return '?';
}

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

// Re-export for consumers that want to import the useEffect-free version.
export { useRegisterDetailContent };
