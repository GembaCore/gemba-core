// ProjectPicker — top-bar project switcher dropdown (gm-root.18).
//
// Always visible in the global SPA chrome regardless of the current route
// or whether a workspace is active. The "+" affordance (gm-root.17.2) is
// a SIBLING chrome element immediately to the LEFT of this picker; it is
// not part of this component. Leave the flex layout slot to the left clean.
//
// Empty state (no projects yet): renders the picker chrome with an empty
// list. The "+" to the left (gm-root.17.2) is the user's path to /new.
// The dropdown itself does NOT include a "create new" entry.
//
// Active project label: the currently-selected project name (or "…" while
// loading). The same pill that the existing BeadsSource label occupied;
// the server-config BeadsSource is now a secondary tooltip (detail).

import { useCallback, useEffect, useRef, useState } from 'react';
import { ChevronDown, FolderOpen, Plus } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { cn } from '@/lib/utils';
import { useProjectPicker } from './ProjectPickerContext';
import { ConfigureProjectModal } from '@/components/projects/ConfigureProjectModal';

export function ProjectPicker() {
  const { projects, activeProject, isLoading, error, switchProject } = useProjectPicker();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  // gm-xwa8: configure-project modal for adopting an existing beads DB
  // that has no .gemba/workspace.toml on this disk. Opened from the
  // "+ Adopt existing beads DB…" affordance at the bottom of the
  // dropdown. Detection path documented inline: see the bead for the
  // alternative server-side enumeration we punted on.
  const [adoptOpen, setAdoptOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Close dropdown on outside click or Escape.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    const onClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onClick);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onClick);
    };
  }, [open]);

  const handleSelect = useCallback(
    async (name: string) => {
      setOpen(false);
      try {
        await switchProject(name);
        // Navigate to the board — the active workspace changed.
        navigate('/board');
      } catch {
        // Error is surfaced via context.error; no local handling needed.
      }
    },
    [switchProject, navigate]
  );

  // Determine the label for the trigger pill.
  const label = isLoading
    ? '...'
    : activeProject
      ? activeProject.name
      : (projects && projects.length > 0 ? projects[0].name : 'no projects');

  // Tooltip: show path of active project or error.
  const tooltip = error
    ? `Error: ${error}`
    : activeProject?.path ?? '';

  const isEmpty = !isLoading && (!projects || projects.length === 0);

  return (
    <div ref={containerRef} className="relative" data-testid="project-picker">
      {/* Trigger pill — same style as the old workspace-label button */}
      <button
        type="button"
        data-testid="workspace-label"
        data-hotkey-target="workspace-switcher"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={tooltip}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-sm',
          'hover:bg-neutral-100 dark:hover:bg-neutral-800',
          error
            ? 'border-red-300 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-200'
            : activeProject
              ? 'border-neutral-200 bg-neutral-50 text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300'
              : 'border-neutral-200 bg-neutral-50 text-neutral-500 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-500'
        )}
      >
        <span className="font-mono" data-testid="project-picker-label">{label}</span>
        <ChevronDown
          className={cn('h-3.5 w-3.5 opacity-60 transition-transform', open && 'rotate-180')}
        />
      </button>

      {/* Dropdown */}
      {open && (
        <div
          role="listbox"
          aria-label="Switch project"
          data-testid="project-picker-dropdown"
          className={cn(
            'absolute left-0 top-full z-50 mt-1 min-w-48 rounded-md border',
            'border-neutral-200 bg-white shadow-md dark:border-neutral-700 dark:bg-neutral-900'
          )}
        >
          {isEmpty ? (
            // Empty state — no projects yet. The "+" (gm-root.17.2) is
            // the entry point; we just show a helpful empty message here.
            <div
              className="flex items-center gap-2 px-3 py-2.5 text-sm text-neutral-400 dark:text-neutral-500"
              data-testid="project-picker-empty"
            >
              <FolderOpen className="h-4 w-4" />
              <span>No projects yet</span>
            </div>
          ) : (
            <ul className="py-1" role="presentation">
              {(projects ?? []).map((p) => (
                <li key={p.name} role="presentation">
                  <button
                    type="button"
                    role="option"
                    aria-selected={p.active ?? p.name === activeProject?.name}
                    data-testid={`project-picker-item-${p.name}`}
                    onClick={() => handleSelect(p.name)}
                    className={cn(
                      'flex w-full items-center gap-2 px-3 py-2 text-left text-sm',
                      'hover:bg-neutral-100 dark:hover:bg-neutral-800',
                      (p.active ?? p.name === activeProject?.name)
                        ? 'font-medium text-neutral-900 dark:text-neutral-100'
                        : 'text-neutral-700 dark:text-neutral-300'
                    )}
                  >
                    <FolderOpen
                      className={cn(
                        'h-3.5 w-3.5 shrink-0',
                        (p.active ?? p.name === activeProject?.name)
                          ? 'text-sky-600 dark:text-sky-400'
                          : 'text-neutral-400 dark:text-neutral-500'
                      )}
                    />
                    <span className="truncate font-mono">{p.name}</span>
                    {(p.active ?? p.name === activeProject?.name) && (
                      <span className="ml-auto shrink-0 text-xs text-sky-600 dark:text-sky-400">
                        active
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
          {/* gm-xwa8: "+ Adopt existing beads DB…" affordance. Opens the
              ConfigureProjectModal so an operator can attach a beads DB
              (legacy / imported / cross-machine) that has no
              .gemba/workspace.toml on this disk. Sits below the project
              list (or empty state) so it doesn't visually compete with
              the existing picker entries. */}
          <div className="border-t border-neutral-200 dark:border-neutral-700">
            <button
              type="button"
              data-testid="project-picker-adopt"
              onClick={() => {
                setOpen(false);
                setAdoptOpen(true);
              }}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-800"
            >
              <Plus className="h-3.5 w-3.5 shrink-0" />
              <span>Adopt existing beads DB…</span>
            </button>
          </div>
        </div>
      )}

      <ConfigureProjectModal
        open={adoptOpen}
        onClose={() => setAdoptOpen(false)}
        onAttached={(name) => {
          // Switch to the newly-attached project so the picker label
          // and route reflect the new workspace immediately. The modal
          // already invalidated the list via context.reload().
          void switchProject(name).then(() => navigate('/board')).catch(() => {});
        }}
      />
    </div>
  );
}
