// Mirrors of the Go CapabilityManifest + OrchestrationCapabilityManifest
// shapes in internal/core. Kept by hand (not codegen) because only the
// manifest subset the UI actually negotiates on belongs here — the full
// set of adaptor types lives in types/core.gen.ts for the data models.
//
// When the manifest wire format changes on the Go side, update these
// mirrors AND the snapshot tests in __tests__/Capability.test.tsx.

export type Transport = 'api' | 'jsonl' | 'mcp';

export type StateCategory = 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled';

export type GroupMode = 'static' | 'pool' | 'graph';

export type WorkspaceKind =
  | 'worktree'
  | 'container'
  | 'k8s_pod'
  | 'vm'
  | 'exec'
  | 'subprocess';

export type CostAxis = 'tokens' | 'wallclock' | 'dollars_native';

export type EscalationKind =
  | 'mcp_elicitation'
  | 'a2a_input_required'
  | 'permission_prompt'
  | 'hitl_approval'
  | 'orchestrator_pause';

export type PeekMode = 'transcript' | 'screenshot' | 'structured';

export type AssignmentStrategy = 'push' | 'pull' | 'hook';

export type EventDelivery = 'sse' | 'push' | 'poll';

export interface FieldExtension {
  name: string;
  type: string;
  description?: string;
}

export interface EdgeExtension {
  name: string;
  directed: boolean;
  inverse?: string;
  description?: string;
}

export interface RelationshipExtension {
  name: string;
  type: string;
  description?: string;
}

export interface IsolationCapabilities {
  fs_scoped: boolean;
  net_isolated: boolean;
  cpu_limited: boolean;
  mem_limited: boolean;
  snapshot_restore: boolean;
}

export interface WorkPlaneManifest {
  adaptor_name: string;
  adaptor_version: string;
  protocol_version: string;
  transport: Transport;
  state_map: Record<string, StateCategory>;
  edge_extensions?: EdgeExtension[];
  field_extensions?: FieldExtension[];
  relationship_extensions?: RelationshipExtension[];
  sprint_native: boolean;
  token_budget_enforced: boolean;
  evidence_synthesis_required: boolean;
}

export interface OrchestrationManifest {
  adaptor_id: string;
  adaptor_version: string;
  orchestration_api_version: string;
  transport: Transport;
  workspace_kinds: WorkspaceKind[];
  default_workspace_kind?: WorkspaceKind;
  per_kind_isolation?: Partial<Record<WorkspaceKind, IsolationCapabilities>>;
  group_modes: GroupMode[];
  assignment_strategies?: AssignmentStrategy[];
  cost_axes: CostAxis[];
  native_cost_unit?: string;
  native_cost_to_dollars?: number;
  escalation_kinds: EscalationKind[];
  peek_modes: PeekMode[];
  event_delivery?: EventDelivery;
  extension?: Record<string, unknown>;
}

// CapabilitiesResponse is what /api/capabilities returns. Both planes are
// optional at the envelope level — before adaptors are wired up, the server
// returns nulls rather than synthesising placeholders, and the UI renders
// a conservative "no capability" state.
export interface CapabilitiesResponse {
  work_plane: WorkPlaneManifest | null;
  orchestration_plane: OrchestrationManifest | null;
}

// CapabilityFlag names the boolean feature groups on the work-plane
// manifest that the Capability gate component accepts in its `has` prop.
// Enumerating them here (rather than accepting any string) lets TypeScript
// catch typos like `<Capability has="sprint_natiive">`.
export type CapabilityFlag =
  | 'sprint_native'
  | 'token_budget_enforced'
  | 'evidence_synthesis_required';
