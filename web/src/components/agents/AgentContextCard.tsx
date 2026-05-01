// AgentContextCard (gm-s47n.6.7). One operator-facing card showing
// the full operational context for a live session: agent identity,
// workspace (repo / branch / worktree / isolation), top concepts,
// pressure / drift / time-on-task. Intended for the coach-mode
// strip and any other surface that needs a compact at-a-glance
// view of "who's loaded with what."
//
// Pure component — every piece of data comes in via props. Conflict
// markers and an optional per-bead affinity overlay are caller-
// supplied so the same card renders inside the dispatch grid
// (where hover state changes its appearance) and inside a static
// roster view.

import { useMemo } from 'react';
import { AlertTriangle, Box, Clock, Crosshair, GitBranch, Gauge, Sparkles, Users } from 'lucide-react';
import type {
  PlannerOperationalContext,
  PlannerRunway,
  PlannerSessionIntent,
} from '@/api/planner';
import { cn } from '@/lib/utils';
import { ParallelismPill } from '@/components/sessions/ParallelismPill';
import { useSessionParallelism } from '@/hooks/useSessionParallelism';

export interface AgentContextCardProps {
  ctx: PlannerOperationalContext;
  // When true, render with the conflict highlight (red border).
  // Used by the coach grid to surface "this session is in
  // workspace-conflict with the hovered bead."
  inConflict?: boolean;
  // Optional per-card affinity overlay. Coach grid passes the
  // hovered bead's affinity score for this session; static
  // surfaces leave it null.
  affinityForHovered?: { beadId: string; combined: number } | null;
  // Override the data-testid prefix. Defaults to the session id;
  // tests asserting on the card without a stable session id can
  // pin a custom value.
  testidPrefix?: string;
  // intent renders the operator-pinned focus directive
  // (gm-v5z2.9). Optional — surfaces only when a focus is active.
  intent?: PlannerSessionIntent | null;
  // runway renders the runway estimate (gm-v5z2.4) so the
  // operator sees what bead-size bucket this session can
  // realistically take next. Optional — older callers leave it
  // undefined.
  runway?: PlannerRunway | null;
  // onSetFocus opens the focus dialog when present. The card
  // renders a "Set focus" affordance only when the callback is
  // bound — read-only surfaces (static roster) leave it
  // undefined.
  onSetFocus?: () => void;
}

export function AgentContextCard({
  ctx,
  inConflict,
  affinityForHovered,
  testidPrefix,
  intent,
  runway,
  onSetFocus,
}: AgentContextCardProps) {
  const sid = ctx.session?.id ?? '';
  const baseTestId = testidPrefix ?? `agent-context-${sid || 'unknown'}`;

  const topConcepts = useMemo(() => {
    const m = ctx.profile?.concepts;
    if (!m) return [] as { tag: string; weight: number }[];
    return Object.entries(m)
      .map(([tag, weight]) => ({ tag, weight }))
      .sort((a, b) => b.weight - a.weight)
      .slice(0, 5);
  }, [ctx.profile?.concepts]);

  return (
    <div
      data-testid={baseTestId}
      data-in-conflict={inConflict || undefined}
      className={cn(
        'min-w-[260px] shrink-0 rounded-md border p-3 text-xs transition-colors',
        inConflict
          ? 'border-rose-400 bg-rose-50 dark:border-rose-700 dark:bg-rose-950/40'
          : 'border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900'
      )}
    >
      <CardHeader ctx={ctx} />
      <CardWorkspace ctx={ctx} />
      <CardConcepts top={topConcepts} testid={`${baseTestId}-concepts`} />
      <CardHealth ctx={ctx} testid={baseTestId} />
      <CardIntent intent={intent ?? null} onSetFocus={onSetFocus} testid={`${baseTestId}-intent`} />
      <CardRunway runway={runway ?? null} testid={`${baseTestId}-runway`} />
      {affinityForHovered ? (
        <AffinityBadge
          beadId={affinityForHovered.beadId}
          combined={affinityForHovered.combined}
          testid={`${baseTestId}-affinity`}
        />
      ) : null}
    </div>
  );
}

