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

export type WorkspaceKind = 'worktree' | 'container' | 'k8s_pod' | 'vm' | 'exec' | 'subprocess';

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
  // read_only is set by adaptors that cannot service mutations (the
  // --dolt-url SQL connector). The SPA hides every edit affordance
  // when true rather than disabling them (gm-root DD-15).
  read_only?: boolean;
  // description_format declares the content type of WorkItem.Description
  // so the SPA's renderer registry can pick the right component
  // ("plain" → preformatted, "markdown" → react-markdown + GFM).
  // Undefined falls through to "plain" on the SPA side so an older
  // server without the field never crashes a newer client.
  description_format?: string;
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

// Re-export the generated Flags projection so the rest of the SPA can
// import it from the capabilities barrel without reaching into
// types/core.gen.ts directly. The Flags struct is the flat-boolean
// projection of WorkPlaneManifest — see internal/core/manifest.go for
// the derivation rules.
export type { Flags, FlagName } from '../types/core.gen';
export { FLAG_NAMES } from '../types/core.gen';
import type { Flags } from '../types/core.gen';

// CapabilityFlag is the accepted value for `<Capability has="...">`.
// It used to be a hand-rolled union of manifest keys ("sprint_native",
// ...), but the SPA now consumes the Flags projection from core so the
// UI speaks one vocabulary regardless of which backend is active
// (gm-cpk). Foolery-backend, VS Code, and other external consumers see
// the same flat-boolean surface.
export type CapabilityFlag = keyof Flags;

// workPlaneFlags derives the flat-boolean projection from a manifest.
// Pure and deterministic — the SPA calls it on every render rather than
// caching, to keep parity with the Go-side (m CapabilityManifest).Flags()
// contract.  Returns null when the manifest is null so callers can tell
// "no adaptor connected" apart from "adaptor says no".
export function workPlaneFlags(m: WorkPlaneManifest | null): Flags | null {
  if (!m) return null;
  return {
    has_sprints: m.sprint_native,
    has_budget: m.token_budget_enforced,
    has_evidence: m.evidence_synthesis_required,
    has_declared_state: Object.keys(m.state_map ?? {}).length > 0,
    has_extension_edges: (m.edge_extensions ?? []).length > 0,
    has_extension_fields: (m.field_extensions ?? []).length > 0,
    has_extension_rel_fields: (m.relationship_extensions ?? []).length > 0,
  };
}
