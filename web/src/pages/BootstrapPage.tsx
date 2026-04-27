// /bootstrap (gm-uipx.7 — ui-spec §5.15).
//
// 4-step modal wizard: Source → Analysis → Plan review → Ratify.
// Each step has its own URL slug under /bootstrap/<step> so back/
// forward navigation works and the e2e tests can land on a specific
// step. Cancel from any step persists nothing — local state only,
// no backend writes until Step 4.

import { useCallback, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { X } from 'lucide-react';
import type { BootstrapPlan, BootstrapProgress, BootstrapSource } from '@/api/bootstrap';
import { SourceTiles } from '@/components/bootstrap/SourceTiles';
import { AnalysisStep } from '@/components/bootstrap/AnalysisStep';
import { PlanReviewStep } from '@/components/bootstrap/PlanReviewStep';
import { RatifyStep } from '@/components/bootstrap/RatifyStep';
import {
  INITIAL_WIZARD_STATE,
  STEPS,
  type StepID,
  type WizardState,
} from '@/components/bootstrap/types';
import { cn } from '@/lib/utils';

const STEP_IDS = new Set<StepID>(STEPS.map((s) => s.id));

function isStepID(v: string | undefined): v is StepID {
  return v !== undefined && STEP_IDS.has(v as StepID);
}

export function BootstrapPage(): JSX.Element {
  const params = useParams<{ step?: string }>();
  const navigate = useNavigate();
  const step: StepID = isStepID(params.step) ? params.step : 'source';

  // Wizard state lives entirely in the SPA until Step 4 commits. The
  // host owns it so each step's mount/unmount inside the route doesn't
  // throw away progress/plan data when the operator clicks "Back".
  const [state, setState] = useState<WizardState>(INITIAL_WIZARD_STATE);

  const goto = useCallback(
    (next: StepID) => navigate(`/bootstrap/${next}`),
    [navigate]
  );

  const cancel = useCallback(() => {
    // No backend writes happen until Step 4 commits. Clearing local
    // state is the only "undo" needed.
    setState(INITIAL_WIZARD_STATE);
    navigate('/board');
  }, [navigate]);

  const onSelectSource = useCallback(
    (source: BootstrapSource) => {
      // Clear analysis state if the operator picks a different source
      // mid-flow (e.g. went back to Step 1 from Step 3).
      setState({
        source,
        analysisId: state.source === source ? state.analysisId : null,
        progress: state.source === source ? state.progress : [],
        plan: state.source === source ? state.plan : null,
      });
      goto('analysis');
    },
    [goto, state.source, state.analysisId, state.progress, state.plan]
  );

  const setAnalysisId = useCallback((id: string) => {
    setState((s) => ({ ...s, analysisId: id }));
  }, []);

  const appendProgress = useCallback((frames: BootstrapProgress[]) => {
    setState((s) => {
      const seen = new Set(s.progress.map((p) => p.seq));
      const fresh = frames.filter((f) => !seen.has(f.seq));
      if (fresh.length === 0) return s;
      const next = [...s.progress, ...fresh].sort((a, b) => a.seq - b.seq);
      return { ...s, progress: next };
    });
  }, []);

  const setPlan = useCallback((p: BootstrapPlan) => {
    setState((s) => ({ ...s, plan: p }));
  }, []);

  const onCommitted = useCallback(
    (boardUrl: string) => {
      // Clear local state then navigate to the post-ratify destination
      // (server-supplied; defaults to /board if the backend left it
      // empty).
      setState(INITIAL_WIZARD_STATE);
      navigate(boardUrl || '/board');
    },
    [navigate]
  );

  return (
    <div data-testid="bootstrap-page" className="flex h-full min-h-0 flex-col">
      <Header step={step} onCancel={cancel} />
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto max-w-4xl">
          <Body
            step={step}
            state={state}
            goto={goto}
            onSelectSource={onSelectSource}
            setAnalysisId={setAnalysisId}
            appendProgress={appendProgress}
            setPlan={setPlan}
            onCommitted={onCommitted}
          />
        </div>
      </div>
    </div>
  );
}

function Header({ step, onCancel }: { step: StepID; onCancel: () => void }): JSX.Element {
  const idx = STEPS.findIndex((s) => s.id === step);
  return (
    <header className="border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-lg font-semibold">Bootstrap</h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Compose a project config in four steps. Cancel from any step
            and nothing is written.
          </p>
        </div>
        <button
          type="button"
          data-testid="bootstrap-cancel"
          onClick={onCancel}
          className="inline-flex items-center gap-1 rounded border border-neutral-300 px-2 py-1 text-xs hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-900"
        >
          <X className="h-3 w-3" /> Cancel
        </button>
      </div>
      <ol
        data-testid="bootstrap-steps"
        className="mt-3 flex items-center gap-1 text-xs"
      >
        {STEPS.map((s, i) => (
          <li
            key={s.id}
            data-testid={`bootstrap-step-${s.id}`}
            data-active={i === idx ? 'true' : undefined}
            data-complete={i < idx ? 'true' : undefined}
            className={cn(
              'flex items-center gap-1 rounded px-2 py-0.5',
              i === idx
                ? 'bg-sky-100 text-sky-800 dark:bg-sky-900/40 dark:text-sky-200'
                : i < idx
                  ? 'text-emerald-700 dark:text-emerald-400'
                  : 'text-neutral-500'
            )}
          >
            <span className="font-mono">{i + 1}.</span>
            <span>{s.label}</span>
          </li>
        ))}
      </ol>
    </header>
  );
}

interface BodyProps {
  step: StepID;
  state: WizardState;
  goto: (s: StepID) => void;
  onSelectSource: (source: BootstrapSource) => void;
  setAnalysisId: (id: string) => void;
  appendProgress: (frames: BootstrapProgress[]) => void;
  setPlan: (p: BootstrapPlan) => void;
  onCommitted: (boardUrl: string) => void;
}

function Body({
  step,
  state,
  goto,
  onSelectSource,
  setAnalysisId,
  appendProgress,
  setPlan,
  onCommitted,
}: BodyProps): JSX.Element {
  if (step === 'source') {
    return <SourceTiles onSelect={onSelectSource} />;
  }

  // Steps 2-4 require a pinned source. If the operator deep-linked
  // straight to /bootstrap/analysis (or the URL got stale via back),
  // fall back to Step 1 with a message.
  if (!state.source) {
    return (
      <div
        data-testid="bootstrap-needs-source"
        className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200"
      >
        Pick a source first.{' '}
        <button
          type="button"
          data-testid="bootstrap-needs-source-restart"
          onClick={() => goto('source')}
          className="underline"
        >
          Start over.
        </button>
      </div>
    );
  }

  if (step === 'analysis') {
    return (
      <AnalysisStep
        source={state.source}
        analysisId={state.analysisId}
        progress={state.progress}
        setAnalysisId={setAnalysisId}
        appendProgress={appendProgress}
        onContinue={() => goto('plan')}
        onBack={() => goto('source')}
      />
    );
  }

  // Steps 3-4 require analysis to have produced an id (and Step 4 a
  // plan). Otherwise route back to Step 2.
  if (!state.analysisId) {
    return (
      <div
        data-testid="bootstrap-needs-analysis"
        className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200"
      >
        Analysis hasn't started yet.{' '}
        <button
          type="button"
          onClick={() => goto('analysis')}
          className="underline"
          data-testid="bootstrap-needs-analysis-back"
        >
          Go to analysis.
        </button>
      </div>
    );
  }

  if (step === 'plan') {
    return (
      <PlanReviewStep
        analysisId={state.analysisId}
        plan={state.plan}
        setPlan={setPlan}
        onBack={() => goto('analysis')}
        onContinue={() => goto('ratify')}
      />
    );
  }

  // step === 'ratify'
  if (!state.plan) {
    return (
      <div
        data-testid="bootstrap-needs-plan"
        className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200"
      >
        No plan loaded.{' '}
        <button
          type="button"
          onClick={() => goto('plan')}
          className="underline"
          data-testid="bootstrap-needs-plan-back"
        >
          Review plan.
        </button>
      </div>
    );
  }

  return (
    <RatifyStep
      analysisId={state.analysisId}
      plan={state.plan}
      onBack={() => goto('plan')}
      onCommitted={onCommitted}
    />
  );
}
