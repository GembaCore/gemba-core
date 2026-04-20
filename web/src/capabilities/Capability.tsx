import type { ReactNode } from 'react';
import { useCapabilities } from './context-internal';
import type { CapabilityFlag } from './types';

// Capability is the JSX gate the rest of the SPA wraps any adaptor-
// sensitive control in. gm-root DD-15 is categorical on this: controls for
// unsupported capabilities are HIDDEN, not disabled. A disabled button
// invites the operator to guess why it's disabled; a missing button says
// "this adaptor doesn't do that" with no ambiguity.
//
// Usage examples:
//
//   <Capability has="sprint_native">
//     <SprintLane />
//   </Capability>
//
//   <Capability hasField="story_points">
//     <StoryPointsColumn />
//   </Capability>
//
//   <Capability hasEdge="blocks">
//     <BlocksIndicator />
//   </Capability>
//
//   <Capability has="token_budget_enforced" fallback={<UnmeteredBadge />}>
//     <BudgetMeter />
//   </Capability>
//
// At most one of `has` / `hasField` / `hasEdge` / `hasRelField` should be
// provided; when multiple are set the component AND's them together (all
// must be satisfied).

export interface CapabilityProps {
  children: ReactNode;
  // has tests one of the boolean flags on the work-plane manifest.
  has?: CapabilityFlag;
  // hasField tests whether the active work-plane declares a field
  // extension with this name. Case-sensitive (JSON key equality).
  hasField?: string;
  // hasEdge tests whether the active work-plane declares an edge
  // extension with this name.
  hasEdge?: string;
  // hasRelField tests whether the active work-plane declares a
  // per-edge relationship extension field with this name.
  hasRelField?: string;
  // fallback renders when the capability is not met. Defaults to null
  // (render nothing) because the default posture is "hide".
  fallback?: ReactNode;
}

export function Capability({
  children,
  has,
  hasField,
  hasEdge,
  hasRelField,
  fallback = null,
}: CapabilityProps) {
  const { workPlane } = useCapabilities();

  if (!workPlane) {
    return <>{fallback}</>;
  }

  const checks: boolean[] = [];
  if (has !== undefined) {
    checks.push(Boolean(workPlane[has]));
  }
  if (hasField !== undefined) {
    checks.push((workPlane.field_extensions ?? []).some((f) => f.name === hasField));
  }
  if (hasEdge !== undefined) {
    checks.push((workPlane.edge_extensions ?? []).some((e) => e.name === hasEdge));
  }
  if (hasRelField !== undefined) {
    checks.push(
      (workPlane.relationship_extensions ?? []).some((r) => r.name === hasRelField),
    );
  }

  // No predicates means the gate is trivially false — it's a noop
  // wrapper and almost certainly a mistake.  Surface it as fallback
  // rather than silently rendering `children`.
  if (checks.length === 0) {
    return <>{fallback}</>;
  }

  const ok = checks.every(Boolean);
  return <>{ok ? children : fallback}</>;
}
