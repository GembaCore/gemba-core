// Typed data-access for /api/v1/newproject (gm-root.17.3 — see
// docs/design/newproject.md).
//
// The /new SPA route runs a conversational project-creation flow
// against the `newproject` skill. The skill (gm-root.17.5) and the
// Onboarder persona host (gm-root.17.10) do NOT exist yet — this
// module is a typed STUB client. Until the backend lands the e2e
// suite intercepts /api/v1/newproject/** in
// testing/e2e/fixtures/server.ts and the SPA route renders + commits
// against the canned fake.
//
// Contract (per design doc):
//
//   POST /api/v1/newproject/start         → mint session id
//   POST /api/v1/newproject/:id/turn      → submit operator message,
//                                            return updated state +
//                                            skill reply
//   POST /api/v1/newproject/:id/ratify    → nonce-confirmed atomic
//                                            commit
//
// One-shot persistence: state lives only in server memory. Refresh /
// disconnect / restart loses the session. The SPA does NOT autosave
// or attempt to resume — by design.

import { apiFetch } from './client';
import { CONFIRM_HEADER } from './workItems';

// ── Output schema (mirrors the Go side in docs/design/newproject.md) ──
//
// The skill emits a fully-populated plan tree. Every field is
// populated; empty strings, empty slices, and Estimate=0 are valid
// empty states. The "no missing keys" contract makes ratification
// total.

export interface DraftBead {
  Title: string;
  Description: string;
  // "task" | "bug" | "feature" | "chore" — open string at the wire so
  // future bead kinds don't break the SPA.
  Type: string;
  Acceptance: string;
  Labels: string[];
  Priority: number;
  Estimate: number;
  Skills: string[];
  DesignNotes: string;
  Notes: string;
  // Intra-tree refs (e.g. "milestone:0/epic:1/bead:2"). Resolved to
  // real bd-… ids during ratification.
  DependsOnRefs: string[];
  BlocksRefs: string[];
}

export interface DraftEpic {
  Title: string;
  Description: string;
  Acceptance: string;
  Labels: string[];
  Priority: number;
  Estimate: number;
  Skills: string[];
  DesignNotes: string;
  Notes: string;
  Beads: DraftBead[];
}

export interface DraftMilestone {
  Title: string;
  Description: string;
  Acceptance: string;
  Labels: string[];
  Priority: number;
  Estimate: number;
  Skills: string[];
  DesignNotes: string;
  Notes: string;
  Epics: DraftEpic[];
}

// ChangeRef points into the plan tree for the most-recent skill edit
// (used by the diff surfacing in the conversation pane). Open shape —
// the skill emits whatever granularity it needs and the SPA renders
// the path verbatim.
export interface ChangeRef {
  // path is a slash-joined path like "milestone:1/epic:0/bead:2".
  // Empty when the change spans the whole tree (e.g. a fresh draft).
  path: string;
  // kind labels what changed (added | removed | renamed | edited).
  // The SPA tone-codes the diff badge by kind.
  kind: 'added' | 'removed' | 'renamed' | 'edited' | '';
  // summary is a human-readable one-liner ("Renamed milestone 2 to
  // 'OSS-ready'"). Empty when the skill didn't synthesize one.
  summary: string;
}

// NewProjectState is the authoritative server-side state for the
// session. Each turn mutates the state in place; the Ratify button
// commits whatever the state is at click time.
export interface NewProjectState {
  ProjectName: string;
  Description: string;
  TechStack: string[];
  // Free-form architecture notes captured from the operator.
  Architecture: string;
  Milestones: DraftMilestone[];
  // DraftProjectMD is the running synthesis the skill keeps current
  // each turn. Ratification persists this verbatim as docs/project.md.
  DraftProjectMD: string;
  Turn: number;
  LastChange: ChangeRef;
}

// EMPTY_STATE is the initial state the SPA renders before the first
// turn lands. It mirrors the "no missing keys" contract — every field
// is present, just empty.
export const EMPTY_STATE: NewProjectState = {
  ProjectName: '',
  Description: '',
  TechStack: [],
  Architecture: '',
  Milestones: [],
  DraftProjectMD: '',
  Turn: 0,
  LastChange: { path: '', kind: '', summary: '' },
};

// ── Conversation transcript ─────────────────────────────────────────

// ConversationMessage is one turn in the message history. The skill
// emits 'assistant' messages (with optional diff metadata in the
// state's LastChange); the operator emits 'user' messages.
export interface ConversationMessage {
  // Stable id so React lists key cleanly even when the same skill
  // reply is re-rendered between turns.
  id: string;
  role: 'user' | 'assistant';
  // Display content. The SPA renders this as plain text in v1; future
  // iterations may upgrade to markdown once the skill emits structured
  // bullets.
  content: string;
  // ISO8601 — server-supplied for assistant messages, client-supplied
  // for user messages.
  at: string;
}

// ── Wire envelopes ──────────────────────────────────────────────────

export interface StartNewProjectResponse {
  // Opaque session id used to scope subsequent /turn and /ratify
  // calls. Lives only in server memory.
  session_id: string;
  // Initial state — empty per EMPTY_STATE on day 0, but the wire
  // shape leaves room for the skill to seed defaults later.
  state: NewProjectState;
  // Optional greeting from the skill. Empty string on day 0.
  greeting: string;
}

