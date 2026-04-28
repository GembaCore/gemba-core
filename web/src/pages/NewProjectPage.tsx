// /new — lightweight project-creation form (gm-root.17.13).
//
// Replaces the conversational /new from gm-root.17.3 (now at /onboard).
// Posts {project_name, description} to POST /api/v1/newproject/create
// which runs the same atomic ratify transaction the conversational
// flow uses, but with an empty Milestones[] tree — so the operator
// gets a project dir + git init + .gemba/workspace.toml + beads DB
// without spawning the Onboarder or touching an LLM. After ratify the
// active workspace switches to the new project and the operator lands
// on /board, which renders an empty-state CTA inviting them into the
// conversational planner if they want one.

import { useCallback, useState } from 'react';
import { Plus } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { createProject } from '@/api/newproject';
import { useProjectPicker } from '@/components/projectpicker/ProjectPickerContext';

type Phase = 'idle' | 'submitting' | 'error';

export function NewProjectPage(): JSX.Element {
  const navigate = useNavigate();
  const { switchProject } = useProjectPicker();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [phase, setPhase] = useState<Phase>('idle');
  const [error, setError] = useState<string | null>(null);

  const trimmedName = name.trim();
  const canSubmit = trimmedName.length > 0 && phase !== 'submitting';

  const onSubmit = useCallback(
    async (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (!canSubmit) return;
      setPhase('submitting');
      setError(null);
      try {
        const resp = await createProject({
          project_name: trimmedName,
          description: description.trim(),
        });
        // Switch the active workspace to the newly-created project so
        // the picker label updates immediately and subsequent surfaces
        // render against the right beads DB. Failures are non-fatal —
        // the project exists; the user can switch manually.
        try {
          await switchProject(resp.project_name);
        } catch {
          /* swallow — non-fatal */
        }
        navigate('/board');
      } catch (err) {
        setPhase('error');
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [canSubmit, trimmedName, description, switchProject, navigate]
  );

  return (
    <div
      data-testid="newproject-page"
      data-phase={phase}
      className="flex h-full items-start justify-center overflow-auto px-6 py-12"
    >
      <form
        onSubmit={onSubmit}
        className="w-full max-w-xl rounded-lg border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-950"
      >
        <header className="mb-4 flex items-center gap-2">
          <Plus className="h-5 w-5 text-emerald-600 dark:text-emerald-400" aria-hidden />
          <h1 className="text-lg font-semibold">New project</h1>
        </header>
        <p className="mb-6 text-sm text-neutral-600 dark:text-neutral-400">
          Create a project — a directory with a git repo, a workspace config, and a fresh
          beads database. You can plan and add work items afterward, on your own or with the
          Onboarder.
        </p>

        <label htmlFor="np-name" className="block text-xs font-medium uppercase tracking-wide text-neutral-700 dark:text-neutral-300">
          Project name
        </label>
        <input
          id="np-name"
          data-testid="newproject-name"
          type="text"
          required
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="my-project"
          autoComplete="off"
          className="mt-1 block w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm shadow-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-neutral-700 dark:bg-neutral-900"
        />
        <p className="mt-1 text-[11px] text-neutral-500 dark:text-neutral-400">
          Used as the directory name under <code>~/gemba/projects/</code> (or the configured
          default).
        </p>

        <label htmlFor="np-description" className="mt-5 block text-xs font-medium uppercase tracking-wide text-neutral-700 dark:text-neutral-300">
          Description <span className="font-normal lowercase text-neutral-500">(optional)</span>
        </label>
        <textarea
          id="np-description"
          data-testid="newproject-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="One or two lines about what this project is for. You can edit it later."
          rows={3}
          className="mt-1 block w-full resize-y rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm shadow-sm focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-neutral-700 dark:bg-neutral-900"
        />

        {error ? (
          <div
            data-testid="newproject-error"
            className="mt-4 rounded-md border border-rose-300 bg-rose-50 px-3 py-2 text-xs text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
          >
            <p className="font-semibold">Couldn't create the project.</p>
            <p className="mt-1">{error}</p>
          </div>
        ) : null}

        <div className="mt-6 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={() => navigate(-1)}
            disabled={phase === 'submitting'}
            className="rounded-md border border-neutral-300 px-3 py-1.5 text-sm hover:bg-neutral-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="newproject-create"
            disabled={!canSubmit}
            className="rounded-md bg-emerald-600 px-4 py-1.5 text-sm font-semibold text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {phase === 'submitting' ? 'Creating…' : 'Create project'}
          </button>
        </div>
      </form>
    </div>
  );
}
