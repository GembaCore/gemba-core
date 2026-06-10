import { useMemo, useState } from 'react';
import { CheckCircle2, Loader2 } from 'lucide-react';
import {
  prepareOnboardingSetup,
  type OnboardingSetupFrame,
  type OnboardingSetupResponse,
} from '@/api/newproject';
import { cn } from '@/lib/utils';

export type ProjectOrigin = 'new' | 'existing' | 'import';
export type OnboardingOrchestration = 'native' | 'gastown';
export type SourceAnalysisTool = 'gitnexus' | 'none';

export interface OnboardingSetup {
  origin: ProjectOrigin;
  projectName: string;
  orchestration: OnboardingOrchestration;
  githubProject: string;
  worktreePath: string;
  gastownLocation: string;
  gastownRig: string;
  gastownWorktreePath: string;
  beadsUrl: string;
  sourceAnalysisTool: SourceAnalysisTool;
}

export interface OnboardingSetupPaneProps {
  onComplete: (setup: OnboardingSetup, result: OnboardingSetupResponse) => void;
  launching?: boolean;
}

const DEFAULT_SETUP: OnboardingSetup = {
  origin: 'new',
  projectName: '',
  orchestration: 'native',
  githubProject: '',
  worktreePath: '',
  gastownLocation: '',
  gastownRig: '',
  gastownWorktreePath: '',
  beadsUrl: '',
  sourceAnalysisTool: 'gitnexus',
};

