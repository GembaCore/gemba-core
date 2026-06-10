package epic_order

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/internal/core/persona"
)

// ID is the canonical skill identifier. Personas reference this string
// in their `skills` list; the dispatcher resolves it through the
// SkillRegistry. Exported as a const so callers (router setup, tests)
// don't have to string-match.
const ID = "epic_order"

// Skill implements persona.Skill for Epic ordering. It is stateless —
// every consult constructs its own input from the request body — so a
// single instance registered at startup can serve all callers
// concurrently.
type Skill struct{}

// New returns a fresh skill instance. The registry stores pointers, so
// callers MUST register the result of New (not a value-form Skill{}),
// otherwise the idempotent same-pointer guard in SkillRegistry.Register
// can't take effect.
func New() *Skill { return &Skill{} }

// Register installs this skill into r. Provided as a helper so the
// composition root (cmd/gemba-server) can wire skills with one call per
// package without knowing the constructor signature.
func Register(r *persona.SkillRegistry) error {
	return r.Register(New())
}

// ID returns "epic_order". MUST match the persona's skills list entry
// or PersonaCanInvoke rejects the consult.
func (Skill) ID() string { return ID }

// Name is the short display label used in the skill picker.
func (Skill) Name() string { return "Epic order" }

// Description is one paragraph rendered in the skill picker and the
// /api/v1/skills/:id payload.
func (Skill) Description() string {
	return "Rank candidate Epics for staging. Takes precomputed signals (deps, file-space, budget cost) and returns a JSONL stream of ranked recommendations with rationales, plus warnings and deferrals."
}

// SkillPrompt is the skill-specific fragment spliced into LayerSkill
// of the prompt envelope (gm-3d6). It is intentionally terse — the
// persona's system_prompt already covers the role, hard rules, and
// output discipline. This fragment only restates the schema the
// model must produce, in case the model ignores tool descriptions.
func (Skill) SkillPrompt() string {
	return strings.TrimSpace(`
Task: rank the candidate Epics in the user message.

You MUST call the emit_ordering tool exactly once. Its argument is an
ordered array of line objects, each with a "type" discriminator:

  - exactly one line with "type":"strategy" first
  - zero or more "type":"recommendation" lines, in rank order, with
    rank starting at 0 and increasing by 1
  - zero or more "type":"warning" or "type":"deferred" lines (any order
    after the recommendations)
  - exactly one "type":"summary" line last

Confidence is on [0, 1]. Rank is a non-negative integer. Never
reference an epic_id outside the input's candidate_epics list — except
when naming a parent in rationale text.

If the input has zero candidate_epics, emit a strategy line that
explains why no ranking is possible and a summary line; emit no
recommendation lines.
`)
}

// OutputTool returns the emit_ordering tool spec. The provider router
// translates this into Anthropic's `tools` field; the model is
// instructed (in SkillPrompt) to call this tool exactly once.
func (Skill) OutputTool() persona.ToolSpec {
	return persona.ToolSpec{
		Name:        "emit_ordering",
		Description: "Emit the ranked staging order as a JSONL-shaped array. The first line MUST have type=strategy; the last MUST have type=summary; recommendation lines (if any) come in rank order between them, followed by zero or more warning/deferred lines.",
		InputSchema: outputSchema,
	}
}

