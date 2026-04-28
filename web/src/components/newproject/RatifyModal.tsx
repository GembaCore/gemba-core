// RatifyModal (gm-root.17.3 — see docs/design/newproject.md).
//
// Final commit modal. Surfaces the full plan tree + draft project.md
// for review and a single confirm button. The confirm POST carries a
// fresh X-GEMBA-Confirm nonce so a double-click can't double-write
// the project tree.
//
// Built as an inline overlay rather than a Radix Dialog so the
// e2e tests can poke at it with stable testids without portal
// indirection (matches the codebase's other inline modal patterns).

import { useEffect, useMemo, useState } from 'react';
import { freshNonce, type NewProjectState } from '@/api/newproject';

export interface RatifyModalProps {
  open: boolean;
  state: NewProjectState;
  // Called with the operator's (immutable) confirm. The host owns the
  // network call and supplies the pinned nonce so a double-click on
  // the confirm button replays cleanly.
  onConfirm: (nonce: string) => void;
  onCancel: () => void;
  // True while /ratify is in flight. Disables the confirm button and
  // surfaces a "Committing…" label.
  committing: boolean;
  // Last error message from a failed ratify, if any.
  error: string | null;
}

export function RatifyModal({
  open,
  state,
  onConfirm,
  onCancel,
  committing,
  error,
}: RatifyModalProps): JSX.Element | null {
  // Pin a single nonce per modal mount so a double-click on the
  // confirm button uses the same nonce — the server cache returns the
  // same envelope rather than running the write twice.
  const nonce = useMemo(() => freshNonce(), []);
  const [esc, setEsc] = useState(0);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !committing) {
        setEsc((n) => n + 1);
        onCancel();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, committing, onCancel]);

  if (!open) return null;

  // Total counts for the at-a-glance summary at the top of the modal.
  const epicCount = state.Milestones.reduce((acc, m) => acc + m.Epics.length, 0);
  const beadCount = state.Milestones.reduce(
    (acc, m) => acc + m.Epics.reduce((a, e) => a + e.Beads.length, 0),
    0
  );

  return (
    <div
      data-testid="newproject-ratify-modal"
      data-esc-presses={esc}
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-40 flex items-center justify-center bg-neutral-900/40 p-4"
    >
      <div className="flex max-h-full w-full max-w-2xl flex-col overflow-hidden rounded-lg border border-neutral-200 bg-white shadow-xl dark:border-neutral-800 dark:bg-neutral-950">
        <header className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <h2 className="text-sm font-semibold">Ratify new project</h2>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Confirm to create the workspace, beads database, and
            <span className="font-mono"> docs/project.md</span>. This commit is atomic — any failure rolls back.
          </p>
        </header>

        <div className="flex-1 overflow-y-auto px-4 py-3">
          <ul
            data-testid="newproject-ratify-summary"
            className="mb-3 grid grid-cols-3 gap-2 text-[11px]"
          >
            <li
              data-testid="newproject-ratify-count-milestones"
              className="rounded border border-neutral-200 px-2 py-1 dark:border-neutral-800"
            >
              <span className="block text-[10px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
                Milestones
              </span>
              <span className="font-mono">{state.Milestones.length}</span>
            </li>
            <li
              data-testid="newproject-ratify-count-epics"
              className="rounded border border-neutral-200 px-2 py-1 dark:border-neutral-800"
            >
              <span className="block text-[10px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
                Epics
              </span>
              <span className="font-mono">{epicCount}</span>
            </li>
            <li
              data-testid="newproject-ratify-count-beads"
              className="rounded border border-neutral-200 px-2 py-1 dark:border-neutral-800"
            >
              <span className="block text-[10px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
                Beads
              </span>
              <span className="font-mono">{beadCount}</span>
            </li>
          </ul>

          <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
            Project
          </h3>
          <p
            data-testid="newproject-ratify-project-name"
            className="mb-3 rounded border border-neutral-200 bg-neutral-50 px-2 py-1 text-xs dark:border-neutral-800 dark:bg-neutral-900"
          >
            {state.ProjectName || (
              <em className="text-rose-700 dark:text-rose-400">no name set</em>
            )}
          </p>

          <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
            Plan tree
          </h3>
          {state.Milestones.length === 0 ? (
            <p
              data-testid="newproject-ratify-tree-empty"
              className="rounded border border-dashed border-neutral-300 px-2 py-3 text-center text-[11px] italic text-neutral-500 dark:border-neutral-700 dark:text-neutral-400"
            >
              No milestones to ratify.
            </p>
          ) : (
            <ol data-testid="newproject-ratify-tree" className="mb-3 space-y-1 text-[11px]">
              {state.Milestones.map((m, i) => (
                <li key={`m-${i}`} className="rounded bg-neutral-50 px-2 py-1 dark:bg-neutral-900">
                  <strong>M{i + 1}.</strong> {m.Title || <em>untitled</em>}
                  {m.Epics.length > 0 && (
                    <ul className="ml-4 list-disc space-y-0.5">
                      {m.Epics.map((e, j) => (
                        <li key={`m-${i}-e-${j}`}>
                          {e.Title || <em>untitled epic</em>}
                          {e.Beads.length > 0 && (
                            <span className="text-neutral-500 dark:text-neutral-400">
                              {' '}
                              ({e.Beads.length} bead{e.Beads.length === 1 ? '' : 's'})
                            </span>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              ))}
            </ol>
          )}

          <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
            docs/project.md
          </h3>
          <pre
            data-testid="newproject-ratify-draft-md"
            className="max-h-48 overflow-auto whitespace-pre-wrap rounded border border-neutral-200 bg-neutral-50 px-2 py-1 text-[11px] dark:border-neutral-800 dark:bg-neutral-900"
          >
            {state.DraftProjectMD || '(empty)'}
          </pre>

          {error ? (
            <p
              data-testid="newproject-ratify-error"
              className="mt-3 rounded border border-rose-300 bg-rose-50 px-2 py-1 text-[11px] text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
            >
              {error}
            </p>
          ) : null}
        </div>

        <footer className="flex items-center justify-between border-t border-neutral-200 px-4 py-3 dark:border-neutral-800">
          <button
            type="button"
            data-testid="newproject-ratify-cancel"
            onClick={onCancel}
            disabled={committing}
            className="rounded border border-neutral-300 px-3 py-1 text-xs hover:bg-neutral-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-900"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="newproject-ratify-confirm"
            onClick={() => onConfirm(nonce)}
            disabled={committing}
            className="rounded bg-emerald-600 px-4 py-1 text-xs font-semibold text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {committing ? 'Committing…' : 'Confirm and commit'}
          </button>
        </footer>
      </div>
    </div>
  );
}
