// Capability-negotiation public surface (gm-e11.4).
//
// Consumers should import from '@/capabilities' rather than reaching
// into individual files — the module boundary is what lets us swap the
// transport layer or regenerate types without touching every call site.

export type {
  CapabilityFlag,
  CostAxis,
  EdgeExtension,
  EscalationKind,
  EventDelivery,
  FieldExtension,
  GroupMode,
  IsolationCapabilities,
  Manifests,
  OrchestrationCapabilityManifest,
  PeekMode,
  RelationshipExtension,
  StateMap,
  Transport,
  WorkCapabilityManifest,
  WorkspaceKind,
} from './types';

export { TRANSPORTS } from './types';

export { CapabilitiesProvider, CapabilitiesTestProvider } from './context';

export {
  useCapabilities,
  useHasCapability,
  useHasEscalation,
  useHasPeekMode,
  useHasWorkspaceKind,
  useTransport,
} from './hooks';

export type { Plane } from './hooks';

export { Capability } from './Capability';
export type { CapabilityProps } from './Capability';

export { FieldExtensions } from './extensions';

export {
  clearFieldRegistry,
  lookupFieldRenderer,
  registerFieldRenderer,
} from './extension-registry';

export type { FieldRenderer, FieldRendererProps } from './extension-registry';

export { endpointFor } from './transport';
