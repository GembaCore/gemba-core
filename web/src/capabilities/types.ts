// Capability-manifest types consumed by the SPA (gm-e11.4).
//
// These mirror the Go source of truth in internal/core/workplane.go
// (CapabilityManifest) and internal/core/orchestration.go
// (OrchestrationCapabilityManifest). Adaptors emit these at Describe
// time; the SPA reads them at mount and hides every control the active
// manifest does not declare (gm-root DD-15).
//
// Codegen parity: these aren't part of cmd/gen-core-types yet — when
// that generator grows to cover the full manifest surface, this file
// should be replaced by the generated artifact. Until then keep the
// shapes byte-for-byte in sync with the Go definitions; a backend
// vocabulary leak here defeats the point of capability negotiation.

export type Transport = 'api' | 'jsonl' | 'mcp';

export const TRANSPORTS: readonly Transport[] = ['api', 'jsonl', 'mcp'] as const;

export type StateCategory = 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled';

export type StateMap = Record<string, StateCategory>;

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

export interface WorkCapabilityManifest {
  adaptor_name: string;
  adaptor_version: string;
  protocol_version: string;
  transport: Transport;
  state_map: StateMap;
  edge_extensions?: EdgeExtension[];
  field_extensions?: FieldExtension[];
  relationship_extensions?: RelationshipExtension[];
  sprint_native: boolean;
  token_budget_enforced: boolean;
  evidence_synthesis_required: boolean;
}

export type WorkspaceKind = 'worktree' | 'container' | 'k8s_pod' | 'vm' | 'exec' | 'subprocess';

export type GroupMode = 'static' | 'pool' | 'graph';

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

export interface IsolationCapabilities {
  fs_scoped: boolean;
  net_isolated: boolean;
  cpu_limited: boolean;
  mem_limited: boolean;
  snapshot_restore: boolean;
}

export interface OrchestrationCapabilityManifest {
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

// Manifests is the envelope /api/capabilities returns. Either half MAY
// be null: that means "no adaptor is registered on this plane" — the UI
// should degrade to the minimal default (hide everything optional)
// rather than render sprint chrome, budget widgets, or peek modes a
// disconnected backend can't service.
export interface Manifests {
  work: WorkCapabilityManifest | null;
  orchestration: OrchestrationCapabilityManifest | null;
}

// The flags readable via <Capability has="..."> JSX gate. Keep this
// list closed: the whole point of the gate is that a typo must be a
// compile-time error, not a silently-always-hidden control.
export type CapabilityFlag =
  | 'sprint_native'
  | 'token_budget_enforced'
  | 'evidence_synthesis_required';

// OrchestrationCapabilityFlag mirrors CapabilityFlag for orchestration.
// Orchestration capability questions are usually "does this adaptor
// emit this escalation kind" or "does it support this peek mode",
// which are set-membership checks — those go through useHasEscalation
// / useHasPeekMode rather than this flag enum. The enum itself is a
// small escape hatch for boolean-shaped orchestration capabilities
// that may land later (e.g. "supports_structured_peek").
export type OrchestrationCapabilityFlag = never;
