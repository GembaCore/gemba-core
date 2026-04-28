// Typed data-access for /api/v1/projects (gm-root.18).
//
// Two endpoints:
//
//   GET  /api/v1/projects          — enumerate projects under default_dir
//   POST /api/v1/projects/switch   — switch the active workspace
//
// The active-workspace state is managed in ProjectPickerContext (see
// web/src/components/projectpicker/ProjectPickerContext.tsx), which
// holds the last-known active project and re-fetches the list on demand.
// gm-root.17.7 (start-planning handoff) calls switchProject after
// ratification; gm-root.17.4 (cold-start redirect) reads the project
// list via this module to decide whether /new is needed.

import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';
import { freshNonce } from './newproject';

// ProjectEntry mirrors the server-side projectEntry shape from
// internal/server/projects.go. Name is the directory basename; Path
// is the absolute disk path; Active is true for the currently-selected
// project (in-process server state; resets on server restart).
export interface ProjectEntry {
  name: string;
  path: string;
  active?: boolean;
}

// ProjectsEnvelope is the response body of GET /api/v1/projects.
export interface ProjectsEnvelope {
  projects: ProjectEntry[];
  total: number;
}

// SwitchProjectRequest is the request body for POST /api/v1/projects/switch.
// Supply name (preferred, matched against the directory basename) or path
// (absolute disk path). Supplying both is fine — name is checked first.
export interface SwitchProjectRequest {
  name?: string;
  path?: string;
}

// SwitchProjectResponse is the response body of POST /api/v1/projects/switch.
export interface SwitchProjectResponse {
  active: ProjectEntry;
}

// listProjects fetches the full project list from GET /api/v1/projects.
// Returns an empty list (not an error) when no projects exist yet — the
// picker renders its empty state normally.
export async function listProjects(): Promise<ProjectsEnvelope> {
  return apiFetch<ProjectsEnvelope>('/v1/projects');
}

// switchProject posts to POST /api/v1/projects/switch and returns the
// newly-active project entry. Throws ApiError on network failure or if
// the project is not found (404).
//
// Shared endpoint for gm-root.17.7 (start-planning handoff after
// ratification) — that bead calls switchProject({ name }) and then
// navigates to the new project's board.
export async function switchProject(req: SwitchProjectRequest): Promise<SwitchProjectResponse> {
  return apiFetch<SwitchProjectResponse>('/v1/projects/switch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

// AttachProjectRequest is the request body for POST /api/v1/projects/attach
// (gm-xwa8). Used by the configure-project modal to "adopt" an existing
// beads DB into a project on this machine. Exactly one of repo_path
// (existing git worktree) or create_at (new directory; gemba creates +
// `git init`s) must be set.
export interface AttachProjectRequest {
  beads_db: string;
  project_name: string;
  repo_path?: string;
  create_at?: string;
}

// AttachProjectResponse mirrors the server's projectEntry shape so the
// picker can re-render with the new entry without an extra round-trip.
export type AttachProjectResponse = ProjectEntry;

// attachProject posts to POST /api/v1/projects/attach. Nonce-gated so
// a double-submit can't double-write the workspace marker. Returns the
// newly-attached project entry on success.
export async function attachProject(
  req: AttachProjectRequest,
  opts: { nonce?: string } = {}
): Promise<AttachProjectResponse> {
  return apiFetch<AttachProjectResponse>('/v1/projects/attach', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
    body: JSON.stringify(req),
  });
}
