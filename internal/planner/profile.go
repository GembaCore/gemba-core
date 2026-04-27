// SessionProfile + read-shape definitions for gm-s47n.2.1.
//
// The session profile is NOT a standalone entity in the domain model
// — it's a join. The planner reads AgentRef + Session + Workspace
// (already in internal/core/orchestration.go) plus this package's
// new session_profiles row plus a derived SessionHealth snapshot.
// docs/design/work-planning.md §4 Layer 1 is the source of truth.

package planner

import (
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// LastBeadsRingSize is the default size of the SessionProfile.LastBeads
// ring buffer. Five is large enough to compute "what did this session
// just touch?" recency scores without paying for the full session
// history every dispatch.
const LastBeadsRingSize = 5

// DefaultDecayHalfLife is the number of completed bead events over
// which a concept or file weight halves. Half-life is in EVENTS,
// not wall time, per spec §4 Layer 1 — an idle session must not
// lose its priming overnight.
const DefaultDecayHalfLife = 5

// ConceptTag is a short tag from the controlled concept vocabulary
// (docs/design/work-planning.md §6). Free string at this layer;
// vocabulary governance is gm-s47n.7.
type ConceptTag string

// SessionProfile is the per-session warm-context snapshot the
// planner consults to compute affinity (spec §5.4).
//
// It lives in dolt (table: session_profiles) keyed by SessionID.
// The schema joins to:
//
//   - core.Session via SessionID
//   - core.Assignment via AssignmentID (denormalised here so the
//     planner doesn't need a second join just to find the bead the
//     session is on)
//
// Decay applies to Concepts and Files: each entry's value is the
// recency-decayed weight summed over completed beads (spec §5.1).
type SessionProfile struct {
	// SessionID matches core.Session.ID — primary key in the dolt
	// table.
	SessionID string `json:"session_id"`
	// AssignmentID denormalised from core.Assignment so the planner
	// can answer "what is this session currently on?" in one read.
	AssignmentID string `json:"assignment_id"`
	// AgentID denormalised from core.AgentRef.ID so per-agent
	// rollups don't require a second lookup.
	AgentID core.AgentID `json:"agent_id"`

	// Concepts is the recency-decayed concept profile: tag → weight.
	// JSON object on the wire; dolt column is JSON.
	Concepts map[ConceptTag]float64 `json:"concepts,omitempty"`

	// Files is the recency-decayed file-touch profile: repo-relative
	// path → weight. Same shape as Concepts.
	Files map[string]float64 `json:"files,omitempty"`

	// TokensUsed is the most recent observed token usage on the
	// session's underlying LLM context. Surfaced via the agent's
	// telemetry hook (gm-s47n.2.3).
	TokensUsed int `json:"tokens_used,omitempty"`
	// ContextWindowMax is the agent's hard context window. Stored
	// alongside TokensUsed so a single read computes
	// `tokens_used / context_window_max` without a separate manifest
	// lookup. May be zero on first observation.
	ContextWindowMax int `json:"context_window_max,omitempty"`
	// ContextPct is the precomputed `TokensUsed / ContextWindowMax`
	// snapshot — derived but persisted so the SPA / planner share the
	// same number without re-deriving. Always in [0, 1] when
	// ContextWindowMax is non-zero; zero when not yet observed.
	ContextPct float64 `json:"context_pct,omitempty"`

	// LastBeads is the ring buffer of the last N completed bead ids
	// (newest last). Capped at LastBeadsRingSize. The retrospective
	// (gm-s47n.8) reads it to detect "session just shipped X" cases.
	LastBeads []core.WorkItemID `json:"last_beads,omitempty"`

	// LastActivityAt is the timestamp of the most recent BEAD-EVENT
	// boundary (claim or completion) on this session — distinct from
	// core.Session.LastHeartbeat which updates on every health ping.
	// "Activity" here means "the session moved the planner's world".
	LastActivityAt time.Time `json:"last_activity_at"`

	// CreatedAt / UpdatedAt — table audit columns. The planner reads
	// UpdatedAt to break ties when two sessions have the same
	// affinity (newer profile wins).
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionHealth is the planner-derived liveness snapshot for a
// session — three numbers spec §4 Layer 4 names. NOT persisted as a
// table; computed on each read from the SessionProfile + the live
// core.Session record. Lives here so the OperationalContext can
// carry a single, stable shape.
//
// Thresholds (warn / strong-warn) live with the planner (Layer 4
// implementation, gm-s47n.5) — this struct is just the numbers.
type SessionHealth struct {
	// ContextPressure = TokensUsed / ContextWindowMax. Zero when
	// ContextWindowMax is not yet observed (session pre-telemetry).
	ContextPressure float64 `json:"context_pressure"`
	// ConceptDrift = cosine distance between the session's last-N
	// concept vector and the lifetime concept vector. Range [0, 1];
	// 0 means "still on the same topic", 1 means "completely shifted".
	ConceptDrift float64 `json:"concept_drift"`
	// TimeOnTask is wall clock since core.Session.StartedAt.
	TimeOnTask time.Duration `json:"time_on_task_ns"`
}

// OperationalContext is the single read shape the planner returns
// from `OperationalContext(session_id)` (gm-s47n.2.5). Scorers,
// coach UI, and auto-dispatch all consume this same struct.
//
// One pointer per join — nil means "not yet provisioned" (e.g. a
// session may exist without an Assignment in transient states; the
// planner handles that case explicitly rather than treating it as
// an error).
//
// Wire shape: stable JSON, every field omitempty so a partial
// snapshot serialises cleanly when the planner wants to surface a
// degraded view.
type OperationalContext struct {
	Agent      *core.AgentRef   `json:"agent,omitempty"`
	Session    *core.Session    `json:"session,omitempty"`
	Workspace  *core.Workspace  `json:"workspace,omitempty"`
	Assignment *core.Assignment `json:"assignment,omitempty"`
	Profile    *SessionProfile  `json:"profile,omitempty"`
	Health     *SessionHealth   `json:"health,omitempty"`
}
