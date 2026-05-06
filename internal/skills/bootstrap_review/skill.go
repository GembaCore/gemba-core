package bootstrap_review

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/internal/core/persona"
)

const ID = "bootstrap_review"

type Skill struct{}

func New() *Skill { return &Skill{} }

func Register(r *persona.SkillRegistry) error {
	return r.Register(New())
}

func (Skill) ID() string { return ID }

func (Skill) Name() string { return "Bootstrap review" }

func (Skill) Description() string {
	return "Review a staged bootstrap-pack Beads draft as a batch. The coach may ask clarifying questions, propose item patches, and summarize whether the set is ready for approval."
}

func (Skill) SkillPrompt() string {
	return strings.TrimSpace(`
Task: review a staged bootstrap-pack draft before it is applied to Beads.

You are a coach. You do not mutate Beads. Inspect the generated milestone,
epics, stories, and tasks as a batch. Ask clarifying questions when the draft
is underspecified, and propose batch-safe title/description patches when the
operator's guidance is clear.

You MUST call the emit_bootstrap_review tool exactly once. Its argument is an
ordered array of line objects:

  - zero or more "type":"question" lines when approval needs clarification
  - zero or more "type":"patch" lines for concrete item title/description edits
  - exactly one "type":"summary" line last, with ready=true only when the batch
    is coherent enough for the operator to approve

Never invent item ids. Only patch item ids present in the input. Keep rationales
short and operator-facing.
`)
}

var outputSchema = map[string]any{
	"type":        "array",
	"description": "Bootstrap review lines: optional questions/patches followed by one summary.",
	"items": map[string]any{
		"oneOf": []any{
			map[string]any{
				"type":     "object",
				"required": []any{"type", "question"},
				"properties": map[string]any{
					"type":     map[string]any{"const": "question"},
					"question": map[string]any{"type": "string", "minLength": 1},
					"reason":   map[string]any{"type": "string"},
				},
			},
			map[string]any{
				"type":     "object",
				"required": []any{"type", "item_id", "rationale"},
				"properties": map[string]any{
					"type":        map[string]any{"const": "patch"},
					"item_id":     map[string]any{"type": "string", "minLength": 1},
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"rationale":   map[string]any{"type": "string", "minLength": 1},
				},
			},
			map[string]any{
				"type":     "object",
				"required": []any{"type", "feature_id", "ready", "note"},
				"properties": map[string]any{
					"type":       map[string]any{"const": "summary"},
					"feature_id": map[string]any{"type": "string", "minLength": 1},
					"ready":      map[string]any{"type": "boolean"},
					"note":       map[string]any{"type": "string", "minLength": 1},
				},
			},
		},
	},
}

func (Skill) OutputTool() persona.ToolSpec {
	return persona.ToolSpec{
		Name:        "emit_bootstrap_review",
		Description: "Emit bootstrap-pack review questions, draft item patches, and one readiness summary.",
		InputSchema: outputSchema,
	}
}

func (Skill) ValidateInput(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("bootstrap_review: input is empty")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var in ReviewInput
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("bootstrap_review: decode input: %w", err)
	}
	if strings.TrimSpace(in.WorkspaceName) == "" {
		return nil, errors.New("bootstrap_review: workspace_name must not be empty")
	}
	if strings.TrimSpace(in.PackID) == "" {
		return nil, errors.New("bootstrap_review: pack_id must not be empty")
	}
	if strings.TrimSpace(in.FeatureID) == "" {
		return nil, errors.New("bootstrap_review: feature_id must not be empty")
	}
	if strings.TrimSpace(in.FeatureTitle) == "" {
		return nil, errors.New("bootstrap_review: feature_title must not be empty")
	}
	if strings.TrimSpace(in.PlanHash) == "" {
		return nil, errors.New("bootstrap_review: plan_hash must not be empty")
	}
	if len(in.Items) == 0 {
		return nil, errors.New("bootstrap_review: items must not be empty")
	}
	for i, item := range in.Items {
		if strings.TrimSpace(item.ID) == "" {
			return nil, fmt.Errorf("bootstrap_review: items[%d].id must not be empty", i)
		}
		if strings.TrimSpace(item.Kind) == "" {
			return nil, fmt.Errorf("bootstrap_review: items[%d].kind must not be empty", i)
		}
		if strings.TrimSpace(item.Title) == "" {
			return nil, fmt.Errorf("bootstrap_review: items[%d].title must not be empty", i)
		}
	}
	return &in, nil
}

func (Skill) ValidateOutputLine(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("bootstrap_review: output line is empty")
	}
	var head struct {
		Type LineType `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("bootstrap_review: line missing type discriminator: %w", err)
	}
	switch head.Type {
	case LineQuestion:
		var v QuestionLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.Question) == "" {
			return nil, errors.New("bootstrap_review: question.question must not be empty")
		}
		return &v, nil
	case LinePatch:
		var v PatchLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.ItemID) == "" {
			return nil, errors.New("bootstrap_review: patch.item_id must not be empty")
		}
		if strings.TrimSpace(v.Rationale) == "" {
			return nil, errors.New("bootstrap_review: patch.rationale must not be empty")
		}
		if strings.TrimSpace(v.Title) == "" && strings.TrimSpace(v.Description) == "" {
			return nil, errors.New("bootstrap_review: patch requires title or description")
		}
		return &v, nil
	case LineSummary:
		var v SummaryLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.FeatureID) == "" {
			return nil, errors.New("bootstrap_review: summary.feature_id must not be empty")
		}
		if strings.TrimSpace(v.Note) == "" {
			return nil, errors.New("bootstrap_review: summary.note must not be empty")
		}
		return &v, nil
	default:
		return nil, fmt.Errorf("bootstrap_review: unknown line type %q", head.Type)
	}
}

func strictUnmarshal(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("bootstrap_review: decode line: %w", err)
	}
	return nil
}
