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

// AdoptableDB is one row from GET /api/v1/projects/adoptable
// (gm-gmyl): a beads-shaped database visible on the configured Dolt
// server that does NOT correspond to any filesystem project. The
// picker renders these as one-click adopt candidates that pre-fill
// the configure-project modal.
export interface AdoptableDB {
  name: string;
  url: string;
}

// AdoptableEnvelope is the response body of /v1/projects/adoptable.
// `notice` is non-empty when the Dolt server is unreachable — the
// picker should still render normally; just hide the "Adoptable"
// section and surface the notice as a small inline hint.
export interface AdoptableEnvelope {
  dbs: AdoptableDB[];
  total: number;
  notice?: string;
}

// listAdoptableDBs fetches the adoptable list. Returns the envelope
// as-is so the caller can read both `dbs` and `notice`. Doesn't
// throw on Dolt-server-unreachable — that scenario surfaces as an
// empty list + a notice string.
export async function listAdoptableDBs(): Promise<AdoptableEnvelope> {
  return apiFetch<AdoptableEnvelope>('/v1/projects/adoptable');
}

// CloneProjectRequest is the request body for POST /v1/projects/clone
// (gm-e12.21.2). Both fields are required; the URL must be one of the
// supported schemes (http, https, ssh, scp-style git@host:org/repo).
export interface CloneProjectRequest {
  url: string;
  target_dir: string;
}

export interface CloneProjectResponse {
  repo_path: string;
  default_branch?: string;
  head_sha?: string;
}

// cloneProject posts to POST /v1/projects/clone. Nonce-gated so a
// double-submit can't double-clone. Throws ApiError on 4xx/5xx; the
// caller renders the error body's `message` inline.
export async function cloneProject(
  req: CloneProjectRequest,
  opts: { nonce?: string } = {}
): Promise<CloneProjectResponse> {
  return apiFetch<CloneProjectResponse>('/v1/projects/clone', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
    body: JSON.stringify(req),
  });
}