function CardHeader({ ctx }: { ctx: PlannerOperationalContext }) {
  const sid = ctx.session?.id ?? '';
  const par = useSessionParallelism(sid);
  return (
    <>
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="flex items-center gap-1 font-mono text-[11px] text-neutral-500">
          <Users className="h-3 w-3" />
          {ctx.agent?.id ?? '(anon)'}
        </span>
        <span className="flex items-center gap-1.5">
          {par ? (
            <ParallelismPill
              inFlight={par.in_flight}
              maxParallel={par.max_parallel}
              intraParallel={par.max_parallel > 1}
              testid={`agent-context-${sid || 'unknown'}-pill`}
            />
          ) : null}
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] dark:bg-neutral-800">
            {ctx.session?.status ?? 'unknown'}
          </span>
        </span>
      </div>
      <div className="mb-1 truncate font-medium">{ctx.agent?.name ?? ctx.session?.id ?? 'Unknown session'}</div>
    </>
  );
}

function CardWorkspace({ ctx }: { ctx: PlannerOperationalContext }) {
  const ws = ctx.workspace;
  const status = ctx.scope_status;
  if (!ws && !status) return null;
  // Worktree path can be long; truncate from the front so the
  // last segment (the worktree name) stays visible.
  const path = ws?.worktree_path ?? '';
  const showPath = path !== '';
  const isolationFlags = formatIsolation(ws?.isolation);
  return (
    <div className="mb-2 space-y-1 text-[11px] text-neutral-500">
      {ws ? (
        <div className="flex items-center gap-1">
          <GitBranch className="h-3 w-3" />
          <span className="truncate">
            {ws.repository}/{ws.branch}
          </span>
        </div>
      ) : null}
      {showPath ? (
        <div className="flex items-center gap-1">
          <Box className="h-3 w-3" />
          <span className="truncate font-mono" title={path}>
            {leftEllipsize(path, 30)}
          </span>
        </div>
      ) : null}
      {ws ? (
        <div className="flex items-center gap-1">
          <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] dark:bg-neutral-800">
            {ws.kind}
          </span>
          {isolationFlags ? (
            <span className="font-mono text-[10px] text-neutral-500">{isolationFlags}</span>
          ) : null}
        </div>
      ) : null}
      <ScopeStatusPills status={status} />
    </div>
  );
}

function ScopeStatusPills({ status }: { status: PlannerOperationalContext['scope_status'] }) {
  if (!status) return null;
  const pills = buildScopePills(status);
  if (pills.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1 pt-0.5" data-testid="scope-status-pills">
      {pills.map((pill) => (
        <span
          key={pill.key}
          title={pill.title}
          className={cn(
            'rounded px-1.5 py-0.5 font-mono text-[10px]',
            scopePillTone(pill.tone)
          )}
        >
          {pill.label}
        </span>
      ))}
    </div>
  );
}

function CardConcepts({
  top,
  testid,
}: {
  top: { tag: string; weight: number }[];
  testid: string;
}) {
  if (top.length === 0) return null;
  return (
    <div data-testid={testid} className="mb-2 flex flex-wrap gap-1">
      {top.map((c) => (
        <span
          key={c.tag}
          className="rounded bg-sky-50 px-1.5 py-0.5 font-mono text-[10px] text-sky-700 dark:bg-sky-950/40 dark:text-sky-300"
        >
          {c.tag}
        </span>
      ))}
    </div>
  );
}

function CardHealth({
  ctx,
  testid,
}: {
  ctx: PlannerOperationalContext;
  testid: string;
}) {
  if (!ctx.health) return null;
  return (
    <div
      data-testid={`${testid}-health`}
      className="grid grid-cols-3 gap-1 text-[10px] text-neutral-500"
    >
      <Stat icon={<Sparkles className="h-3 w-3" />} value={ctx.health.context_pressure} />
      <Stat icon={<AlertTriangle className="h-3 w-3" />} value={ctx.health.concept_drift} />
      <Stat
        icon={<Clock className="h-3 w-3" />}
        value={ctx.health.time_on_task_ns / 1e9 / 60}
        suffix="m"
        precision={0}
      />
    </div>
  );
}

