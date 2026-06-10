// Planner API client (gm-s47n.6.1).
//
// Wraps GET /api/planner/coach — the single endpoint the
// coach-mode page consumes. Server-side composition keeps the
// SPA free of conflict / affinity math; we just render.

import { apiFetch } from './client';

// Wire shapes mirror internal/server/planner_coach.go. Keep in sync
// when the server response grows fields — fields the SPA doesn't
// read can be added freely; renaming or removing is a breaking
// change.

export interface PlannerOperationalContext {
  session?: {
    id: string;
    assignment_id: string;
    agent_id: string;
    status: string;
    started_at: string;
    last_heartbeat?: string | null;
  } | null;
  agent?: {
    id: string;
    name: string;
    agent_kind: string;
    role?: string;
    workspace?: string;
  } | null;
  workspace?: {
    id: string;
    kind: string;
    repository?: string;
    branch?: string;
    worktree_path?: string;
    isolation?: {
      fs_scoped?: boolean;
      net_isolated?: boolean;
      cpu_limited?: boolean;
      mem_limited?: boolean;
      snapshot_restore?: boolean;
    };
  } | null;
  scope_status?: {
    git?: {
      state: 'clean' | 'dirty' | 'unavailable' | string;
      changed_files?: number;
      ahead?: number;
      behind?: number;
      head_sha?: string;
      upstream?: string;
      reason?: string;
    } | null;
    analysis?: {
      backend?: string;
      state: 'current' | 'stale' | 'missing' | 'unavailable' | string;
      indexed_at?: string;
      indexed_commit?: string;
      head_sha?: string;
      reason?: string;
    } | null;
  } | null;
  profile?: {
    session_id: string;
    concepts?: Record<string, number>;
    files?: Record<string, number>;
    context_pct?: number;
    last_beads?: string[];
  } | null;
  health?: {
    context_pressure: number;
    concept_drift: number;
    time_on_task_ns: number;
  } | null;
}

export interface PlannerReadyBead {
  id: string;
  title: string;
  kind: string;
  status: string;
  state_category: string;
  concepts?: string[];
  targets?: string[];
  repository?: string;
  branch?: string;
}

export interface PlannerEdgeReason {
  Kind: number;
  Detail?: string;
}

export interface PlannerConflictEdge {
  From: string;
  To: string;
  Reasons: PlannerEdgeReason[];
}

export interface PlannerWorkspaceCollision {
  a?: string;
  b: string;
  reason: string;
  live_session_id?: string;
}

export interface PlannerSemanticConflict {
  a: string;
  b: string;
  reason: string;
}

export interface PlannerAffinityScores {
  concept_overlap: number;
  file_familiarity: number;
  workspace_match: number;
  recency: number;
  headroom: number;
  combined: number;
}

export interface PlannerAffinityRow {
  bead_id: string;
  session_id: string;
  agent_id?: string;
  scores: PlannerAffinityScores;
}

export interface PlannerBatch {
  Beads: string[];
}

export interface PlannerCoachResponse {
  sessions: PlannerOperationalContext[];
  ready_beads: PlannerReadyBead[];
  conflicts: PlannerConflictEdge[];
  workspace: PlannerWorkspaceCollision[];
  semantic: PlannerSemanticConflict[];
  affinity: PlannerAffinityRow[];
  batches: PlannerBatch[];
  notices?: string[];
  // selection is the per-(bead, session) Selection.Result the
  // server emits when WP2.0 Layer 5 is wired (gm-v5z2.7). Optional
  // because the field landed after the rest of the response shape;
  // SPA components branch on its presence to render the
  // Justification + dispatch_status badge.
  selection?: PlannerSelectionResult[];
}

// PlannerSessionIntent mirrors planner/intent.Intent. Surfaces the
// operator's pinned focus directive on the agent context strip
// (gm-v5z2.9). Empty restrictors mean "no focus active."
export interface PlannerSessionIntent {
  session_id: string;
  epic_id?: string;
  label?: string;
  bead_id_regex?: string;
  rationale?: string;
  demotion_factor?: number;
}

// PlannerRunway is the per-session runway snapshot (gm-v5z2.4).
// Bucket maps to the same enum as bead.estimated_size so the
// runway gate is a one-line comparison.
export interface PlannerRunway {
  bucket: 'small' | 'medium' | 'large';
  score: number;
  drivers: {
    context_pressure: number;
    concept_drift: number;
    calibration: number;
    headroom: number;
    drift_penalty: number;
  };
}

// PlannerSelectionResult mirrors planner/selection.Result —
// per-(bead, session) gate outcome + composed score + the
// Justification slice the SPA renders verbatim under each cell.
export interface PlannerSelectionResult {
  bead_id: string;
  session_id?: string;
  outcome: 'dispatchable' | 'rejected';
  reason?: string; // gate name for rejected
  score: number;
  components: {
    affinity: number;
    affinity_full?: PlannerAffinityScores;
    leverage: number;
    leverage_weight: number;
    epic_affinity: number;
    pre_gate_score: number;
    runway_demoted?: boolean;
    intent_demoted?: boolean;
    fairness_boost?: number;
  };
  justification: string[];
}

export async function getCoach(): Promise<PlannerCoachResponse> {
  return apiFetch<PlannerCoachResponse>('/planner/coach');
}