// ValidateInput parses raw and runs the structural rules the prompt
// composer relies on. Returns the typed *EpicOrderInput on success.
// Failure messages are user-facing — the HTTP layer surfaces them as
// 400 bodies — so they MUST NOT leak internal field names that don't
// appear in the JSON wire shape.
func (Skill) ValidateInput(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("epic_order: input is empty")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var in EpicOrderInput
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("epic_order: decode input: %w", err)
	}
	if strings.TrimSpace(in.Workspace) == "" {
		return nil, errors.New("epic_order: workspace must not be empty")
	}
	if strings.TrimSpace(in.WorkspaceName) == "" {
		return nil, errors.New("epic_order: workspace_name must not be empty")
	}
	if in.AsOf.IsZero() {
		return nil, errors.New("epic_order: as_of must be set")
	}
	if len(in.CandidateEpics) == 0 {
		return nil, errors.New("epic_order: candidate_epics must not be empty")
	}
	for i, e := range in.CandidateEpics {
		if strings.TrimSpace(string(e.EpicID)) == "" {
			return nil, fmt.Errorf("epic_order: candidate_epics[%d].epic_id must not be empty", i)
		}
		if strings.TrimSpace(e.Title) == "" {
			return nil, fmt.Errorf("epic_order: candidate_epics[%d].title must not be empty", i)
		}
		if strings.TrimSpace(e.UIState) == "" {
			return nil, fmt.Errorf("epic_order: candidate_epics[%d].ui_state must not be empty", i)
		}
	}
	if in.MaxRecommendations < 0 {
		return nil, errors.New("epic_order: max_recommendations must be >= 0")
	}
	if c := in.Constraints; c.MinConfidence < 0 || c.MinConfidence > 1 {
		return nil, errors.New("epic_order: constraints.min_confidence must be in [0, 1]")
	}
	if sc := in.SprintContext; sc != nil {
		if sc.TokenBudget < 0 || sc.TokensUsed < 0 {
			return nil, errors.New("epic_order: sprint_context token counts must be non-negative")
		}
		t := sc.BurnThresholds
		if !(0 <= t.Inform && t.Inform <= t.Warn && t.Warn <= t.Stop && t.Stop <= 1) {
			return nil, errors.New("epic_order: sprint_context.burn_thresholds must satisfy 0 <= inform <= warn <= stop <= 1")
		}
	}
	return &in, nil
}

// ValidateOutputLine parses one element of the model's emit_ordering
// array. The dispatcher calls this on every line; on error the line is
// dropped and a `warning` line is spliced in its place.
//
// Returns one of: *StrategyLine, *RecommendationLine, *WarningLine,
// *DeferredLine, *SummaryLine.
func (Skill) ValidateOutputLine(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("epic_order: output line is empty")
	}
	var head struct {
		Type LineType `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("epic_order: line missing type discriminator: %w", err)
	}
	switch head.Type {
	case LineStrategy:
		var v StrategyLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.Reasoning) == "" {
			return nil, errors.New("epic_order: strategy.reasoning must not be empty")
		}
		if strings.TrimSpace(v.Model) == "" {
			return nil, errors.New("epic_order: strategy.model must not be empty")
		}
		return &v, nil
	case LineRecommendation:
		var v RecommendationLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if v.Rank < 0 {
			return nil, errors.New("epic_order: recommendation.rank must be >= 0")
		}
		if strings.TrimSpace(string(v.EpicID)) == "" {
			return nil, errors.New("epic_order: recommendation.epic_id must not be empty")
		}
		if v.Confidence < 0 || v.Confidence > 1 {
			return nil, errors.New("epic_order: recommendation.confidence must be in [0, 1]")
		}
		if strings.TrimSpace(v.Rationale) == "" {
			return nil, errors.New("epic_order: recommendation.rationale must not be empty")
		}
		return &v, nil
	case LineWarning:
		var v WarningLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.Kind) == "" {
			return nil, errors.New("epic_order: warning.kind must not be empty")
		}
		if strings.TrimSpace(v.Detail) == "" {
			return nil, errors.New("epic_order: warning.detail must not be empty")
		}
		return &v, nil
	case LineDeferred:
		var v DeferredLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(v.EpicID)) == "" {
			return nil, errors.New("epic_order: deferred.epic_id must not be empty")
		}
		if strings.TrimSpace(v.Reason) == "" {
			return nil, errors.New("epic_order: deferred.reason must not be empty")
		}
		return &v, nil
	case LineSummary:
		var v SummaryLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.Model) == "" {
			return nil, errors.New("epic_order: summary.model must not be empty")
		}
		if v.ConfidenceOverall < 0 || v.ConfidenceOverall > 1 {
			return nil, errors.New("epic_order: summary.confidence_overall must be in [0, 1]")
		}
		return &v, nil
	default:
		return nil, fmt.Errorf("epic_order: unknown line type %q", head.Type)
	}
}

// strictUnmarshal rejects unknown fields so a model that hallucinates
// extra keys produces a validation error the dispatcher can splice a
// warning line for, rather than silently dropping the data.
func strictUnmarshal(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("epic_order: decode line: %w", err)
	}
	return nil
}
