// Step 4 — Ratify with nonce-confirmed commit (gm-uipx.7 — ui-spec §5.15).
//
// Final summary + commit button. The commit POST carries a fresh
// X-GEMBA-Confirm nonce so an accidental double-click can't write
// project config twice; the server caches replays per nonce.

import { useMemo, useState } from 'react';
import { commitBootstrap, type BootstrapPlan } from '@/api/bootstrap';

export interface RatifyStepProps {
  analysisId: string;
  plan: BootstrapPlan;
  onBack: () => void;
  onCommitted: (boardUrl: string) => void;
}

export function RatifyStep({
  analysisId,
  plan,
  onBack,
  onCommitted,
}: RatifyStepProps): JSX.Element {
  const [committing, setCommitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Pin a single nonce per mounted RatifyStep so a double-click on
  // the commit button uses the same nonce — the server cache returns
  // the same envelope rather than running the write twice.
  const nonce = useMemo(() => freshNonce(), []);

  const counts = plan.findings.reduce(
    (acc, f) => {
      acc[f.level] += 1;
      return acc;
    },
    { pass: 0, warn: 0, fail: 0 }
  );
  const failCount = counts.fail;

  const onCommit = async () => {
    if (committing) return;
    setCommitting(true);
    setError(null);
    try {
      const res = await commitBootstrap(analysisId, { nonce });
      onCommitted(res.board_url);
    } catch (e) {
      setError((e as Error).message);
      setCommitting(false);
    }
  };

  return (
    <div data-testid="bootstrap-ratify-step" className="flex flex-col gap-4">
      <div
        data-testid="bootstrap-ratify-summary"
        className="rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 text-xs dark:border-neutral-800 dark:bg-neutral-950"
      >
        <div className="font-semibold">
          About to commit project configuration from{' '}
          <span className="font-mono">{plan.source}</span>.
        </div>
        <ul className="mt-2 space-y-0.5 text-neutral-600 dark:text-neutral-400">
          <li data-testid="bootstrap-ratify-plan-count">
            Plan items: {plan.plan.length}
          </li>
          <li data-testid="bootstrap-ratify-findings">
            Findings: PASS {counts.pass} · WARN {counts.warn} · FAIL {counts.fail}
          </li>
          {failCount > 0 ? (
            <li
              data-testid="bootstrap-ratify-fail-warning"
              className="text-rose-700 dark:text-rose-400"
            >
              {failCount} consistency failure{failCount === 1 ? '' : 's'} —
              review before committing.
            </li>
          ) : null}
        </ul>
      </div>

      {error ? (
        <p
          data-testid="bootstrap-ratify-error"
          className="rounded border border-rose-300 bg-rose-50 px-3 py-2 text-xs text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
        >
          {error}
        </p>
      ) : null}

      <div className="flex items-center justify-between">
        <button
          type="button"
          data-testid="bootstrap-ratify-back"
          onClick={onBack}
          disabled={committing}
          className="rounded border border-neutral-300 px-3 py-1.5 text-xs hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-900"
        >
          Back
        </button>
        <button
          type="button"
          data-testid="bootstrap-ratify-commit"
          onClick={onCommit}
          disabled={committing}
          className="rounded bg-emerald-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {committing ? 'Committing…' : 'Commit project config'}
        </button>
      </div>
    </div>
  );
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
