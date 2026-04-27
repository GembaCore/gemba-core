// Step 2 — Analysis live progress (gm-uipx.7 — ui-spec §5.15).
//
// Long-poll /api/bootstrap/progress while the backend analyses the
// chosen source. Render progress lines as they arrive; when the
// backend marks the final frame done=true, surface a "Continue"
// button that advances to Step 3.

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  pollBootstrapProgress,
  startBootstrap,
  type BootstrapProgress,
  type BootstrapSource,
} from '@/api/bootstrap';

const POLL_INTERVAL_MS = 500;

export interface AnalysisStepProps {
  source: BootstrapSource;
  analysisId: string | null;
  progress: BootstrapProgress[];
  setAnalysisId: (id: string) => void;
  appendProgress: (frames: BootstrapProgress[]) => void;
  onContinue: () => void;
  onBack: () => void;
}

export function AnalysisStep({
  source,
  analysisId,
  progress,
  setAnalysisId,
  appendProgress,
  onContinue,
  onBack,
}: AnalysisStepProps): JSX.Element {
  const [error, setError] = useState<string | null>(null);
  const startingRef = useRef(false);
  const lastSeq = progress.reduce((max, f) => (f.seq > max ? f.seq : max), 0);
  const done = progress.some((f) => f.done);

  // Kick off analysis on first mount when there isn't already an
  // analysis_id pinned (e.g. the operator stepped back from Step 3).
  useEffect(() => {
    if (analysisId || startingRef.current) return;
    startingRef.current = true;
    startBootstrap({ source })
      .then((res) => setAnalysisId(res.analysis_id))
      .catch((e) => setError((e as Error).message))
      .finally(() => {
        startingRef.current = false;
      });
  }, [analysisId, source, setAnalysisId]);

  // Long-poll progress. We schedule one poll at a time so a slow
  // backend can't pile up overlapping requests.
  const poll = useCallback(async (): Promise<boolean> => {
    if (!analysisId) return false;
    try {
      const frames = await pollBootstrapProgress(analysisId, lastSeq);
      if (frames.length > 0) appendProgress(frames);
      return frames.some((f) => f.done);
    } catch (e) {
      setError((e as Error).message);
      return true;
    }
  }, [analysisId, lastSeq, appendProgress]);

  useEffect(() => {
    if (!analysisId || done) return;
    let cancelled = false;
    const tick = async () => {
      if (cancelled) return;
      const finished = await poll();
      if (cancelled || finished) return;
      window.setTimeout(tick, POLL_INTERVAL_MS);
    };
    tick();
    return () => {
      cancelled = true;
    };
  }, [analysisId, done, poll]);

  return (
    <div data-testid="bootstrap-analysis-step" className="flex flex-col gap-4">
      <div className="text-xs uppercase tracking-wide text-neutral-500">
        Analyzing source: <span className="font-mono">{source}</span>
      </div>
      <div
        data-testid="bootstrap-analysis-log"
        className="min-h-[12rem] rounded-md border border-neutral-200 bg-neutral-50 p-3 font-mono text-xs dark:border-neutral-800 dark:bg-neutral-950"
      >
        {progress.length === 0 && !error ? (
          <p
            data-testid="bootstrap-analysis-waiting"
            className="italic text-neutral-500"
          >
            Starting analysis…
          </p>
        ) : null}
        <ul className="space-y-1">
          {progress.map((f) => (
            <li
              key={f.seq}
              data-testid={`bootstrap-analysis-line-${f.seq}`}
              className="flex gap-2 text-neutral-700 dark:text-neutral-300"
            >
              <span className="text-neutral-400">[{f.seq}]</span>
              <span>{f.line}</span>
            </li>
          ))}
        </ul>
        {error ? (
          <p
            data-testid="bootstrap-analysis-error"
            className="mt-2 text-rose-600 dark:text-rose-400"
          >
            {error}
          </p>
        ) : null}
      </div>
      <div className="flex items-center justify-between">
        <button
          type="button"
          data-testid="bootstrap-analysis-back"
          onClick={onBack}
          className="rounded border border-neutral-300 px-3 py-1.5 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
        >
          Back
        </button>
        <button
          type="button"
          data-testid="bootstrap-analysis-continue"
          onClick={onContinue}
          disabled={!done}
          className="rounded bg-sky-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {done ? 'Continue to plan review' : 'Analyzing…'}
        </button>
      </div>
    </div>
  );
}
