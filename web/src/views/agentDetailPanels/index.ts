// Public surface for the per-Workspace.kind detail panels (gm-e12.15).
// Pure re-exports — `AgentDetail` switches over Workspace.kind and
// renders one of these.

export { WorktreePanel } from './WorktreePanel';
export { ContainerPanel } from './ContainerPanel';
export { K8sPodPanel } from './K8sPodPanel';
export { VMPanel } from './VMPanel';
export { ExecPanel } from './ExecPanel';
export { SubprocessPanel } from './SubprocessPanel';
export { UnknownKindPanel } from './UnknownKindPanel';
export {
  CANONICAL_KINDS,
  isCanonicalKind,
  type AgentDetailPanelProps,
  type CanonicalWorkspaceKind,
} from './types';
