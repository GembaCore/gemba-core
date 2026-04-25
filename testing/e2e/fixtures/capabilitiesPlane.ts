// fixtures/capabilitiesPlane.ts (gm-5v8v.7 / gm-5v8v.8 follow-up)
//
// Per-test seed for the /api/capabilities envelope. Specs that need
// the SPA to see a particular WorkPlane / OrchestrationPlane manifest
// (e.g. graph extension edges, AgentDetailDrawer transcript pane,
// EpicDrawer Dispatch button) call capabilitiesPlane.set({...})
// before navigating.
//
// Default is the same `{work_plane: null, orchestration_plane: null}`
// envelope the previous handler synthesised — specs that don't care
// keep the empty/no-adaptor view.
//
// Wire shape mirrors web/src/capabilities/types.ts CapabilitiesResponse
// minus the regenerated dependency on core.gen — we re-declare the
// minimal subset here so the fixture stays decoupled from the SPA's
// alias paths.

import type {
  WorkPlaneManifest,
  OrchestrationManifest,
  CapabilitiesResponse,
} from '../../../web/src/capabilities/types';

export interface CapabilitiesPlane {
  set(envelope: Partial<CapabilitiesResponse>): void;
  setWorkPlane(manifest: WorkPlaneManifest | null): void;
  setOrchestrationPlane(manifest: OrchestrationManifest | null): void;
  get(): CapabilitiesResponse;
  clear(): void;
}

const EMPTY: CapabilitiesResponse = {
  work_plane: null,
  orchestration_plane: null,
};

export function createCapabilitiesPlane(): CapabilitiesPlane {
  let state: CapabilitiesResponse = { ...EMPTY };
  return {
    set(envelope) {
      state = { ...state, ...envelope };
    },
    setWorkPlane(manifest) {
      state = { ...state, work_plane: manifest };
    },
    setOrchestrationPlane(manifest) {
      state = { ...state, orchestration_plane: manifest };
    },
    get() {
      return state;
    },
    clear() {
      state = { ...EMPTY };
    },
  };
}

// ── Builders ─────────────────────────────────────────────────────────
//
// Convenience factories for the manifests specs reach for most. Each
// returns a fully populated record so a test seeding `workPlaneManifest()`
// gets a manifest that satisfies every required field; overrides
// patch on top for the specific fields the test cares about.

export function workPlaneManifest(
  overrides: Partial<WorkPlaneManifest> = {}
): WorkPlaneManifest {
  return {
    adaptor_name: 'beads',
    adaptor_version: 'fake-1.0.0',
    protocol_version: '1.0.0',
    transport: 'api',
    state_map: {
      open: 'unstarted',
      in_progress: 'started',
      closed: 'completed',
    },
    sprint_native: false,
    token_budget_enforced: false,
    evidence_synthesis_required: false,
    description_format: 'markdown',
    ...overrides,
  };
}

export function orchestrationManifest(
  overrides: Partial<OrchestrationManifest> = {}
): OrchestrationManifest {
  return {
    adaptor_id: 'fake-orchestrator',
    adaptor_version: 'fake-1.0.0',
    orchestration_api_version: '1.0.0',
    transport: 'api',
    workspace_kinds: ['worktree'],
    group_modes: ['static'],
    cost_axes: ['tokens'],
    escalation_kinds: ['hitl_approval'],
    peek_modes: [],
    ...overrides,
  };
}
