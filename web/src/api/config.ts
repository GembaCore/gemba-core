// Typed access to /api/config — the snapshot of server-side flags the
// SPA needs at boot. The Topbar reads BeadsSource from this so operators
// running multiple gemba instances against different beads databases can
// tell them apart at a glance (the database name *is* the workspace
// identity; cardinality is 1:1).

import { apiFetch } from './client';

export interface BeadsSource {
  // "beads-dir" | "dolt-url" | "unconfigured"
  kind: string;
  label: string;
  // Full path or DSN with credentials stripped. Empty when kind is
  // "unconfigured" or when a dolt-url failed to parse server-side.
  detail?: string;
}

export interface ServerConfig {
  auth_mode: string;
  yolo_available: boolean;
  listen: string;
  port: number;
  city: string;
  town: string;
  beads_source: BeadsSource;
}

export async function getServerConfig(): Promise<ServerConfig> {
  return apiFetch<ServerConfig>('/config');
}
