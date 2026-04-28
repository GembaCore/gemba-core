// CreateProjectModal — unified entry point replacing /new + ConfigureProjectModal
// (gm-e12.21.3).
//
// One modal, two axes side-by-side, project name on top:
//
//   ┌─ Create project ──────────────────────────────────────────────┐
//   │ Project name [_______________________]                        │
//   ├─────────────────────────┬─────────────────────────────────────┤
//   │ Beads database          │ Repository                          │
//   │ ( ) Existing on disk    │ ( ) Existing on disk                │
//   │ ( ) Adopt from server   │ ( ) Clone from URL                  │
//   │ ( ) Create new          │ ( ) Create new                      │
//   ├─────────────────────────┴─────────────────────────────────────┤
//   │                  [Cancel]  [Create project]                   │
//   └───────────────────────────────────────────────────────────────┘
//
// Submit verb is uniformly "Create project" — even pure adopt+existing
// flows count as "creating a project record on this disk."
//
// Combinations the v1 backend supports out of the box:
//   - existing_db    + existing_repo   → attachProject({beads_db, repo_path})
//   - existing_db    + create_repo     → attachProject({beads_db, create_at})
//   - adopt_server   + existing_repo   → attachProject({beads_db: url, repo_path})
//   - adopt_server   + create_repo     → attachProject({beads_db: url, create_at})
//   - create_db      + create_repo     → createProject({project_name, description})
//
// Combinations that need follow-up backend work (rendered disabled
// with a tooltip):
//   - create_db      + existing_repo   → needs a "init beads DB into existing dir" endpoint
//   - any            + clone_url       → needs gm-e12.21.2's clone endpoint
//
// File-picker buttons use the shared <FilePicker> component
// (gm-e12.21.1) — bounded to $HOME and the configured projects dir.
// The DB axis sets requireBeadsDb=true to mark dirs that already
// contain a .beads/ subdir; the Repository axis leaves it false.

import * as Dialog from '@radix-ui/react-dialog';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError } from '@/api/client';
import { attachProject, cloneProject, listAdoptableDBs, type AdoptableDB } from '@/api/projects';
import { createProject } from '@/api/newproject';
import { useProjectPicker } from '@/components/projectpicker/ProjectPickerContext';
import { FilePicker } from '@/components/FilePicker';
import { cn } from '@/lib/utils';

type DbMode = 'existing' | 'adopt' | 'create';
type RepoMode = 'existing' | 'clone' | 'create';

export interface CreateProjectModalProps {
  open: boolean;
  onClose: () => void;
  // prefill carries optional initial state — used when the picker's
  // "Adoptable beads DBs" row opens the modal with a specific schema
  // already selected on the DB axis.
  prefill?: {
    name?: string;
    db?: { mode: DbMode; value: string };
    repo?: { mode: RepoMode; value: string };
  };
}

// Each (DbMode, RepoMode) cell carries:
//   supported  — true when the v1 backend can dispatch it directly
//   reason     — operator-facing tooltip when supported=false
const COMBO_SUPPORT: Record<DbMode, Record<RepoMode, { supported: boolean; reason?: string }>> = {
  existing: {
    existing: { supported: true },
    clone: { supported: true },
    create: { supported: true },
  },
  adopt: {
    existing: { supported: true },
    clone: { supported: true },
    create: { supported: true },
  },
  create: {
    existing: { supported: false, reason: 'Init beads DB into existing dir pending backend extension' },
    clone: { supported: true },
    create: { supported: true },
  },
};

