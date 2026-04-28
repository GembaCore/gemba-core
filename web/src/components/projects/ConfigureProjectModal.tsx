// ConfigureProjectModal — adopt an existing beads DB as a gemba project
// (gm-xwa8).
//
// Opens when the picker resolves a beads DB without a .gemba/workspace.toml
// marker on this disk. Two scenarios this surface handles:
//
//   1. The operator has a legacy / imported / cross-machine beads DB
//      and wants to "adopt" it as a gemba project.
//   2. The disk-side .gemba/workspace.toml is stale or missing while the
//      backing beads DB is fine.
//
// The conversational /new flow is the wrong tool for both: that flow
// generates a fresh project tree from an LLM transcript. Attach is
// deterministic — write workspace.toml, optionally `git init` a fresh
// repo, ping the DB to confirm it is reachable. NO milestones / epics /
// beads are seeded.
//
// Field summary:
//   - Project name (text input).
//   - Base repository — radio:
//       a. "Use existing repository" → path picker (text input).
//       b. "Create new repository at default location" → readonly
//          preview of <default_dir>/<name>/.

import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '@/api/client';
import { attachProject, type AttachProjectRequest } from '@/api/projects';
import { useProjectPicker } from '@/components/projectpicker/ProjectPickerContext';

export interface ConfigureProjectModalProps {
  open: boolean;
  onClose: () => void;
  // beadsDB is the URL the picker resolved when the operator selected
  // an unmarked DB. Empty string is allowed (operator opened the modal
  // via the "+ Adopt existing beads DB…" affordance with no picker
  // context); the operator types it in.
  beadsDB?: string;
  // initialName lets the picker derive a default from the DB schema
  // name when discoverable.
  initialName?: string;
  // defaultDir is the resolved default projects directory used to render
  // the "Create new repository at default location" preview path.
  defaultDir?: string;
  // onAttached fires after a successful attach — the picker uses it to
  // close itself and switch the active workspace.
  onAttached?: (name: string) => void;
}

type RepoMode = 'existing' | 'create';

export function ConfigureProjectModal({
  open,
  onClose,
  beadsDB = '',
  initialName = '',
  defaultDir = '',
  onAttached,
}: ConfigureProjectModalProps) {
  const { reload } = useProjectPicker();
  const [name, setName] = useState(initialName);
  const [beadsDBValue, setBeadsDBValue] = useState(beadsDB);
  const [mode, setMode] = useState<RepoMode>('existing');
  const [repoPath, setRepoPath] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset state on open (so a closed-and-reopened modal starts clean).
  useEffect(() => {
    if (open) {
      setName(initialName);
      setBeadsDBValue(beadsDB);
      setMode('existing');
      setRepoPath('');
      setSubmitting(false);
      setError(null);
    }
  }, [open, initialName, beadsDB]);

  // Live preview of the create-at path. Empty when name is blank or
  // defaultDir wasn't provided.
  const createAtPreview = useMemo(() => {
    const trimmed = name.trim();
    if (!trimmed || !defaultDir) return '';
    const sep = defaultDir.endsWith('/') ? '' : '/';
    return `${defaultDir}${sep}${trimmed}/`;
  }, [name, defaultDir]);

  const canSubmit = (() => {
    if (submitting) return false;
    if (!name.trim()) return false;
    if (!beadsDBValue.trim()) return false;
    if (mode === 'existing' && !repoPath.trim()) return false;
    if (mode === 'create' && !createAtPreview) return false;
    return true;
  })();

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    const body: AttachProjectRequest = {
      beads_db: beadsDBValue.trim(),
      project_name: name.trim(),
      ...(mode === 'existing'
        ? { repo_path: repoPath.trim() }
        : { create_at: createAtPreview.replace(/\/$/, '') }),
    };
    try {
      const resp = await attachProject(body);
      onAttached?.(resp.name);
      // Refresh the picker's project list so the new entry appears.
      reload();
      onClose();
    } catch (err: unknown) {
      const msg =
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : String(err);
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <Dialog.Portal>
        <Dialog.Overlay
          data-testid="configure-project-modal-overlay"
          className="fixed inset-0 z-40 bg-black/40"
        />
        <Dialog.Content
          data-testid="configure-project-modal"
          aria-label="Configure project"
          className="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg bg-white p-6 shadow-xl dark:bg-neutral-900"
        >
          <Dialog.Title className="mb-4 text-lg font-semibold">
            Configure project
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-sm text-neutral-600 dark:text-neutral-400">
            Adopt an existing beads database as a gemba project. This writes
            <span className="font-mono"> .gemba/workspace.toml </span>
            but does NOT migrate or seed the database.
          </Dialog.Description>

          <label
            htmlFor="configure-project-name"
            className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400"
          >
            Project name
          </label>
          <input
            id="configure-project-name"
            data-testid="configure-project-name"
            type="text"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="my-project"
            className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
          />

          <label
            htmlFor="configure-project-beadsdb"
            className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400"
          >
            Beads database URL
          </label>
          <input
            id="configure-project-beadsdb"
            data-testid="configure-project-beadsdb"
            type="text"
            value={beadsDBValue}
            onChange={(e) => setBeadsDBValue(e.target.value)}
            placeholder="mysql://root@127.0.0.1:3307/my_project"
            className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 font-mono text-sm dark:border-neutral-700 dark:bg-neutral-800"
          />

          <fieldset className="mb-4">
            <legend className="mb-2 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
              Base repository
            </legend>
            <label className="mb-2 flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="repo-mode"
                value="existing"
                checked={mode === 'existing'}
                onChange={() => setMode('existing')}
                data-testid="configure-project-mode-existing"
                className="mt-1"
              />
              <span className="flex-1">
                <span className="block">Use existing repository</span>
                {mode === 'existing' && (
                  <input
                    data-testid="configure-project-repopath"
                    type="text"
                    value={repoPath}
                    onChange={(e) => setRepoPath(e.target.value)}
                    placeholder="/abs/path/to/existing/repo"
                    className="mt-1 w-full rounded-md border border-neutral-300 px-2 py-1 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-800"
                  />
                )}
              </span>
            </label>
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="repo-mode"
                value="create"
                checked={mode === 'create'}
                onChange={() => setMode('create')}
                data-testid="configure-project-mode-create"
                className="mt-1"
              />
              <span className="flex-1">
                <span className="block">Create new repository at default location</span>
                {mode === 'create' && (
                  <span
                    data-testid="configure-project-createat-preview"
                    className="mt-1 block rounded-md border border-neutral-200 bg-neutral-50 px-2 py-1 font-mono text-xs text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300"
                  >
                    {createAtPreview || '(enter project name)'}
                  </span>
                )}
              </span>
            </label>
          </fieldset>

          {error && (
            <div
              data-testid="configure-project-error"
              className="mb-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
            >
              {error}
            </div>
          )}

          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              data-testid="configure-project-cancel"
              className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submit}
              disabled={!canSubmit}
              data-testid="configure-project-submit"
              className="rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
            >
              {submitting ? 'Attaching…' : 'Attach project'}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
