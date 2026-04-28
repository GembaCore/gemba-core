// RatifyDoneScreen (gm-root.17.7 — see docs/design/newproject.md §"Start
// planning" handoff).
//
// Full-page handoff shown after a successful Ratify. Replaces the two-pane
// conversation + plan-preview layout; it is the next view the operator sees
// after the project is committed.
//
// Two CTAs:
//
//   Start planning (primary) — switches the active workspace to the new
//     project + navigates to /walk (Gemba walk surface per gm-3nk).
//     The milestones and epics seeded by ratification are the natural
//     agenda; the walk surface will pull them in when it starts.
//
//   Skip (secondary) — switches the active workspace to the new project
//     + navigates to /gemba (the dashboard).
//
// Active-workspace switch (gm-102l): calls useProjectPicker().switchProject()
// from ProjectPickerContext (gm-root.18) before navigating. This keeps the
// top-bar picker label and activeProject state in sync with the newly-created
// project so the operator immediately sees the correct workspace selected.
//
// Gemba walk route (/walk) exists (gm-i65, shipped). The seed-agenda query
// parameter (e.g. ?seed=ratify&session=<id>) is a future gm-3nk concern —
// the handoff navigates to /walk bare today; the walk surface will pull the
// freshly-created milestones via the normal agenda population on start.

import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { CheckCircle2, Map, SkipForward } from 'lucide-react';
import { useProjectPicker } from '@/components/projectpicker/ProjectPickerContext';

export interface RatifyDoneScreenProps {
  // Human-readable project name for the confirmation headline.
  projectName: string;
  // Filesystem path of the new project root (used in the detail line).
  projectPath: string;
  // Milestone count — surfaces the "seeded N milestones" line so the
  // operator knows the walk agenda has content.
  milestoneCount: number;
  epicCount: number;
}

export function RatifyDoneScreen({
  projectName,
  projectPath,
  milestoneCount,
  epicCount,
}: RatifyDoneScreenProps): JSX.Element {
  const navigate = useNavigate();
  const { switchProject } = useProjectPicker();

  // Both CTAs switch the active workspace first (gm-102l) via
  // ProjectPickerContext.switchProject(). This keeps the top-bar picker
  // label and SPA activeProject state in sync with the new project so
  // the operator sees the correct workspace selected immediately after
  // navigating. Errors are swallowed so a failed switch doesn't block
  // navigation — the operator can switch manually from the picker.
  const switchWorkspace = useCallback(async () => {
    try {
      await switchProject(projectName);
    } catch {
      // Non-fatal: switch failure must not block navigation.
    }
  }, [switchProject, projectName]);

  const onStartPlanning = useCallback(async () => {
    await switchWorkspace();
    // /walk is the Gemba walk surface (gm-3nk, gm-i65). The walk page
    // starts a fresh walk if none is in flight; the freshly-created
    // milestones and epics will surface via the normal agenda pull.
    navigate('/walk');
  }, [navigate, switchWorkspace]);

  const onSkip = useCallback(async () => {
    await switchWorkspace();
    navigate('/gemba');
  }, [navigate, switchWorkspace]);

  const beadWord = milestoneCount === 1 ? 'milestone' : 'milestones';
  const epicWord = epicCount === 1 ? 'epic' : 'epics';

  return (
    <div
      data-testid="ratify-done-screen"
      className="flex h-full flex-col items-center justify-center px-6 py-12"
    >
      <div className="flex w-full max-w-md flex-col items-center gap-6 text-center">
        {/* Success icon */}
        <CheckCircle2
          data-testid="ratify-done-icon"
          className="h-14 w-14 text-emerald-500 dark:text-emerald-400"
          strokeWidth={1.5}
        />

        {/* Headline */}
        <div>
          <h1
            data-testid="ratify-done-project-name"
            className="text-xl font-semibold tracking-tight"
          >
            {projectName || 'Project'} is ready.
          </h1>
          {milestoneCount > 0 && (
            <p
              data-testid="ratify-done-summary"
              className="mt-1 text-sm text-neutral-500 dark:text-neutral-400"
            >
              Seeded {milestoneCount} {beadWord} and {epicCount} {epicWord}.
            </p>
          )}
          <p
            data-testid="ratify-done-path"
            className="mt-1 font-mono text-[11px] text-neutral-400 dark:text-neutral-500"
          >
            {projectPath}
          </p>
        </div>

        {/* CTAs */}
        <div className="flex w-full flex-col gap-3">
          {/* Primary: Start planning → /walk */}
          <button
            type="button"
            data-testid="ratify-done-start-planning"
            onClick={() => { void onStartPlanning(); }}
            className="flex items-center justify-center gap-2 rounded-md bg-emerald-600 px-5 py-2.5 text-sm font-semibold text-white shadow hover:bg-emerald-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-600"
          >
            <Map className="h-4 w-4" />
            Start planning
          </button>

          {/* Secondary: Skip → /gemba */}
          <button
            type="button"
            data-testid="ratify-done-skip"
            onClick={() => { void onSkip(); }}
            className="flex items-center justify-center gap-2 rounded-md border border-neutral-300 px-5 py-2 text-sm text-neutral-700 hover:bg-neutral-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-900"
          >
            <SkipForward className="h-4 w-4" />
            Skip — go to dashboard
          </button>
        </div>

        <p className="text-[11px] text-neutral-400 dark:text-neutral-500">
          You can start a Gemba walk later from the dashboard.
        </p>
      </div>
    </div>
  );
}
