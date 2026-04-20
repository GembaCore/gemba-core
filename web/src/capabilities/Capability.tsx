import type { ReactNode } from 'react';
import { useCapabilities } from './hooks';
import type { CapabilityFlag, EscalationKind, PeekMode, WorkspaceKind } from './types';

// <Capability has="sprint_native"> is the canonical JSX gate (gm-e11.4).
// It renders children only when the active WorkCapabilityManifest
// declares the flag. Null manifest ⇒ gate off — the UI hides controls
// the backend has not opted into, it never disables them (gm-root DD-15).
//
// Prefer this component over `useCapabilities()` spaghetti in render;
// the declarative form keeps the "is this feature on?" question next to
// the JSX that depends on the answer.

type BaseProps = {
  children: ReactNode;
  fallback?: ReactNode;
};

type FlagProps = BaseProps & {
  has: CapabilityFlag;
  missing?: never;
  escalation?: never;
  peek?: never;
  workspace?: never;
};

type MissingProps = BaseProps & {
  missing: CapabilityFlag;
  has?: never;
  escalation?: never;
  peek?: never;
  workspace?: never;
};

type EscalationProps = BaseProps & {
  escalation: EscalationKind;
  has?: never;
  missing?: never;
  peek?: never;
  workspace?: never;
};

type PeekProps = BaseProps & {
  peek: PeekMode;
  has?: never;
  missing?: never;
  escalation?: never;
  workspace?: never;
};

type WorkspaceProps = BaseProps & {
  workspace: WorkspaceKind;
  has?: never;
  missing?: never;
  escalation?: never;
  peek?: never;
};

export type CapabilityProps =
  | FlagProps
  | MissingProps
  | EscalationProps
  | PeekProps
  | WorkspaceProps;

export function Capability(props: CapabilityProps): JSX.Element {
  const { manifests } = useCapabilities();
  const { children, fallback = null } = props;

  let on = false;

  if ('has' in props && props.has) {
    on = Boolean(manifests.work?.[props.has]);
  } else if ('missing' in props && props.missing) {
    on = !manifests.work?.[props.missing];
  } else if ('escalation' in props && props.escalation) {
    on = manifests.orchestration?.escalation_kinds.includes(props.escalation) ?? false;
  } else if ('peek' in props && props.peek) {
    on = manifests.orchestration?.peek_modes.includes(props.peek) ?? false;
  } else if ('workspace' in props && props.workspace) {
    on = manifests.orchestration?.workspace_kinds.includes(props.workspace) ?? false;
  }

  return <>{on ? children : fallback}</>;
}
