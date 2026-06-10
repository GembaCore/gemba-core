// Public barrel for the capabilities layer. See context.tsx / Capability.tsx
// for the two entry points most of the SPA uses:
//
//   <CapabilitiesProvider>                — app-root setup
//   <Capability has="has_sprints" />      — JSX gate around adaptor controls
//   <FieldExtensionSlot name="..." />     — adaptor-namespaced custom fields
//   useCapabilities()                     — imperative access to the manifest
//   resolveTransport(manifest.transport)  — transport-aware URL building
//
// Extension widgets register themselves at module load via
// registerFieldExtension / registerEdgeExtension / registerRelFieldExtension.

export { CapabilitiesProvider } from './context';
export type { CapabilitiesProviderProps } from './context';
export { useCapabilities } from './context-internal';
export type { CapabilityState } from './context-internal';
export { Capability } from './Capability';
export type { CapabilityProps } from './Capability';
export { FieldExtensionSlot, EdgeExtensionSlot, RelFieldExtensionSlot } from './Extension';
export type {
  FieldExtensionSlotProps,
  EdgeExtensionSlotProps,
  RelFieldExtensionSlotProps,
} from './Extension';
export {
  registerFieldExtension,
  registerEdgeExtension,
  registerRelFieldExtension,
  resolveFieldExtension,
  resolveEdgeExtension,
  resolveRelFieldExtension,
} from './extension-registry';
export type {
  FieldExtensionProps,
  EdgeExtensionProps,
  RelFieldExtensionProps,
} from './extension-registry';
export {
  resolveTransport,
  workPlaneTransport,
  orchestrationTransport,
  routePath,
  API_BASE,
  EVENTS_BASE,
} from './transport';
export type { TransportInfo } from './transport';
export type {
  CapabilitiesResponse,
  WorkPlaneManifest,
  OrchestrationManifest,
  CapabilityFlag,
  Flags,
  FlagName,
  Transport,
  StateCategory,
  GroupMode,
  WorkspaceKind,
  CostAxis,
  EscalationKind,
  PeekMode,
  AssignmentStrategy,
  EventDelivery,
  FieldExtension,
  EdgeExtension,
  RelationshipExtension,
  IsolationCapabilities,
} from './types';
export { FLAG_NAMES, workPlaneFlags } from './types';
