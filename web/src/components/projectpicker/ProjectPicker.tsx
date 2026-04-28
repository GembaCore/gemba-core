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
import { ChevronDown, FolderOpen, Plus, Database } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { cn } from '@/lib/utils';
import { useProjectPicker } from './ProjectPickerContext';
import { useCreateProjectModal } from '@/components/projects/CreateProjectModalContext';
import { listAdoptableDBs, type AdoptableDB } from '@/api/projects';

export function ProjectPicker() {
  const { projects, activeProject, isLoading, error, switchProject } = useProjectPicker();
  const navigate = useNavigate();
  const { open: openCreateModal } = useCreateProjectModal();
  const [open, setOpen] = useState(false);
  // gm-gmyl: enumerate beads-shaped DBs visible on the configured Dolt
  // server that aren't already attached to a project. Fetched lazily
  // when the dropdown opens (avoids a Dolt round-trip on every page
  // load) and cached in state until the picker re-mounts. Empty list
  // is the common case — most operators have a one-project rig.
  const [adoptable, setAdoptable] = useState<AdoptableDB[]>([]);
  const [adoptableNotice, setAdoptableNotice] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // gm-gmyl: refresh the adoptable list each time the dropdown opens.
  // The Dolt server's databases can change between opens (new schema
  // imported, project attached); the cache lasts only as long as the
  // dropdown is mounted, then re-fetches on next open.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void listAdoptableDBs()
      .then((env) => {
        if (cancelled) return;
        setAdoptable(env.dbs ?? []);
        setAdoptableNotice(env.notice && env.notice.length > 0 ? env.notice : null);
      })
      .catch(() => {
        if (cancelled) return;
        // Hard fetch failure (server 500, network drop) — just hide
        // the section. The "+ Adopt existing beads DB…" affordance
        // below stays usable as the manual fallback.
        setAdoptable([]);
        setAdoptableNotice(null);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

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
          {/* gm-gmyl: "Adoptable beads DBs" section — schemas the
              configured Dolt server hosts that aren't yet attached to a
              filesystem project. Click → opens ConfigureProjectModal
              with the URL pre-filled. Only renders when the lister
              returned at least one candidate. */}
          {adoptable.length > 0 && (
            <div
              className="border-t border-neutral-200 dark:border-neutral-700"
              data-testid="project-picker-adoptable-section"
            >
              <div className="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
                Adoptable beads DBs
              </div>
              <ul className="pb-1" role="presentation">
                {adoptable.map((db) => (
                  <li key={db.name} role="presentation">
                    <button
                      type="button"
                      data-testid={`project-picker-adoptable-${db.name}`}
                      onClick={() => {
                        setOpen(false);
                        openCreateModal({
                          name: db.name,
                          db: { mode: 'adopt', value: db.url },
                          repo: { mode: 'create', value: '' },
                        });
                      }}
                      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-neutral-700 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
                    >
                      <Database className="h-3.5 w-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
                      <span className="truncate font-mono">{db.name}</span>
                      <span className="ml-auto truncate text-[10px] text-neutral-500 dark:text-neutral-500">
                        adopt
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {adoptableNotice && (
            <div
              className="border-t border-neutral-200 px-3 py-2 text-[11px] italic text-neutral-500 dark:border-neutral-700 dark:text-neutral-400"
              data-testid="project-picker-adoptable-notice"
            >
              {adoptableNotice}
            </div>
          )}
          {/* "+ Adopt existing beads DB…" affordance opens the unified
              Create-project modal pre-set to the Adopt-from-server DB
              axis. Sits below the project list (or empty state) so it
              doesn't visually compete with existing picker entries.
              gm-e12.21.3 superseded the standalone ConfigureProjectModal. */}
          <div className="border-t border-neutral-200 dark:border-neutral-700">
            <button
              type="button"
              data-testid="project-picker-adopt"
              onClick={() => {
                setOpen(false);
                openCreateModal({ db: { mode: 'adopt', value: '' } });
              }}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-800"
            >
              <Plus className="h-3.5 w-3.5 shrink-0" />
              <span>Adopt existing beads DB…</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