function Stat({
  icon,
  value,
  suffix,
  precision = 2,
}: {
  icon: React.ReactNode;
  value: number;
  suffix?: string;
  precision?: number;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      {icon}
      <span className="font-mono">
        {value.toFixed(precision)}
        {suffix ?? ''}
      </span>
    </span>
  );
}

function CardIntent({
  intent,
  onSetFocus,
  testid,
}: {
  intent: PlannerSessionIntent | null;
  onSetFocus?: () => void;
  testid: string;
}) {
  // No intent set + no callback to set one → render nothing.
  // (Static surfaces should not show an empty intent line.)
  if (!intent && !onSetFocus) return null;

  const summary = intent ? intentSummary(intent) : null;
  const demote = intent?.demotion_factor ?? 0.4;
  return (
    <div
      data-testid={testid}
      className="mt-2 flex items-center justify-between gap-2 rounded border border-violet-200 bg-violet-50 px-2 py-1 text-[11px] text-violet-800 dark:border-violet-900/40 dark:bg-violet-950/30 dark:text-violet-200"
    >
      <span className="inline-flex items-center gap-1">
        <Crosshair className="h-3 w-3" aria-hidden />
        {summary ? (
          <>
            <span className="font-mono">{summary}</span>
            <span className="font-mono text-[10px] opacity-70">×{demote.toFixed(2)}</span>
          </>
        ) : (
          <span className="opacity-70">no focus pinned</span>
        )}
      </span>
      {onSetFocus ? (
        <button
          type="button"
          onClick={onSetFocus}
          data-testid={`${testid}-set-focus`}
          className="rounded border border-violet-300 px-1.5 py-0.5 font-mono text-[10px] hover:bg-violet-100 dark:border-violet-700 dark:hover:bg-violet-900/40"
        >
          {intent ? 'edit' : 'set focus'}
        </button>
      ) : null}
    </div>
  );
}

function intentSummary(i: PlannerSessionIntent): string {
  if (i.epic_id) return `epic=${i.epic_id}`;
  if (i.label) return `label=${i.label}`;
  if (i.bead_id_regex) return `regex=${i.bead_id_regex}`;
  return '(empty)';
}

function CardRunway({
  runway,
  testid,
}: {
  runway: PlannerRunway | null;
  testid: string;
}) {
  if (!runway) return null;
  return (
    <div
      data-testid={testid}
      className={cn(
        'mt-2 flex items-center justify-between gap-2 rounded border px-2 py-1 text-[11px]',
        runwayTone(runway.bucket)
      )}
    >
      <span className="inline-flex items-center gap-1">
        <Gauge className="h-3 w-3" aria-hidden />
        <span>runway: {runway.bucket}</span>
      </span>
      <span className="font-mono text-[10px] opacity-70">{runway.score.toFixed(2)}</span>
    </div>
  );
}

function runwayTone(bucket: PlannerRunway['bucket']): string {
  switch (bucket) {
    case 'large':
      return 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/40 dark:bg-emerald-950/30 dark:text-emerald-200';
    case 'medium':
      return 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-900/40 dark:bg-sky-950/30 dark:text-sky-200';
    default:
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200';
  }
}

function AffinityBadge({
  beadId,
  combined,
  testid,
}: {
  beadId: string;
  combined: number;
  testid: string;
}) {
  return (
    <div
      data-testid={testid}
      className="mt-2 rounded border border-emerald-200 bg-emerald-50 px-2 py-1 text-[11px] text-emerald-800 dark:border-emerald-900/40 dark:bg-emerald-950/30 dark:text-emerald-200"
    >
      affinity for {beadId}: {combined.toFixed(2)}
    </div>
  );
}