export interface TurnRequest {
  // Operator message. Free text — the skill parses intent.
  message: string;
  // Optional in-place edits the operator made to the plan preview
  // pane. The skill sees these as "the operator already changed X" so
  // its next turn doesn't try to re-derive them. Open shape — the
  // skill interprets the path-keyed diff.
  edits?: Record<string, unknown>;
}

export interface TurnResponse {
  // Updated full state. The wire is authoritative — the SPA replaces
  // its local copy each turn rather than merging diffs.
  state: NewProjectState;
  // Skill reply for the conversation pane. Empty when the skill
  // chooses to update the plan silently (rare).
  reply: string;
  // Server-assigned message id for the reply (so the SPA's transcript
  // can dedupe + key without re-deriving).
  reply_id: string;
  // Server-supplied timestamp for the reply.
  reply_at: string;
}

export interface RatifyRequest {
  // Snapshot of the state the operator confirmed in the modal. The
  // server compares this against the live session state and rejects
  // mismatches so a stale modal can't commit a state the operator
  // didn't see.
  state: NewProjectState;
}

export interface RatifyResponse {
  // Filesystem path of the new project root
  // (default_dir/<project-name>/).
  project_path: string;
  // Human-readable project name — echoed back by the server so the
  // handoff screen can confirm which project was created without
  // reading it from local state.
  project_name: string;
  // Milestone and epic counts — seeded by the ratification transaction
  // so the handoff screen can show "seeded N milestones and M epics"
  // without re-querying the new workspace's beads database. Both 0
  // for the lightweight create path that bypasses the conversational
  // flow (gm-root.17.13).
  milestone_count: number;
  epic_count: number;
}

// gm-root.17.13 — lightweight project creation. The body is two
// fields: a project name and an optional description. The server
// scaffolds the dir + git + .gemba + beads DB with no plan tree
// (empty Milestones[]). No Onboarder, no LLM. The same
// X-GEMBA-Confirm nonce gate the conversational /ratify uses
// applies; the response shape is the same RatifyResponse so the
// handoff screen treats both paths uniformly.
export interface CreateProjectRequest {
  project_name: string;
  description: string;
}

// gm-root.17.13 — Onboarder availability probe. The board's empty
// state reads this to decide whether to render "Plan with the
// Onboarder →". 200 in both available + unavailable cases; the
// `available` flag is the operative signal.
export interface OnboarderProbeResponse {
  available: boolean;
  reason?: string;
}

// ── Client functions ────────────────────────────────────────────────

// startNewProject — POST /api/v1/newproject/start. Mints a fresh
// session id and returns the initial state. No request body in v1
// (the skill kicks off the conversation with a generic greeting); a
// future iteration may pass a seed prompt.
export async function startNewProject(): Promise<StartNewProjectResponse> {
  return apiFetch<StartNewProjectResponse>('/v1/newproject/start', {
    method: 'POST',
    body: '{}',
    headers: { 'Content-Type': 'application/json' },
  });
}

// submitTurn — POST /api/v1/newproject/:id/turn. Submits an operator
// message (and any in-place plan edits) and returns the updated state
// plus the skill reply.
export async function submitTurn(
  sessionId: string,
  body: TurnRequest
): Promise<TurnResponse> {
  if (!sessionId) throw new Error('submitTurn: sessionId is required');
  return apiFetch<TurnResponse>(`/v1/newproject/${encodeURIComponent(sessionId)}/turn`, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Content-Type': 'application/json' },
  });
}

// ratifyNewProject — POST /api/v1/newproject/:id/ratify. Nonce-gated
// through the shared X-GEMBA-Confirm header so a double-click on the
// confirm button can't double-write the project. The server runs the
// atomic transaction described in the design doc and returns the SPA's
// post-ratify destination.
export async function ratifyNewProject(
  sessionId: string,
  body: RatifyRequest,
  opts: { nonce?: string } = {}
): Promise<RatifyResponse> {
  if (!sessionId) throw new Error('ratifyNewProject: sessionId is required');
  return apiFetch<RatifyResponse>(`/v1/newproject/${encodeURIComponent(sessionId)}/ratify`, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

export function freshNonce(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `nonce-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

// createProject — POST /api/v1/newproject/create. Lightweight path
// that scaffolds a project (dir + git init + .gemba + beads DB +
// initial commit) with no plan tree, no Onboarder, no LLM.
// gm-root.17.13.
export async function createProject(
  body: CreateProjectRequest,
  opts: { nonce?: string } = {}
): Promise<RatifyResponse> {
  return apiFetch<RatifyResponse>('/v1/newproject/create', {
    method: 'POST',
    body: JSON.stringify(body),
    headers: {
      'Content-Type': 'application/json',
      [CONFIRM_HEADER]: opts.nonce ?? freshNonce(),
    },
  });
}

// probeOnboarder — GET /api/v1/onboarder/probe. Reports whether the
// Onboarder persona host can resolve an LLM client. The SPA reads
// this on board empty-state to decide whether to render the
// "Plan with the Onboarder →" CTA. gm-root.17.13.
export async function probeOnboarder(): Promise<OnboarderProbeResponse> {
  return apiFetch<OnboarderProbeResponse>('/v1/onboarder/probe', {
    method: 'GET',
  });
}