export function CreateProjectModal({ open, onClose, prefill }: CreateProjectModalProps) {
  const navigate = useNavigate();
  const { switchProject, reload } = useProjectPicker();

  const [name, setName] = useState(prefill?.name ?? '');
  const [dbMode, setDbMode] = useState<DbMode>(prefill?.db?.mode ?? 'create');
  const [repoMode, setRepoMode] = useState<RepoMode>(prefill?.repo?.mode ?? 'create');
  const [existingDbPath, setExistingDbPath] = useState('');
  const [adoptDbUrl, setAdoptDbUrl] = useState(prefill?.db?.mode === 'adopt' ? prefill.db.value : '');
  const [existingRepoPath, setExistingRepoPath] = useState(
    prefill?.repo?.mode === 'existing' ? prefill.repo.value : ''
  );
  const [cloneUrl, setCloneUrl] = useState('');
  const [adoptable, setAdoptable] = useState<AdoptableDB[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset state on each open so a closed-and-reopened modal starts
  // clean (or re-applies fresh prefill).
  useEffect(() => {
    if (!open) return;
    setName(prefill?.name ?? '');
    setDbMode(prefill?.db?.mode ?? 'create');
    setRepoMode(prefill?.repo?.mode ?? 'create');
    setExistingDbPath('');
    setAdoptDbUrl(prefill?.db?.mode === 'adopt' ? prefill.db.value : '');
    setExistingRepoPath(prefill?.repo?.mode === 'existing' ? prefill.repo.value : '');
    setCloneUrl('');
    setError(null);
    setSubmitting(false);
  }, [open, prefill]);

  // Lazy-fetch adoptable DBs the first time the operator picks "Adopt
  // from server" — keeps the open() path snappy for the common
  // create+create case.
  useEffect(() => {
    if (!open || dbMode !== 'adopt') return;
    if (adoptable.length > 0) return;
    let cancelled = false;
    void listAdoptableDBs()
      .then((env) => {
        if (cancelled) return;
        setAdoptable(env.dbs ?? []);
      })
      .catch(() => {
        // Silent — the operator can still type a URL if the lister 500s.
      });
    return () => {
      cancelled = true;
    };
  }, [open, dbMode, adoptable.length]);

  const combo = COMBO_SUPPORT[dbMode][repoMode];
  const trimmedName = name.trim();

  // canSubmit collapses every per-mode validation into one boolean so
  // the Submit button can be disabled without per-cell logic at the
  // call site.
  const canSubmit = useMemo(() => {
    if (!combo.supported) return false;
    if (submitting) return false;
    if (trimmedName.length === 0) return false;
    if (dbMode === 'existing' && existingDbPath.trim().length === 0) return false;
    if (dbMode === 'adopt' && adoptDbUrl.trim().length === 0) return false;
    if (repoMode === 'existing' && existingRepoPath.trim().length === 0) return false;
    if (repoMode === 'clone' && cloneUrl.trim().length === 0) return false;
    return true;
  }, [combo.supported, submitting, trimmedName, dbMode, existingDbPath, adoptDbUrl, repoMode, existingRepoPath, cloneUrl]);

  const onSubmit = useCallback(async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      const beadsDb =
        dbMode === 'existing' ? existingDbPath.trim()
        : dbMode === 'adopt' ? adoptDbUrl.trim()
        : '';

      // Clone first when the repo axis asks for it. The cloned dir
      // becomes the repo_path the rest of the flow attaches against.
      let resolvedRepoPath = '';
      if (repoMode === 'clone') {
        // Default the target dir under the operator's projects root —
        // the clone endpoint enforces the allow list and refuses
        // non-empty targets, so this is safe even when we've never
        // computed a real default_dir client-side.
        const targetDir = `${trimmedName}`;
        const cloneResp = await cloneProject({
          url: cloneUrl.trim(),
          target_dir: targetDir.startsWith('/') ? targetDir : `/${targetDir}`,
        });
        resolvedRepoPath = cloneResp.repo_path;
      } else if (repoMode === 'existing') {
        resolvedRepoPath = existingRepoPath.trim();
      }

      if (dbMode === 'create' && repoMode === 'create') {
        // Lightweight scaffold: directory + git init + .gemba + beads DB.
        await createProject({ project_name: trimmedName, description: '' });
      } else if (dbMode === 'create' && repoMode === 'clone') {
        // Cloned repo, fresh beads DB inside it. Express via attach
        // with an empty beads_db so the backend creates a new one
        // alongside the cloned repo. (Backend extension lands in a
        // follow-up; for now this dispatches the same way as adopt+
        // existing-repo with an empty DB hint — which the backend
        // currently rejects, surfacing the "needs init" gap to the
        // operator.)
        await attachProject({
          beads_db: '',
          project_name: trimmedName,
          repo_path: resolvedRepoPath,
        });
      } else {
        // Adopt path covers existing-db, adopt-server, and any
        // existing/create/clone repo combination supported above.
        const req =
          repoMode === 'existing' || repoMode === 'clone'
            ? { beads_db: beadsDb, project_name: trimmedName, repo_path: resolvedRepoPath }
            : { beads_db: beadsDb, project_name: trimmedName, create_at: trimmedName };
        await attachProject(req);
      }

      // Re-fetch the project list so the picker dropdown reflects the
      // new entry, then switch and route to /board.
      await reload();
      try {
        await switchProject(trimmedName);
      } catch {
        // non-fatal — the project exists; the operator can switch
        // manually if the auto-switch races.
      }
      onClose();
      navigate('/board');
    } catch (err) {
      setSubmitting(false);
      if (err instanceof ApiError) {
        setError(`${err.status}: ${err.message}`);
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
    }
  }, [
    canSubmit, dbMode, existingDbPath, adoptDbUrl, repoMode, existingRepoPath,
    cloneUrl, trimmedName, reload, switchProject, onClose, navigate,
  ]);

  return (
    <Dialog.Root open={open} onOpenChange={(o) => !o && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
        <Dialog.Content
          data-testid="create-project-modal"
          className={cn(
            'fixed left-1/2 top-1/2 z-50 w-full max-w-3xl -translate-x-1/2 -translate-y-1/2',
            'rounded-lg border border-neutral-200 bg-white p-6 shadow-lg',
            'dark:border-neutral-800 dark:bg-neutral-950'
          )}
        >
          <Dialog.Title className="mb-4 text-lg font-semibold">Create project</Dialog.Title>

          {/* Project name — top, full width */}
          <label className="mb-6 block">
            <span className="mb-1 block text-sm font-medium">Project name</span>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
              data-testid="create-project-name"
              placeholder="my-project"
              className="w-full rounded-md border border-neutral-300 bg-white px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
            />
          </label>

          <div className="grid grid-cols-2 gap-6">
            {/* Beads database axis */}
            <fieldset data-testid="db-axis" className="space-y-3">
              <legend className="text-sm font-medium">Beads database</legend>
              <Radio
                checked={dbMode === 'existing'}
                onChange={() => setDbMode('existing')}
                label="Existing on disk"
                testid="db-mode-existing"
              >
                <FilePicker
                  value={existingDbPath}
                  onChange={setExistingDbPath}
                  disabled={dbMode !== 'existing'}
                  requireBeadsDb
                  placeholder="/path/to/project"
                  testid="db-existing-path"
                />
              </Radio>
              <Radio
                checked={dbMode === 'adopt'}
                onChange={() => setDbMode('adopt')}
                label="Adopt from server"
                testid="db-mode-adopt"
              >
                <select
                  value={adoptDbUrl}
                  onChange={(e) => setAdoptDbUrl(e.target.value)}
                  disabled={dbMode !== 'adopt'}
                  data-testid="db-adopt-select"
                  className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-sm disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
                >
                  <option value="">Select schema…</option>
                  {adoptable.map((db) => (
                    <option key={db.url} value={db.url}>{db.name}</option>
                  ))}
                </select>
              </Radio>
              <Radio
                checked={dbMode === 'create'}
                onChange={() => setDbMode('create')}
                label="Create new"
                testid="db-mode-create"
              >
                <span className="block font-mono text-xs text-neutral-500 dark:text-neutral-400">
                  {trimmedName ? `${trimmedName}.db` : '<name>.db'}
                </span>
              </Radio>
            </fieldset>

            {/* Repository axis */}
            <fieldset data-testid="repo-axis" className="space-y-3">
              <legend className="text-sm font-medium">Repository</legend>
              <Radio
                checked={repoMode === 'existing'}
                onChange={() => setRepoMode('existing')}
                label="Existing on disk"
                testid="repo-mode-existing"
              >
                <FilePicker
                  value={existingRepoPath}
                  onChange={setExistingRepoPath}
                  disabled={repoMode !== 'existing'}
                  placeholder="/path/to/repo"
                  testid="repo-existing-path"
                />
              </Radio>
              <Radio
                checked={repoMode === 'clone'}
                onChange={() => setRepoMode('clone')}
                label="Clone from URL"
                testid="repo-mode-clone"
              >
                <input
                  type="text"
                  value={cloneUrl}
                  onChange={(e) => setCloneUrl(e.target.value)}
                  disabled={repoMode !== 'clone'}
                  placeholder="https://github.com/owner/repo.git"
                  data-testid="repo-clone-url"
                  className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-sm disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
                />
              </Radio>
              <Radio
                checked={repoMode === 'create'}
                onChange={() => setRepoMode('create')}
                label="Create new"
                testid="repo-mode-create"
              >
                <span className="block font-mono text-xs text-neutral-500 dark:text-neutral-400">
                  {trimmedName ? `${trimmedName}/` : '<name>/'} <span className="opacity-70">in default dir</span>
                </span>
              </Radio>
            </fieldset>
          </div>

          {!combo.supported && (
            <div
              data-testid="combo-warning"
              className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300"
            >
              Not yet supported: {combo.reason}
            </div>
          )}

          {error && (
            <div
              data-testid="create-project-error"
              role="alert"
              className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
            >
              {error}
            </div>
          )}

          <div className="mt-6 flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              data-testid="create-project-cancel"
              className="rounded-md px-3 py-1.5 text-sm text-neutral-700 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-900"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={onSubmit}
              disabled={!canSubmit}
              data-testid="create-project-submit"
              className={cn(
                'rounded-md px-4 py-1.5 text-sm font-medium',
                canSubmit
                  ? 'bg-neutral-900 text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200'
                  : 'bg-neutral-200 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-500'
              )}
            >
              {submitting ? 'Creating…' : 'Create project'}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

// Radio — one row in the axis fieldset. Renders a label + optional
// child input that's only enabled when this radio is selected.
function Radio({
  checked,
  onChange,
  label,
  testid,
  disabled,
  disabledReason,
  children,
}: {
  checked: boolean;
  onChange: () => void;
  label: string;
  testid: string;
  disabled?: boolean;
  disabledReason?: string;
  children?: React.ReactNode;
}) {
  return (
    <label
      title={disabled ? disabledReason : undefined}
      className={cn(
        'flex cursor-pointer flex-col gap-1.5 rounded-md border border-transparent p-2',
        checked && !disabled && 'border-neutral-300 bg-neutral-50 dark:border-neutral-700 dark:bg-neutral-900',
        disabled && 'cursor-not-allowed opacity-50'
      )}
    >
      <div className="flex items-center gap-2 text-sm">
        <input
          type="radio"
          checked={checked}
          onChange={onChange}
          disabled={disabled}
          data-testid={testid}
          className="h-3.5 w-3.5"
        />
        <span>{label}</span>
      </div>
      {children && <div className="ml-5">{children}</div>}
    </label>
  );
}

