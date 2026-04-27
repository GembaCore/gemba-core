// Shared types for the kind-specific Agent detail panels (gm-e12.15).
//
// Each Workspace.kind gets its own affordance set per ui-spec §5.18 +
// DD-5. The panels all consume the same PlannerOperationalContext so a
// caller can branch on workspace.kind and forward the same prop bundle.

import type { PlannerOperationalContext } from '@/api/planner';

export interface AgentDetailPanelProps {
  ctx: PlannerOperationalContext;
}

// CanonicalWorkspaceKind mirrors internal/core/orchestration.go's
// WorkspaceKind enum. The wire surface is a free-form string (adaptors
// MAY emit kinds beyond core's knowledge) so the SPA falls back to a
// generic panel when an adaptor declares a kind we don't recognize.
export type CanonicalWorkspaceKind =
  | 'worktree'
  | 'container'
  | 'k8s_pod'
  | 'vm'
  | 'exec'
  | 'subprocess';

export const CANONICAL_KINDS: readonly CanonicalWorkspaceKind[] = [
  'worktree',
  'container',
  'k8s_pod',
  'vm',
  'exec',
  'subprocess',
];

export function isCanonicalKind(k: string | undefined): k is CanonicalWorkspaceKind {
  if (!k) return false;
  return (CANONICAL_KINDS as readonly string[]).includes(k);
}
