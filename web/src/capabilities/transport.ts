import type { OrchestrationManifest, Transport, WorkPlaneManifest } from './types';

// Transport-aware routing (gm-root DD-12). The browser can only speak
// HTTP, but Gemba's adaptors may be attached over three wires:
//
//   - api    — HTTP+JSON direct (browser hits the adaptor endpoint).
//   - jsonl  — newline-delimited JSON over stdio (in-process or subprocess).
//   - mcp    — Model Context Protocol.
//
// jsonl and mcp adaptors ride behind the Gemba server's proxy: the SPA
// still talks HTTP, but the server multiplexes requests onto the
// underlying wire. resolveTransportBase returns the URL prefix the UI
// should use for a given manifest.
//
// For v1 the mapping is simple — /api for every transport, because the
// Gemba server is always the HTTP proxy — but callers MUST consult this
// helper rather than hardcoding, so when gm-e4.x ships direct-adaptor
// routing (e.g. per-adaptor subdomains, websocket bridges) every call
// site already threads the transport information.

export const API_BASE = '/api';
export const EVENTS_BASE = '/events';

export interface TransportInfo {
  transport: Transport;
  base: string;
  // proxied=true means the request is rewritten by the gemba server
  // onto a non-HTTP wire (jsonl/mcp). The UI doesn't need to change
  // behaviour but the capability-browser may want to surface this so
  // operators know why a request took an extra hop.
  proxied: boolean;
}

export function resolveTransport(transport: Transport): TransportInfo {
  switch (transport) {
    case 'api':
      return { transport, base: API_BASE, proxied: false };
    case 'jsonl':
    case 'mcp':
      return { transport, base: API_BASE, proxied: true };
  }
}

export function workPlaneTransport(m: WorkPlaneManifest | null): TransportInfo | null {
  return m ? resolveTransport(m.transport) : null;
}

export function orchestrationTransport(
  m: OrchestrationManifest | null,
): TransportInfo | null {
  return m ? resolveTransport(m.transport) : null;
}

// routePath joins the transport base with a resource path. It refuses
// non-absolute paths so callers can't accidentally build relative URLs
// that escape the transport envelope.
export function routePath(info: TransportInfo, path: string): string {
  if (!path.startsWith('/')) {
    throw new Error(`routePath: path must start with '/'; got ${path}`);
  }
  return `${info.base}${path}`;
}
