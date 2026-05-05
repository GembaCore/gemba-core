// web/src/help/index.ts — route → help-module registry (gm-root.22.3).
//
// Used by HelpTab to resolve the appropriate RouteHelp component for the
// current pathname.
//
// Matching strategy:
//   - Exact-match entries are checked first (e.g. '/board').
//   - For routes with dynamic segments (e.g. /board/:epicId, /walks/:id),
//     HelpTab uses startsWith() against the key prefix.
//   - Unknown routes fall back to DefaultHelp.

import type { ComponentType } from 'react';

export type HelpModule = {
  RouteHelp: ComponentType;
};

// Each entry is a lazy-loadable module factory so the route help content
// is split from the main bundle. HelpTab resolves these synchronously
// via the registry — the components themselves are lightweight TSX, so
// eager import is fine for v1. Switch to React.lazy if bundle analysis
// warrants it.
import { RouteHelp as BoardHelp } from './BoardHelp';
import { RouteHelp as RefineHelp } from './RefineHelp';
import { RouteHelp as GraphHelp } from './GraphHelp';
import { RouteHelp as WalkHelp } from './WalkHelp';
import { RouteHelp as EscalationsHelp } from './EscalationsHelp';
import { RouteHelp as InsightsHelp } from './InsightsHelp';
import { RouteHelp as SessionsHelp } from './SessionsHelp';
import { RouteHelp as SettingsHelp } from './SettingsHelp';
import { RouteHelp as DefaultHelpComponent } from './DefaultHelp';

export { RouteHelp as ColdStartHelp } from './ColdStartHelp';
export { RouteHelp as DefaultHelp } from './DefaultHelp';

// Route prefix → component. Order matters: more-specific prefixes must
// come before their parent paths. HelpTab checks startsWith in entry
// order (Object.entries preserves insertion order in modern JS).
export const ROUTE_HELP_REGISTRY: Record<string, ComponentType> = {
  '/board': BoardHelp,
  '/refine': RefineHelp,
  '/backlog': RefineHelp,
  '/graph': GraphHelp,
  '/walk': WalkHelp,
  '/walks': WalkHelp,
  '/escalations': EscalationsHelp,
  '/insights': InsightsHelp,
  '/sessions': SessionsHelp,
  '/agents': SessionsHelp,
  '/settings': SettingsHelp,
};

/**
 * Resolve the help component for the given pathname.
 * Falls back to DefaultHelp when no entry matches.
 */
export function resolveHelpComponent(pathname: string): ComponentType {
  for (const [prefix, Component] of Object.entries(ROUTE_HELP_REGISTRY)) {
    if (pathname === prefix || pathname.startsWith(`${prefix}/`) || pathname.startsWith(`${prefix}?`)) {
      return Component;
    }
  }
  return DefaultHelpComponent;
}
