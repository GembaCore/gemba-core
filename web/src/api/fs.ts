// Filesystem-listing client for the <FilePicker> component
// (gm-e12.21.1). Mirrors GET /api/v1/fs/list — read-only directory
// metadata bounded to $HOME and the configured projects dir.

import { apiFetch } from './client';

export interface FsEntry {
  name: string;
  kind: 'dir' | 'file';
  has_beads_db?: boolean;
}

export interface FsListEnvelope {
  path: string;
  parent?: string;
  home: string;
  projects_dir?: string;
  entries: FsEntry[];
}

// listFs fetches directory metadata for an absolute filesystem path.
// Throws ApiError on 4xx/5xx so callers can render an inline message
// for not-found / permission-denied / outside-allow-list cases.
export async function listFs(path: string): Promise<FsListEnvelope> {
  const qs = new URLSearchParams({ path });
  return apiFetch<FsListEnvelope>(`/v1/fs/list?${qs.toString()}`);
}

// FsInfo carries just the allow-list roots — what the picker needs
// to hydrate its Home / Projects shortcuts without first navigating
// into a directory.
export interface FsInfo {
  home: string;
  projects_dir?: string;
}

export async function getFsInfo(): Promise<FsInfo> {
  return apiFetch<FsInfo>('/v1/fs/info');
}
