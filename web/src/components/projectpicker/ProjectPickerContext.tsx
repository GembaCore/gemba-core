// ProjectPickerContext — active-workspace state for the top-bar project
// picker (gm-root.18).
//
// Manages:
//   - the list of projects on this machine (fetched from /api/v1/projects)
//   - the currently-active project (server-reflected + SPA-optimistic)
//   - a `switchProject` mutation that calls POST /api/v1/projects/switch
//     and updates local state optimistically
//
// AppShell mounts the provider so the picker and any future consumer
// (e.g. gm-root.17.7's start-planning handoff) can read the active
// project without prop-drilling.
//
// gm-root.17.7 (start-planning handoff): call usePicker().switchProject()
// from within the ratification flow to set the newly-created project as
// active. The context provides this as a stable named API so that bead
// does not need to re-implement the mutation.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { listProjects, switchProject as apiSwitch, type ProjectEntry } from '@/api/projects';

export interface ProjectPickerState {
  // All projects discovered under default_dir. Empty on first load /
  // empty-state. Null means "loading" (before first fetch completes).
  projects: ProjectEntry[] | null;
  // The project the operator last switched to (server-reflected + SPA
  // optimistic). Null when no project has been switched to this session.
  activeProject: ProjectEntry | null;
  // isLoading is true while the initial project list fetch is in-flight.
  isLoading: boolean;
  // error is set when the last fetch or switch attempt failed.
  error: string | null;
  // switchProject switches the active workspace to the named project.
  // Updates activeProject optimistically; re-fetches the list to sync
  // the server's Active flag. Exposed for gm-root.17.7 to call after
  // ratification.
  switchProject: (name: string) => Promise<void>;
  // reload triggers a fresh fetch of the project list (e.g. after
  // project creation).
  reload: () => void;
}

const ProjectPickerContext = createContext<ProjectPickerState | null>(null);

export function ProjectPickerProvider({ children }: { children: ReactNode }) {
  const [projects, setProjects] = useState<ProjectEntry[] | null>(null);
  const [activeProject, setActiveProject] = useState<ProjectEntry | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reloadTick, setReloadTick] = useState(0);

  // Fetch project list on mount and whenever reload() is called.
  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    listProjects()
      .then((env) => {
        if (cancelled) return;
        setProjects(env.projects);
        // Reflect server-side active flag if set.
        const serverActive = env.projects.find((p) => p.active) ?? null;
        if (serverActive) {
          setActiveProject(serverActive);
        }
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [reloadTick]);

  const reload = useCallback(() => {
    setReloadTick((t) => t + 1);
  }, []);

  const switchProject = useCallback(async (name: string) => {
    // Optimistic update: find the project in the local list and mark it
    // active immediately so the picker label updates without waiting for
    // the round-trip.
    setProjects((prev) => {
      if (!prev) return prev;
      return prev.map((p) => ({ ...p, active: p.name === name }));
    });

    try {
      const resp = await apiSwitch({ name });
      // Server confirmed — sync with authoritative entry.
      setActiveProject(resp.active);
      setProjects((prev) => {
        if (!prev) return [resp.active];
        return prev.map((p) => ({ ...p, active: p.name === resp.active.name }));
      });
      setError(null);
    } catch (err: unknown) {
      // Roll back optimistic update on failure.
      reload();
      setError(err instanceof Error ? err.message : String(err));
      throw err;
    }
  }, [reload]);

  const value = useMemo<ProjectPickerState>(
    () => ({ projects, activeProject, isLoading, error, switchProject, reload }),
    [projects, activeProject, isLoading, error, switchProject, reload]
  );

  return (
    <ProjectPickerContext.Provider value={value}>
      {children}
    </ProjectPickerContext.Provider>
  );
}

export function useProjectPicker(): ProjectPickerState {
  const ctx = useContext(ProjectPickerContext);
  if (!ctx) {
    throw new Error('useProjectPicker: no ProjectPickerProvider in tree');
  }
  return ctx;
}
