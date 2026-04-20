import { useContext } from 'react';
import { CapabilitiesContext, type CapabilitiesContextValue } from './context-value';
import type { CapabilityFlag, EscalationKind, PeekMode, Transport, WorkspaceKind } from './types';

// useCapabilities is the canonical reader — every other hook layers
// on top of it. Lives here (not in context.tsx) so the .tsx files can
// keep their component-only exports for fast-refresh.
export function useCapabilities(): CapabilitiesContextValue {
  return useContext(CapabilitiesContext);
}

// useHasCapability is the hook form of the JSX <Capability> gate, for
// cases where children can't cleanly represent the gated work (column
// definitions, filter options, table accessors). Keep the JSX
// component the default; reach for the hook only when the render tree
// is data-driven.
export function useHasCapability(flag: CapabilityFlag): boolean {
  const { manifests } = useCapabilities();
  return Boolean(manifests.work?.[flag]);
}

export function useHasEscalation(kind: EscalationKind): boolean {
  const { manifests } = useCapabilities();
  return manifests.orchestration?.escalation_kinds.includes(kind) ?? false;
}

export function useHasPeekMode(mode: PeekMode): boolean {
  const { manifests } = useCapabilities();
  return manifests.orchestration?.peek_modes.includes(mode) ?? false;
}

export function useHasWorkspaceKind(kind: WorkspaceKind): boolean {
  const { manifests } = useCapabilities();
  return manifests.orchestration?.workspace_kinds.includes(kind) ?? false;
}

// useTransport reads the live transport for a given plane. Returns
// null when no adaptor is bound on that plane, which callers should
// treat as "hide the control rather than guess a transport".
export type Plane = 'work' | 'orchestration';

export function useTransport(plane: Plane): Transport | null {
  const { manifests } = useCapabilities();
  return (
    (plane === 'work' ? manifests.work?.transport : manifests.orchestration?.transport) ?? null
  );
}
