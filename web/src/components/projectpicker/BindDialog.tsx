// BindDialog — bind an incomplete project entry to a git repo
// (gm-root.17.14).
//
// Opens when the operator clicks a picker entry whose `kind` is not
// "complete":
//
//   - kind === "needs_workspace" → the directory has a beads DB but no
//     .gemba/workspace.toml. The operator picks "create here" or
//     "move into existing repo".
//   - kind === "needs_repo"      → the directory has a workspace marker
//     but isn't inside a git worktree. Same two options.
//
// On confirm the dialog calls POST /api/v1/projects/bind via
// bindProject(); on success it switches the active workspace to the
// now-complete entry and refreshes the picker list. On error the
// server message is surfaced verbatim so the operator can retry or
// cancel.
//
// The locked contract (server stream owns; do NOT change here):
//
//   POST /api/v1/projects/bind
//     { beads_db_path, target_repo_path, mode: "create" | "navigate" }
//   → ProjectEntry (kind: "complete")
//
// "create"   — beads_db_path == target_repo_path; server git-inits the
//              dir and writes .gemba/workspace.toml.
// "navigate" — operator pastes an absolute repo path; server moves the
//              beads DB into that repo and writes the marker.

import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useState } from 'react';
import { ApiError } from '@/api/client';
import {
  bindProject,
  type BindProjectRequest,
  type ProjectEntry,
} from '@/api/projects';
import { useProjectPicker } from './ProjectPickerContext';

export interface BindDialogProps {
  open: boolean;
  // The entry the operator clicked. The dialog's body copy and the
  // bind call's beads_db_path are derived from this.
  entry: ProjectEntry | null;
  onClose: () => void;
  // Optional callback after a successful bind. Defaults to switching
  // the active workspace and reloading the picker list (the picker
  // wires both via context).
  onBound?: (entry: ProjectEntry) => void;
}

type BindMode = 'create' | 'navigate';

export function BindDialog({ open, entry, onClose, onBound }: BindDialogProps) {
  const { switchProject, reload } = useProjectPicker();
  const [mode, setMode] = useState<BindMode>('create');
  const [targetRepoPath, setTargetRepoPath] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset state on open so a closed-and-reopened dialog starts clean.
  useEffect(() => {
    if (open) {
      setMode('create');
      setTargetRepoPath('');
      setSubmitting(false);
      setError(null);
    }
  }, [open]);

  if (!entry) {
    // Defensive: render nothing if the parent didn't pass an entry.
    // The picker only opens the dialog with an entry, but a future
    // caller might forget.
    return null;
  }

  const isNeedsRepo = entry.kind === 'needs_repo';
  const description = isNeedsRepo
    ? "This workspace isn't in a git repo. Pick how to bind it:"
    : "This beads database doesn't have a .gemba/workspace.toml yet. Pick how to bind it:";

  const canSubmit = (() => {
    if (submitting) return false;
    if (mode === 'navigate' && !targetRepoPath.trim()) return false;
    return true;
  })();

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    const body: BindProjectRequest = {
      beads_db_path: entry.path,
      target_repo_path:
        mode === 'create' ? entry.path : targetRepoPath.trim(),
      mode,
    };
    try {
      const resp = await bindProject(body);
      // Default behavior — switch to the now-complete project and
      // refresh the picker list. Callers that want different behavior
      // pass onBound and own the post-bind flow themselves.
      if (onBound) {
        onBound(resp);
      } else {
        try {
          await switchProject(resp.name);
        } catch {
          // switchProject already set context.error; the dialog has
          // done its job by binding successfully, so close anyway.
        }
        reload();
      }
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
          data-testid="bind-dialog-overlay"
          className="fixed inset-0 z-40 bg-black/40"
        />
        <Dialog.Content
          data-testid="bind-dialog"
          aria-label="Bind project to git repo"
          className="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg bg-white p-6 shadow-xl dark:bg-neutral-900"
        >
          <Dialog.Title className="mb-2 text-lg font-semibold">
            Bind {entry.name} to a git repo
          </Dialog.Title>
          <Dialog.Description
            data-testid="bind-dialog-description"
            className="mb-4 text-sm text-neutral-600 dark:text-neutral-400"
          >
            {description}
          </Dialog.Description>

          <fieldset className="mb-4">
            <legend className="sr-only">Bind mode</legend>
            <label className="mb-2 flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="bind-mode"
                value="create"
                checked={mode === 'create'}
                onChange={() => setMode('create')}
                data-testid="bind-dialog-mode-create"
                className="mt-1"
              />
              <span className="flex-1">
                <span className="block">Create a new git repo here</span>
                <span className="mt-0.5 block text-xs text-neutral-500 dark:text-neutral-400">
                  <span className="font-mono">{entry.path}</span>
                </span>
              </span>
            </label>
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="bind-mode"
                value="navigate"
                checked={mode === 'navigate'}
                onChange={() => setMode('navigate')}
                data-testid="bind-dialog-mode-navigate"
                className="mt-1"
              />
              <span className="flex-1">
                <span className="block">Move it into an existing git repo</span>
                {mode === 'navigate' && (
                  <input
                    data-testid="bind-dialog-target-repo-path"
                    type="text"
                    value={targetRepoPath}
                    onChange={(e) => setTargetRepoPath(e.target.value)}
                    placeholder="/abs/path/to/existing/repo"
                    autoFocus
                    className="mt-1 w-full rounded-md border border-neutral-300 px-2 py-1 font-mono text-xs dark:border-neutral-700 dark:bg-neutral-800"
                  />
                )}
              </span>
            </label>
          </fieldset>

          {error && (
            <div
              data-testid="bind-dialog-error"
              className="mb-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
            >
              {error}
            </div>
          )}

          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              data-testid="bind-dialog-cancel"
              className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submit}
              disabled={!canSubmit}
              data-testid="bind-dialog-confirm"
              className="rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
            >
              {submitting ? 'Binding…' : 'Confirm'}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
