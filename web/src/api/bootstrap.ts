// Typed data-access for /api/bootstrap (gm-uipx.7 — ui-spec §5.15).
//
// The /bootstrap wizard composes a project config across four steps:
// Source → Analysis → Plan review → Ratify. Each step has a typed
// wire shape so the SPA's progressive UX (live progress strings,
// PASS/WARN/FAIL drill-down, nonce-confirmed commit) is testable
// without the backend wired up.
//
// Backend handlers do NOT exist yet (verified against internal/server
// at gm-uipx.7 commit). This file is a typed STUB client intended to
// be wired to real handlers in a follow-up bead. Until then the e2e
// suite intercepts /api/bootstrap/** with page.route() (see
// testing/e2e/fixtures/server.ts dispatch).

import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';

// BootstrapSource — the four kinds of project bootstrap sources the
// wizard supports per ui-spec §5.15. Each tile maps 1:1 to one of
// these tokens; the analysis backend dispatches on the value.
export type BootstrapSource = 'jira' | 'beads' | 'source-code' | 'fresh';

export interface BootstrapStartRequest {
  source: BootstrapSource;
  // Free-form payload the operator may need to attach to the source
  // (Jira board id, Beads database name, source-code path, etc.). The
  // SPA collects this in Step 1 and the backend interprets it per
  // source. Open shape so the SPA can ship before the backend pins
  // the per-source schema.
  config?: Record<string, unknown>;
}

export interface BootstrapStartResponse {
  // analysis_id is returned by the start endpoint and used to pin the
  // long-poll progress endpoint and the plan-fetch endpoint to the
  // same backend in-flight task. Opaque to the SPA.
  analysis_id: string;
}

// BootstrapProgress is one frame of analysis progress. The backend
// emits these in order; the SPA renders the tail of the list as a
// streaming log. `done=true` marks the last frame and unblocks
// Step 3.
export interface BootstrapProgress {
  // seq is monotonically increasing per analysis_id. The poll loop
  // sends ?since=<lastSeq> so the server can return only new frames.
  seq: number;
  // line is a human-readable progress string ("Scanning 247 issues…").
  line: string;
  // at is an ISO8601 timestamp the SPA renders next to the line.
  at: string;
  // done is true on the final frame; the SPA stops polling and
  // advances to Step 3.
  done?: boolean;
}

export interface BootstrapProgressResponse {
  frames: BootstrapProgress[];
}

// BootstrapPlanItem is one row in the generated plan (epics +
// milestones + sprints). Shape stays open for now so the SPA can
// render whatever the backend emits without the wire pinning to
// over-specific Go types.
export interface BootstrapPlanItem {
  id: string;
  kind: 'epic' | 'milestone' | 'sprint' | 'story';
  title: string;
  // detail is the operator-readable description that lands under the
  // row's "expand" toggle.
  detail?: string;
  // children form the two-level outline that the plan renders.
  children?: BootstrapPlanItem[];
}

// BootstrapConsistencyFinding is one row in the consistency report.
// PASS / WARN / FAIL drives the row's tone + the top-line summary.
export type BootstrapFindingLevel = 'pass' | 'warn' | 'fail';

export interface BootstrapConsistencyFinding {
  id: string;
  level: BootstrapFindingLevel;
  // title is the one-line headline ("Sprint 3 has no owner").
  title: string;
  // detail is the operator drill-down that lands under the row's
  // expand toggle.
  detail?: string;
}

// BootstrapPlan is the Step-3 fetch payload. The plan renders left,
// the consistency report renders right; the SPA totals findings at
// the top of the report (PASS=12 / WARN=2 / FAIL=0).
export interface BootstrapPlan {
  analysis_id: string;
  source: BootstrapSource;
  plan: BootstrapPlanItem[];
  findings: BootstrapConsistencyFinding[];
}

// BootstrapCommitResponse is the Step-4 ratify payload. project_path
// surfaces in the success toast so the operator knows where the
// config landed. board_url is the permanent post-ratify destination.
export interface BootstrapCommitResponse {
  project_path: string;
  board_url: string;
}

// startBootstrap — POST /api/bootstrap/start. Backend kicks off an
// analysis task and returns the opaque id used to scope progress +
// plan fetches. Cancel before Step 4 is a no-op on the backend; the
// task self-terminates after its progress queue drains.
export async function startBootstrap(
  body: BootstrapStartRequest
): Promise<BootstrapStartResponse> {
  return apiFetch<BootstrapStartResponse>('/bootstrap/start', {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
}

// pollBootstrapProgress — GET /api/bootstrap/progress?analysis_id&since.
// Long-poll style: callers pass the highest seq they've seen and get
// any newer frames. When the backend emits a frame with done=true the
// client stops polling.
export async function pollBootstrapProgress(
  analysisId: string,
  since: number
): Promise<BootstrapProgress[]> {
  if (!analysisId) throw new Error('pollBootstrapProgress: analysisId is required');
  const qs = new URLSearchParams({
    analysis_id: analysisId,
    since: String(since),
  });
  const env = await apiFetch<BootstrapProgressResponse>(`/bootstrap/progress?${qs.toString()}`);
  return env.frames ?? [];
}

// fetchBootstrapPlan — GET /api/bootstrap/plan?analysis_id. Available
// after the analysis completes; before that the backend returns 404
// with code "bootstrap_pending".
export async function fetchBootstrapPlan(analysisId: string): Promise<BootstrapPlan> {
  if (!analysisId) throw new Error('fetchBootstrapPlan: analysisId is required');
  const qs = new URLSearchParams({ analysis_id: analysisId });
  return apiFetch<BootstrapPlan>(`/bootstrap/plan?${qs.toString()}`);
}

// commitBootstrap — POST /api/bootstrap/commit. Nonce-gated through
// the shared X-GEMBA-Confirm header so an operator double-click can
// not double-write project config. The backend persists
// .gemba/project.toml + the seed bead database and returns the SPA's
// post-ratify destination.
export async function commitBootstrap(
  analysisId: string,
  opts: { nonce?: string } = {}
): Promise<BootstrapCommitResponse> {
  if (!analysisId) throw new Error('commitBootstrap: analysisId is required');
  return apiFetch<BootstrapCommitResponse>('/bootstrap/commit', {
    method: 'POST',
    body: JSON.stringify({ analysis_id: analysisId }),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
