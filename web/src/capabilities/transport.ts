import type { Transport } from './types';
import type { Plane } from './hooks';

// Transport-aware routing helpers (gm-e11.4 / gm-root DD-12).
//
// The Go manifest carries a Transport enum (api | jsonl | mcp). The SPA
// itself only talks HTTP, but it still needs the value: when an adaptor
// is bound over jsonl or mcp it is running in-process (or behind a
// stdio bridge) and the browser can't reach it directly — mutating UI
// must route through the gemba server instead of attempting a direct
// call. This module centralises that decision so individual data hooks
// don't each reimplement it.
//
// useTransport (hook form) lives in hooks.ts; endpointFor is a pure
// function with no React dependencies so it can be called from query
// functions and plain modules.

// endpointFor returns the URL the SPA should hit for a logical endpoint
// name on a given plane. Today every supported transport flows through
// /api/* — the server is the single transport boundary the browser
// sees (gm-root §Novel §2). We encode that invariant here so future
// transports (direct WebSocket to an mcp adaptor, for example) land in
// one place rather than every call site.
export function endpointFor(plane: Plane, _transport: Transport, path: string): string {
  void plane;
  const trimmed = path.startsWith('/') ? path.slice(1) : path;
  return `/api/${trimmed}`;
}
