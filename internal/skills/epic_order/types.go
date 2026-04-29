package epic_order

import (
	"encoding/json"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// LineType is the discriminator for an output line in the emit_ordering
// tool's array. The dispatcher reads this field on every line to enforce
// ordering invariants (strategy first, summary last) before persisting
// the response.
type LineType string

const (
	LineStrategy       LineType = "strategy"
	LineRecommendation LineType = "recommendation"
	LineWarning        LineType = "warning"
	LineDeferred       LineType = "deferred"
	LineSummary        LineType = "summary"
)

// EpicOrderInput is the request body for an epic_order consult. The
// caller (the /plan view handler) is responsible for assembling this
// from live workspace state — the persona sees a snapshot, not live
// data, so re-running with the same input MUST produce equivalent
// reasoning (modulo model nondeterminism).
type EpicOrderInput struct {
	// Workspace is the workspace ID the recommendation is scoped to.
	// Free-form string at the skill boundary; the dispatcher validates
	// it against the workspace registry before invoking.
	Workspace string `json:"workspace"`

	// WorkspaceName is the human-readable label spliced into the
	// persona's system_prompt template ({{workspace_name}}).
	WorkspaceName string `json:"workspace_name"`

	// AsOf is the freeze-point all derived signals were computed at.
	// Echoed back in the strategy line so the audit log can show the
	// snapshot the PM reasoned over.
	AsOf time.Time `json:"as_of"`

	// SprintContext is optional. When present, the PM uses budget
	// thresholds to trim aggressively; when absent, it ranks without
	// budget reasoning and a warning may surface that omission.
	SprintContext *SprintContext `json:"sprint_context,omitempty"`

	// CandidateEpics are the Epics to rank. Empty slice is rejected.
	CandidateEpics []EpicInput `json:"candidate_epics"`

	// RecentCompletions calibrates velocity estimates. The PM may
	// reference these in rationales but never recommends action on
	// them (they're already closed).
	RecentCompletions []ClosedEpicSummary `json:"recent_completions,omitempty"`

	// OpenEscalations may reorder priorities. An Epic that resolves
	// an open escalation jumps in rank.
	OpenEscalations []EscalationSummary `json:"open_escalations,omitempty"`

	// Constraints bound the consult — dollar cap, latency cap, and
	// minimum confidence floor. The dispatcher enforces dollar/latency
	// caps before the provider call fires; MinConfidence is enforced
	// post-validation when filtering recommendations.
	Constraints EpicOrderConstraints `json:"constraints"`

	// Guidance is operator free-form intent ("focus on unblocking UI
	// work"). Spliced into the user message verbatim. The PM is
	// instructed to never let guidance override hard rules (dep graph,
	// budget cap).
	Guidance string `json:"guidance,omitempty"`

	// MaxRecommendations caps the rec list length. Zero means "all
	// candidates are eligible".
	MaxRecommendations int `json:"max_recommendations,omitempty"`
}

// EpicInput is one Epic the PM may rank. The caller pre-derives every
// field; the persona never queries back into the bead database in v1.
type EpicInput struct {
	EpicID core.WorkItemID `json:"epic_id"`
	Title  string          `json:"title"`

	// Summary is one-paragraph free-form context. Optional.
	Summary string `json:"summary,omitempty"`

	// ParentEpic, when present, is the Epic this one nests under.
	// The PM may reference parents in rationale text but only stages
	// candidates from CandidateEpics.
	ParentEpic *core.WorkItemID `json:"parent_epic,omitempty"`

	// UIState is the current planner-board column ("on_deck",
	// "in_scope", etc.). Free-form at this boundary because the core
	// EpicUIState enum (gm-hjx) is still in design.
	UIState string `json:"ui_state"`

	// CurrentRank is the existing user_order if one has been pinned.
	// The PM may preserve it or override; either way, suggested
	// actions reset rank explicitly.
	CurrentRank *int `json:"current_rank,omitempty"`

	// Derived carries the precomputed signals (scope, budget cost,
	// blast radius, etc.) — opaque at this layer. The composing
	// prompt walks the map and renders it as bullet points; the
	// shape is whatever gm-hjx ratifies.
	Derived map[string]any `json:"derived,omitempty"`

	// DependsOnEpics is the dep graph slice the PM must respect:
	// no Epic can rank above one it depends on.
	DependsOnEpics []core.WorkItemID `json:"depends_on_epics,omitempty"`

	// ParallelGroup, when present, declares membership in a parallel
	// batch. The PM is instructed to audit groups (file-space overlap)
	// and emit warnings on conflicts.
	ParallelGroup *string `json:"parallel_group,omitempty"`

	// EstimatedTokens is the caller's best estimate of cost. The
	// PM uses this for budget reasoning and echoes it on the rec line.
	EstimatedTokens *int `json:"estimated_tokens,omitempty"`

	// FileSpaceSignature lists the top N paths the Epic historically
	// touches (or is expected to touch). Used by the audit-parallel-
	// groups rule.
	FileSpaceSignature []string `json:"file_space_signature,omitempty"`
}

// ClosedEpicSummary calibrates velocity. Caller passes in the last N
// closed Epics with elapsed time + token cost so the PM can reason
// about throughput.
type ClosedEpicSummary struct {
	EpicID         core.WorkItemID `json:"epic_id"`
	Title          string          `json:"title"`
	ClosedAt       time.Time       `json:"closed_at"`
	ElapsedHours   float64         `json:"elapsed_hours,omitempty"`
	TokensConsumed int             `json:"tokens_consumed,omitempty"`
}

// EscalationSummary surfaces an open escalation that may reorder
// priorities. The PM is instructed: "an Epic that resolves an open
// escalation jumps in rank."
type EscalationSummary struct {
	ID          string          `json:"id"`
	Severity    string          `json:"severity"`
	Title       string          `json:"title"`
	BlockedEpic core.WorkItemID `json:"blocked_epic,omitempty"`
	OpenedAt    time.Time       `json:"opened_at,omitempty"`
}

// SprintContext is the budget envelope the PM reasons inside. Burn
// thresholds are fractions of TokenBudget at which behavior changes
// (inform → log, warn → trim, stop → reject).
type SprintContext struct {
	SprintID       string         `json:"sprint_id,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	EndsAt         time.Time      `json:"ends_at,omitempty"`
	TokenBudget    int            `json:"token_budget"`
	TokensUsed     int            `json:"tokens_used"`
	BurnThresholds BurnThresholds `json:"burn_thresholds"`
}

// BurnThresholds is broken out as a named type so it round-trips
// cleanly through JSON Schema generation; an anonymous struct would
// emit an inline definition that's harder to reference from the tool
// schema.
type BurnThresholds struct {
	Inform float64 `json:"inform"`
	Warn   float64 `json:"warn"`
	Stop   float64 `json:"stop"`
}

// EpicOrderConstraints bounds the consult. Zero values are "no limit
// from the caller" — the persona's BudgetPolicy still applies on top.
type EpicOrderConstraints struct {
	MaxDollars     float64 `json:"max_dollars,omitempty"`
	MaxLatencySecs int     `json:"max_latency_seconds,omitempty"`
	RequiredModel  string  `json:"required_model,omitempty"`
	MinConfidence  float64 `json:"min_confidence,omitempty"`
}

// StrategyLine is the first emitted line. Exactly one per consult.
// Captures the PM's framing + model metadata so the audit log can
// reconstruct what context the model saw without re-fetching.
type StrategyLine struct {
	Type            LineType  `json:"type"`
	Workspace       string    `json:"workspace"`
	AsOf            time.Time `json:"as_of"`
	Model           string    `json:"model"`
	Reasoning       string    `json:"reasoning"`
	TotalConsidered int       `json:"total_considered"`
	TotalRanked     int       `json:"total_ranked"`
	Filtered        int       `json:"filtered,omitempty"`
	FilterReason    string    `json:"filter_reason,omitempty"`
}

// RecommendationLine is a ranked Epic. Zero or more per consult, in
// rank order. Confidence is on [0, 1]; the UI renders < 0.6 with a
// "low confidence" treatment.
type RecommendationLine struct {
	Type            LineType         `json:"type"`
	Rank            int              `json:"rank"`
	EpicID          core.WorkItemID  `json:"epic_id"`
	ParallelGroup   *string          `json:"parallel_group,omitempty"`
	Confidence      float64          `json:"confidence"`
	Rationale       string           `json:"rationale"`
	EstimatedTokens *int             `json:"estimated_tokens,omitempty"`
	SuggestedAction *SuggestedAction `json:"suggested_action,omitempty"`
}

// SuggestedAction is the verb+path the operator (or auto-approve, in
// `managed` mode) applies to enact the recommendation. The persona
// MUST NOT call APIs itself — it proposes, the operator confirms via
// the apply endpoint.
type SuggestedAction struct {
	Verb string          `json:"verb"`
	Path string          `json:"path"`
	Body json.RawMessage `json:"body,omitempty"`
}

// WarningLine flags an issue with the recommendation set. Kind is a
// closed enum (parallel_conflict / dep_cycle / budget_overrun /
// missing_signal) so consumers can switch on it; Detail is free-form.
type WarningLine struct {
	Type    LineType          `json:"type"`
	Kind    string            `json:"kind"`
	EpicIDs []core.WorkItemID `json:"epic_ids,omitempty"`
	Detail  string            `json:"detail"`
}

// DeferredLine declares an Epic the PM declined to rank, with reason.
// Distinct from filtering: filtered = "trimmed for budget"; deferred =
// "actively can't act on this" (externally blocked, etc.).
type DeferredLine struct {
	Type   LineType        `json:"type"`
	EpicID core.WorkItemID `json:"epic_id"`
	Reason string          `json:"reason"`
}

// SummaryLine is the last emitted line. Exactly one per consult.
// Aggregates cost + latency + counts so the audit-log row can be
// rendered without re-walking the line list.
type SummaryLine struct {
	Type                 LineType `json:"type"`
	RankedCount          int      `json:"ranked_count"`
	FilteredCount        int      `json:"filtered_count,omitempty"`
	WarningsCount        int      `json:"warnings_count,omitempty"`
	DeferredCount        int      `json:"deferred_count,omitempty"`
	TotalEstimatedTokens int      `json:"total_estimated_tokens,omitempty"`
	SprintBudgetHeadroom float64  `json:"sprint_budget_headroom,omitempty"`
	AdvisorCostDollars   float64  `json:"advisor_cost_dollars"`
	TokensIn             int      `json:"tokens_in"`
	TokensOut            int      `json:"tokens_out"`
	LatencyMs            int      `json:"latency_ms"`
	Model                string   `json:"model"`
	ConfidenceOverall    float64  `json:"confidence_overall,omitempty"`
}