// IsolationFlags is the isolation subset of the workspace shape
// the strip renders. Re-declared here (rather than projected via
// a conditional type from PlannerOperationalContext) so TypeScript
// resolves it cleanly under strict mode.
interface IsolationFlags {
  fs_scoped?: boolean;
  net_isolated?: boolean;
  cpu_limited?: boolean;
  mem_limited?: boolean;
  snapshot_restore?: boolean;
}

// formatIsolation renders the isolation flags as a compact tag
// list ("fs+net+cpu") so a quick glance reveals what the kernel
// is enforcing. Returns "" when no flags are set.
function formatIsolation(iso?: IsolationFlags): string {
  if (!iso) return '';
  const flags: string[] = [];
  if (iso.fs_scoped) flags.push('fs');
  if (iso.net_isolated) flags.push('net');
  if (iso.cpu_limited) flags.push('cpu');
  if (iso.mem_limited) flags.push('mem');
  if (iso.snapshot_restore) flags.push('snap');
  return flags.join('+');
}

type ScopePillTone = 'good' | 'warn' | 'danger' | 'neutral';

interface ScopePill {
  key: string;
  label: string;
  tone: ScopePillTone;
  title?: string;
}

function buildScopePills(
  status: NonNullable<PlannerOperationalContext['scope_status']>
): ScopePill[] {
  const pills: ScopePill[] = [];
  const git = status.git;
  if (git) {
    if (git.state === 'clean') {
      pills.push({ key: 'git', label: 'clean', tone: 'good', title: git.head_sha });
    } else if (git.state === 'dirty') {
      const count = git.changed_files ?? 0;
      pills.push({
        key: 'git',
        label: count > 0 ? `changes ${count}` : 'changes',
        tone: 'warn',
        title: git.reason,
      });
    } else {
      pills.push({ key: 'git', label: 'git unknown', tone: 'neutral', title: git.reason });
    }
    if (git.upstream) {
      pills.push(syncPill(git.ahead ?? 0, git.behind ?? 0, git.upstream));
    }
  }

  const analysis = status.analysis;
  if (analysis) {
    const backend = analysis.backend || 'analysis';
    if (analysis.state === 'current') {
      pills.push({
        key: 'analysis',
        label: `${backend} current`,
        tone: 'good',
        title: analysis.indexed_commit,
      });
    } else if (analysis.state === 'stale') {
      pills.push({
        key: 'analysis',
        label: `${backend} stale`,
        tone: 'warn',
        title: analysis.reason,
      });
    } else if (analysis.state === 'missing') {
      pills.push({
        key: 'analysis',
        label: `${backend} missing`,
        tone: 'warn',
        title: analysis.reason,
      });
    } else {
      pills.push({
        key: 'analysis',
        label: `${backend} unknown`,
        tone: 'neutral',
        title: analysis.reason,
      });
    }
  }

  return pills;
}

function syncPill(ahead: number, behind: number, upstream: string): ScopePill {
  if (ahead === 0 && behind === 0) {
    return { key: 'sync', label: 'synced', tone: 'good', title: upstream };
  }
  if (ahead > 0 && behind > 0) {
    return { key: 'sync', label: `diverged ${ahead}/${behind}`, tone: 'danger', title: upstream };
  }
  if (ahead > 0) {
    return { key: 'sync', label: `ahead ${ahead}`, tone: 'warn', title: upstream };
  }
  return { key: 'sync', label: `behind ${behind}`, tone: 'danger', title: upstream };
}

function scopePillTone(tone: ScopePillTone): string {
  switch (tone) {
    case 'good':
      return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300';
    case 'warn':
      return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300';
    case 'danger':
      return 'bg-rose-50 text-rose-700 dark:bg-rose-950/40 dark:text-rose-300';
    default:
      return 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300';
  }
}

// leftEllipsize keeps the right (most-specific) part of a long
// path visible. "/var/gemba/worktrees/sess-warm" with max=20 →
// "…/worktrees/sess-warm".
function leftEllipsize(s: string, max: number): string {
  if (s.length <= max) return s;
  return '…' + s.slice(s.length - max + 1);
}
