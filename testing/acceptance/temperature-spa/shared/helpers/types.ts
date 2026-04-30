// Shared types for the temperature-spa acceptance suite.
//
// AcceptanceResults is the running collection the milestone steps
// append to over the lifetime of one variant run. The report writer
// (gm-root.27.18) and bug-filing helper (gm-root.27.19) read from
// this; ProjectHandle (gm-root.27.3, in ./bootstrap.ts) carries the
// runtime gemba server pointers that those helpers also accept.

export type Variant = 'native' | 'gastown';
export type AgentMode = 'mock' | 'real';

export type MilestoneID = 'M1' | 'M2' | 'M3' | 'triage';

export type BeadState =
  | 'open'
  | 'ready'
  | 'in_progress'
  | 'blocked'
  | 'closed'
  | 'deferred';

export interface BeadTransition {
  beadID: string;
  from: BeadState | null;
  to: BeadState;
  at: string; // ISO timestamp
}

export interface MilestoneTiming {
  id: MilestoneID;
  startedAt: string;
  endedAt: string;
  durationMs: number;
}

export interface EscalationRecord {
  id: string;
  injectedAt: string;
  resolvedAt?: string;
  durationMs?: number;
  resolved: boolean;
}

export interface AssertionOutcome {
  // Convention: prefix with "<milestone>:" so the report can group
  // by milestone without an extra schema field.
  name: string;
  passed: boolean;
  expected?: unknown;
  got?: unknown;
  message?: string;
  // Path to a screenshot Playwright captured on failure, if any.
  screenshot?: string;
  // Stack trace from the failing assertion. Captured by the helper
  // wrapper so report/bug-filing share the exact text.
  stack?: string;
  // Bead the bug-filing helper opened for this failure, if it ran.
  filedBeadID?: string;
}

// AcceptanceResults — running collection appended by the milestone
// steps over the lifetime of one variant run. The variant + agent +
// gemba SHA fields live here (rather than on ProjectHandle) so
// reporting works without coupling to the bootstrap helper.
export interface AcceptanceResults {
  variant: Variant;
  agentMode: AgentMode;
  // Rig the variant ran against. Native uses 'local'; gastown sets
  // this to the per-run rig name created via the UI.
  rig: string;
  // Gemba commit SHA the test exercised. Captured at start so a
  // failure bead has a definite reproducer pointer.
  gembaCommitSHA: string;
  // Target SPA project SHA when the spec records one (some variants
  // generate the target so this stays empty).
  targetProjectSHA?: string;
  startedAt: string;
  endedAt?: string;
  beadTransitions: BeadTransition[];
  milestones: MilestoneTiming[];
  escalations: EscalationRecord[];
  assertions: AssertionOutcome[];
  // Beads .19 created during this run, in order of creation.
  filedBugs: string[];
  // Set when the test exits via panic / fatal — used by the report
  // header to mark the run as crashed.
  fatalError?: { message: string; stack?: string };
}
