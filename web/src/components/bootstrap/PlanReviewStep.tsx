// Step 3 — Plan review side-by-side (gm-uipx.7 — ui-spec §5.15).
//
// Left pane: generated plan (epics / milestones / sprints / stories).
// Right pane: consistency report — top-line PASS/WARN/FAIL summary
// followed by per-row drill-down details. Each finding is collapsed
// by default; click toggles the detail body.

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight } from 'lucide-react';
import {
  fetchBootstrapPlan,
  type BootstrapConsistencyFinding,
  type BootstrapPlan,
  type BootstrapPlanItem,
} from '@/api/bootstrap';
import { cn } from '@/lib/utils';

export interface PlanReviewStepProps {
  analysisId: string;
  plan: BootstrapPlan | null;
  setPlan: (p: BootstrapPlan) => void;
  onBack: () => void;
  onContinue: () => void;
}

export function PlanReviewStep({
  analysisId,
  plan,
  setPlan,
  onBack,
  onContinue,
}: PlanReviewStepProps): JSX.Element {
  const q = useQuery({
    queryKey: ['bootstrap', 'plan', analysisId],
    queryFn: () => fetchBootstrapPlan(analysisId),
    enabled: plan === null && !!analysisId,
  });

  // Cache the fetched plan into wizard state so re-renders + back-
  // navigation don't re-fetch.
  useEffect(() => {
    if (q.data && !plan) setPlan(q.data);
  }, [q.data, plan, setPlan]);

  const resolved = plan ?? q.data ?? null;

  if (q.isLoading && !resolved) {
    return (
      <p data-testid="bootstrap-plan-loading" className="text-sm italic text-neutral-500">
        Loading plan…
      </p>
    );
  }
  if (q.error && !resolved) {
    return (
      <p data-testid="bootstrap-plan-error" className="text-sm text-rose-600 dark:text-rose-400">
        Failed to load plan: {(q.error as Error).message}
      </p>
    );
  }
  if (!resolved) {
    return (
      <p data-testid="bootstrap-plan-empty" className="text-sm italic text-neutral-500">
        No plan available.
      </p>
    );
  }

  return (
    <div data-testid="bootstrap-plan-step" className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <PlanPane plan={resolved.plan} />
        <ReportPane findings={resolved.findings} />
      </div>
      <div className="flex items-center justify-between">
        <button
          type="button"
          data-testid="bootstrap-plan-back"
          onClick={onBack}
          className="rounded border border-neutral-300 px-3 py-1.5 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
        >
          Back
        </button>
        <button
          type="button"
          data-testid="bootstrap-plan-continue"
          onClick={onContinue}
          className="rounded bg-sky-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-sky-500"
        >
          Continue to ratify
        </button>
      </div>
    </div>
  );
}

function PlanPane({ plan }: { plan: BootstrapPlanItem[] }): JSX.Element {
  return (
    <section
      data-testid="bootstrap-plan-pane"
      className="rounded-md border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-950"
    >
      <header className="border-b border-neutral-200 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-neutral-600 dark:border-neutral-800 dark:text-neutral-400">
        Generated plan
      </header>
      <ul className="divide-y divide-neutral-100 dark:divide-neutral-900">
        {plan.length === 0 ? (
          <li className="px-3 py-3 text-xs italic text-neutral-500">
            No plan items.
          </li>
        ) : (
          plan.map((item) => <PlanRow key={item.id} item={item} depth={0} />)
        )}
      </ul>
    </section>
  );
}

function PlanRow({ item, depth }: { item: BootstrapPlanItem; depth: number }): JSX.Element {
  return (
    <li
      data-testid={`bootstrap-plan-row-${item.id}`}
      className="px-3 py-1.5"
      style={{ paddingLeft: `${0.75 + depth * 1}rem` }}
    >
      <div className="flex items-baseline gap-2">
        <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] uppercase text-neutral-600 dark:bg-neutral-900 dark:text-neutral-400">
          {item.kind}
        </span>
        <span className="text-sm">{item.title}</span>
      </div>
      {item.detail ? (
        <p className="mt-0.5 text-[11px] text-neutral-500">{item.detail}</p>
      ) : null}
      {item.children && item.children.length > 0 ? (
        <ul className="mt-1">
          {item.children.map((c) => (
            <PlanRow key={c.id} item={c} depth={depth + 1} />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

function ReportPane({
  findings,
}: {
  findings: BootstrapConsistencyFinding[];
}): JSX.Element {
  const counts = findings.reduce(
    (acc, f) => {
      acc[f.level] += 1;
      return acc;
    },
    { pass: 0, warn: 0, fail: 0 } as Record<BootstrapConsistencyFinding['level'], number>
  );
  const overall: BootstrapConsistencyFinding['level'] =
    counts.fail > 0 ? 'fail' : counts.warn > 0 ? 'warn' : 'pass';

  return (
    <section
      data-testid="bootstrap-report-pane"
      className="rounded-md border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-950"
    >
      <header className="flex items-center justify-between border-b border-neutral-200 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-neutral-600 dark:border-neutral-800 dark:text-neutral-400">
        <span>Consistency report</span>
        <span data-testid="bootstrap-report-overall" className={overallTone(overall)}>
          {overall.toUpperCase()}
        </span>
      </header>
      <div
        data-testid="bootstrap-report-summary"
        className="flex gap-3 border-b border-neutral-200 px-3 py-2 text-xs dark:border-neutral-800"
      >
        <span data-testid="bootstrap-report-count-pass" className="text-emerald-700 dark:text-emerald-400">
          PASS {counts.pass}
        </span>
        <span data-testid="bootstrap-report-count-warn" className="text-amber-700 dark:text-amber-400">
          WARN {counts.warn}
        </span>
        <span data-testid="bootstrap-report-count-fail" className="text-rose-700 dark:text-rose-400">
          FAIL {counts.fail}
        </span>
      </div>
      <ul className="divide-y divide-neutral-100 dark:divide-neutral-900">
        {findings.length === 0 ? (
          <li className="px-3 py-3 text-xs italic text-neutral-500">No findings.</li>
        ) : (
          findings.map((f) => <FindingRow key={f.id} finding={f} />)
        )}
      </ul>
    </section>
  );
}

function FindingRow({ finding }: { finding: BootstrapConsistencyFinding }): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <li
      data-testid={`bootstrap-report-finding-${finding.id}`}
      className="px-3 py-1.5"
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        data-testid={`bootstrap-report-toggle-${finding.id}`}
        className="flex w-full items-center gap-2 text-left text-xs"
      >
        {open ? (
          <ChevronDown className="h-3 w-3 text-neutral-500" aria-hidden />
        ) : (
          <ChevronRight className="h-3 w-3 text-neutral-500" aria-hidden />
        )}
        <span className={cn('font-mono text-[10px] uppercase', levelTone(finding.level))}>
          {finding.level}
        </span>
        <span className="flex-1">{finding.title}</span>
      </button>
      {open && finding.detail ? (
        <p
          data-testid={`bootstrap-report-detail-${finding.id}`}
          className="mt-1 pl-5 text-[11px] text-neutral-600 dark:text-neutral-400"
        >
          {finding.detail}
        </p>
      ) : null}
    </li>
  );
}

function overallTone(level: BootstrapConsistencyFinding['level']): string {
  switch (level) {
    case 'pass':
      return 'text-emerald-700 dark:text-emerald-400';
    case 'warn':
      return 'text-amber-700 dark:text-amber-400';
    case 'fail':
      return 'text-rose-700 dark:text-rose-400';
  }
}

function levelTone(level: BootstrapConsistencyFinding['level']): string {
  return overallTone(level);
}