export function OnboardingSetupPane({
  onComplete,
  launching = false,
}: OnboardingSetupPaneProps): JSX.Element {
  const [setup, setSetup] = useState<OnboardingSetup>(DEFAULT_SETUP);
  const [frames, setFrames] = useState<OnboardingSetupFrame[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [preparing, setPreparing] = useState(false);
  const activity = useMemo(() => setupActivity(setup), [setup]);
  const canContinue = isComplete(setup) && !launching && !preparing;

  const update = <K extends keyof OnboardingSetup>(key: K, value: OnboardingSetup[K]) => {
    setSetup((current) => {
      const next = { ...current, [key]: value };
      if (key === 'projectName' && !current.githubProject.trim()) {
        next.githubProject = slugify(String(value));
      }
      return next;
    });
  };

  const begin = async () => {
    if (!canContinue) return;
    setPreparing(true);
    setError(null);
    setFrames([
      {
        seq: 1,
        line: 'Running deterministic setup before launching the Onboarder.',
        level: 'info',
        done: false,
      },
    ]);
    try {
      const result = await prepareOnboardingSetup({
        origin: setup.origin,
        project_name: setup.projectName,
        github_project: setup.githubProject,
        orchestration: setup.orchestration,
        worktree_path: setup.orchestration === 'native' ? setup.worktreePath : undefined,
        gastown_location: setup.orchestration === 'gastown' ? setup.gastownLocation : undefined,
        gastown_rig: setup.orchestration === 'gastown' ? setup.gastownRig : undefined,
        gastown_worktree_path:
          setup.orchestration === 'gastown' ? setup.gastownWorktreePath : undefined,
        beads_url: setup.orchestration === 'gastown' ? setup.beadsUrl : undefined,
        source_analysis_tool: setup.sourceAnalysisTool,
      });
      setFrames(result.frames);
      onComplete(setup, result);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setFrames([]);
    } finally {
      setPreparing(false);
    }
  };

  return (
    <div
      data-testid="onboard-setup-pane"
      className="mx-auto flex h-full w-full max-w-5xl flex-col px-6 py-6"
    >
      <header className="mb-5">
        <h1 className="text-xl font-semibold text-neutral-900 dark:text-neutral-100">
          Start with deterministic setup
        </h1>
        <p className="mt-1 max-w-3xl text-sm text-neutral-500 dark:text-neutral-400">
          Gemba asks for project identity, source, repository, and runtime host before launching the
          Onboarder. The LLM only begins once the workspace contract is known.
        </p>
      </header>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        <section className="min-h-0 overflow-y-auto rounded-md border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-950">
          <Fieldset title="Project source">
            <Segmented
              value={setup.origin}
              options={[
                { value: 'new', label: 'New' },
                { value: 'existing', label: 'Existing' },
                { value: 'import', label: 'Import' },
              ]}
              onChange={(value) => update('origin', value as ProjectOrigin)}
            />
          </Fieldset>

          <Fieldset title="Project identity">
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">
                Project name
              </span>
              <input
                data-testid="onboard-setup-project-name"
                value={setup.projectName}
                onChange={(e) => update('projectName', e.target.value)}
                placeholder={setup.origin === 'new' ? 'temperature-spa' : 'existing-project'}
                className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
              />
            </label>
            <label className="mt-3 block">
              <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">
                GitHub project
              </span>
              <input
                data-testid="onboard-setup-github"
                value={setup.githubProject}
                onChange={(e) => update('githubProject', e.target.value)}
                placeholder="owner/repo"
                className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
              />
            </label>
          </Fieldset>

          <Fieldset title="Orchestration layer">
            <Segmented
              value={setup.orchestration}
              options={[
                { value: 'native', label: 'Native' },
                { value: 'gastown', label: 'Gas Town' },
              ]}
              onChange={(value) => update('orchestration', value as OnboardingOrchestration)}
            />
          </Fieldset>

          {setup.orchestration === 'gastown' ? (
            <Fieldset title="Gas Town location">
              <input
                data-testid="onboard-setup-gastown-location"
                value={setup.gastownLocation}
                onChange={(e) => update('gastownLocation', e.target.value)}
                placeholder="/path/to/city-or-town"
                className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
              />
              <label className="mt-3 block">
                <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">
                  Rig
                </span>
                <input
                  data-testid="onboard-setup-gastown-rig"
                  value={setup.gastownRig}
                  onChange={(e) => update('gastownRig', e.target.value)}
                  placeholder={slugify(setup.projectName) || 'existing-rig'}
                  className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
                />
              </label>
              <label className="mt-3 block">
                <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">
                  Rig worktree
                </span>
                <input
                  data-testid="onboard-setup-gastown-worktree"
                  value={setup.gastownWorktreePath}
                  onChange={(e) => update('gastownWorktreePath', e.target.value)}
                  placeholder="/path/to/city/rigs/example"
                  className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
                />
              </label>
              <label className="mt-3 block">
                <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">
                  Beads Dolt URL
                </span>
                <input
                  data-testid="onboard-setup-beads-url"
                  value={setup.beadsUrl}
                  onChange={(e) => update('beadsUrl', e.target.value)}
                  placeholder="mysql://root@127.0.0.1:3307/project_beads"
                  className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
                />
              </label>
            </Fieldset>
          ) : (
            <Fieldset title="Local worktree">
              <input
                data-testid="onboard-setup-worktree"
                value={setup.worktreePath}
                onChange={(e) => update('worktreePath', e.target.value)}
                placeholder="/path/to/worktree"
                className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
              />
            </Fieldset>
          )}

          <Fieldset title="Code intelligence">
            <Segmented
              value={setup.sourceAnalysisTool}
              options={[
                { value: 'gitnexus', label: 'GitNexus' },
                { value: 'none', label: 'Skip' },
              ]}
              onChange={(value) => update('sourceAnalysisTool', value as SourceAnalysisTool)}
            />
            <p className="mt-2 text-xs text-neutral-500 dark:text-neutral-400">
              Existing codebases default to GitNexus. Gemba installs it if needed, runs analysis,
              tests the source-analysis MCP connection, and writes agent setup files so LLMs can
              find Beads and code analysis.
            </p>
          </Fieldset>
        </section>

        <aside className="flex min-h-0 flex-col rounded-md border border-neutral-200 bg-neutral-50 p-4 dark:border-neutral-800 dark:bg-neutral-950">
          <h2 className="text-sm font-semibold">Setup activity</h2>
          <ol data-testid="onboard-setup-activity" className="mt-3 space-y-2 overflow-y-auto">
            {frames.length > 0
              ? frames.map((item) => (
                  <li
                    key={`${item.seq}-${item.line}`}
                    data-testid={`onboard-setup-frame-${item.seq}`}
                    className={cn(
                      'flex gap-2 text-xs',
                      item.level === 'warn'
                        ? 'text-amber-700 dark:text-amber-300'
                        : 'text-neutral-600 dark:text-neutral-300'
                    )}
                  >
                    {item.done ? (
                      <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600" />
                    ) : (
                      <Loader2 className="mt-0.5 h-3.5 w-3.5 shrink-0 animate-spin text-sky-600" />
                    )}
                    <span>{item.line}</span>
                  </li>
                ))
              : activity.map((item) => (
                  <li
                    key={item.label}
                    className="flex gap-2 text-xs text-neutral-600 dark:text-neutral-300"
                  >
                    {item.done ? (
                      <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600" />
                    ) : (
                      <Loader2 className="mt-0.5 h-3.5 w-3.5 shrink-0 animate-spin text-sky-600" />
                    )}
                    <span>{item.label}</span>
                  </li>
                ))}
          </ol>
          {error ? (
            <p
              data-testid="onboard-setup-error"
              className="mt-3 rounded border border-rose-300 bg-rose-50 px-2 py-1 text-xs text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
            >
              {error}
            </p>
          ) : null}
          <button
            type="button"
            data-testid="onboard-setup-continue"
            disabled={!canContinue}
            onClick={begin}
            className={cn(
              'mt-auto rounded-md px-4 py-2 text-sm font-medium',
              canContinue
                ? 'bg-sky-600 text-white hover:bg-sky-500'
                : 'bg-neutral-200 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-500'
            )}
          >
            {preparing
              ? 'Preparing workspace...'
              : launching
                ? 'Launching Onboarder...'
                : 'Begin coaching'}
          </button>
        </aside>
      </div>
    </div>
  );
}

function Fieldset({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset className="mb-5 last:mb-0">
      <legend className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        {title}
      </legend>
      {children}
    </fieldset>
  );
}

function Segmented({
  value,
  options,
  onChange,
}: {
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
}) {
  return (
    <div className="inline-flex rounded-md border border-neutral-300 bg-neutral-100 p-0.5 dark:border-neutral-700 dark:bg-neutral-900">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          data-testid={`onboard-setup-${option.value}`}
          onClick={() => onChange(option.value)}
          className={cn(
            'rounded px-3 py-1.5 text-xs font-medium',
            value === option.value
              ? 'bg-white text-neutral-900 shadow-sm dark:bg-neutral-800 dark:text-neutral-100'
              : 'text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100'
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function setupActivity(setup: OnboardingSetup): Array<{ label: string; done: boolean }> {
  const originLabel =
    setup.origin === 'new'
      ? 'new project'
      : setup.origin === 'existing'
        ? 'existing project'
        : 'imported project';
  const host =
    setup.orchestration === 'gastown'
      ? 'Gas Town mayor or crew host selected'
      : 'native worktree host selected';
  return [
    { label: `Project source selected: ${originLabel}`, done: true },
    { label: 'Project name captured', done: setup.projectName.trim().length > 0 },
    { label: 'GitHub project identity verified', done: setup.githubProject.trim().length > 0 },
    { label: host, done: true },
    {
      label:
        setup.orchestration === 'gastown'
          ? 'Gas Town root ready; existing rigs will be reused before creating new ones'
          : 'Local worktree location ready for sync or creation',
      done:
        setup.orchestration === 'gastown'
          ? setup.gastownLocation.trim().length > 0
          : setup.worktreePath.trim().length > 0,
    },
    {
      label:
        setup.orchestration === 'gastown'
          ? 'Beads will be read from the configured Dolt URL, not from the rig directory'
          : 'Local Beads database can be initialized or reused',
      done: setup.orchestration !== 'gastown' || setup.beadsUrl.trim().length > 0,
    },
    {
      label:
        setup.origin === 'new'
          ? 'Workspace scaffold can be created before LLM coaching'
          : 'Workspace can be adopted before LLM coaching',
      done: isComplete(setup),
    },
    {
      label:
        setup.sourceAnalysisTool === 'gitnexus'
          ? gitnexusActivityLabel(setup.origin)
          : 'Source analysis explicitly skipped',
      done: true,
    },
    {
      label: 'Beads MCP and source-analysis MCP will be tested before agent launch',
      done: setup.sourceAnalysisTool !== 'none',
    },
    {
      label:
        'LLM setup files will advertise Beads, decisions, epics, related beads, and analysis tools',
      done: true,
    },
  ];
}

function gitnexusActivityLabel(origin: ProjectOrigin): string {
  if (origin === 'new') {
    return 'GitNexus selected for first source-analysis index when code lands';
  }
  return 'GitNexus selected: install if missing, run analysis, and verify MCP';
}

function isComplete(setup: OnboardingSetup): boolean {
  if (!setup.projectName.trim() || !setup.githubProject.trim()) return false;
  if (setup.orchestration === 'gastown') return setup.gastownLocation.trim().length > 0;
  return setup.worktreePath.trim().length > 0;
}

function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._/-]+/g, '-')
    .replace(/^-+|-+$/g, '');
}
