// Package escalation_handoff — see doc.go for the full picture.
//
// The skill is intentionally tiny because the consult dispatcher
// already does the heavy lifting (persona resolution, prompt
// composition, spawn). All this skill adds is:
//
//  1. A stable id (`escalation_handoff`) that personas can opt into
//     via their TOML `skills` block.
//  2. Input validation for the HandoffInput shape.
//  3. A skill-specific prompt fragment that frames the user message
//     as "an operator handed off this escalation; please reply".
//  4. An output tool spec (`emit_handoff_response`) so the model's
//     structured output flows through the same FanFromHub pipeline
//     as every other LLM-driven skill.

package escalation_handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/internal/core/persona"
)

// ID is the canonical skill identifier the dispatcher resolves the
// skill instance through. Personas reference this string verbatim in
// their TOML `skills` list; PersonaCanInvoke uses string equality.
const ID = "escalation_handoff"

// Skill implements persona.Skill for hand-off routing. Stateless —
// every consult composes its own input from the request body, so a
// single registered instance serves all callers concurrently.
type Skill struct{}

// New returns a fresh skill instance. Register with the
// SkillRegistry via Register; the registry stores pointers, so
// callers MUST register the result of New rather than a value-form
// Skill{} so the idempotent same-pointer guard takes effect.
func New() *Skill { return &Skill{} }

// Register installs this skill into r. The composition root
// (cmd/gemba serve, via internal/cli/serve.go) calls this alongside
// epic_order.Register so the SkillRegistry is a single read surface.
func Register(r *persona.SkillRegistry) error {
	return r.Register(New())
}

// ID returns "escalation_handoff". MUST match the persona's skills
// list entry or PersonaCanInvoke rejects the consult.
func (Skill) ID() string { return ID }

// Name is the short display label rendered in the skill picker.
func (Skill) Name() string { return "Escalation hand-off" }

// Description is one paragraph rendered in the skill picker and the
// /api/v1/skills/:id payload.
func (Skill) Description() string {
	return "Route a stuck escalation to a chosen persona for a second opinion. " +
		"Does not resolve the escalation — the persona replies with a recommended " +
		"disposition and the operator decides whether to act on it."
}

// SkillPrompt is the skill-specific fragment spliced into LayerSkill
// of the prompt envelope (gm-3d6). Terse on purpose: the persona's
// system_prompt already covers the role + voice; this fragment only
// states the task shape and the output discipline.
func (Skill) SkillPrompt() string {
	return strings.TrimSpace(`
Task: an operator has handed off a stuck escalation to you for a second opinion.

You will receive the escalation's title, its prompt to the original agent, and
optionally the originating session id. Read it, decide whether you would
approve, deny, modify, defer, or need more information, and reply.

You MUST call the emit_handoff_response tool exactly once. Its argument is an
ordered array of line objects, each with a "type" discriminator:

  - one or more "type":"response" lines, in narrative order, each with a
    non-empty "message" field
  - exactly one "type":"summary" line last, with the originating
    "escalation_id", a "disposition" string, and a non-empty "note"

You are NOT resolving the escalation. The operator decides whether to act on
your recommendation. Do not promise side effects; reason about the request and
hand back a clear disposition.
`)
}

// outputSchema is the JSON Schema for the emit_handoff_response
// tool's array. Stored as map[string]any so the provider router can
// re-marshal into vendor-native shape (Anthropic tools, etc.) without
// an extra dependency at this layer.
var outputSchema = map[string]any{
	"type":        "array",
	"description": "Ordered array of response lines followed by exactly one summary line.",
	"items": map[string]any{
		"oneOf": []any{
			map[string]any{
				"type":     "object",
				"required": []any{"type", "message"},
				"properties": map[string]any{
					"type":    map[string]any{"const": "response"},
					"message": map[string]any{"type": "string", "minLength": 1},
				},
			},
			map[string]any{
				"type":     "object",
				"required": []any{"type", "escalation_id", "note"},
				"properties": map[string]any{
					"type":          map[string]any{"const": "summary"},
					"escalation_id": map[string]any{"type": "string", "minLength": 1},
					"disposition":   map[string]any{"type": "string"},
					"note":          map[string]any{"type": "string", "minLength": 1},
				},
			},
		},
	},
}

// OutputTool returns the emit_handoff_response tool spec. The
// provider router translates this into the vendor's structured-
// output shape (Anthropic's `tools` field, OpenAI's
// `function_call`); the model is told via SkillPrompt to call this
// tool exactly once.
func (Skill) OutputTool() persona.ToolSpec {
	return persona.ToolSpec{
		Name:        "emit_handoff_response",
		Description: "Emit the persona's hand-off reply as a JSONL-shaped array. The last line MUST be type=summary with the originating escalation_id and a non-empty note; preceding lines (one or more) MUST be type=response with a non-empty message.",
		InputSchema: outputSchema,
	}
}

// ValidateInput parses raw and runs the structural rules the prompt
// composer relies on. Failure messages are user-facing — the HTTP
// layer surfaces them as 400 bodies — so they MUST NOT leak internal
// field names that don't appear in the JSON wire shape.
func (Skill) ValidateInput(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("escalation_handoff: input is empty")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var in HandoffInput
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("escalation_handoff: decode input: %w", err)
	}
	if strings.TrimSpace(in.EscalationID) == "" {
		return nil, errors.New("escalation_handoff: escalation_id must not be empty")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("escalation_handoff: title must not be empty")
	}
	// Prompt + assignment_id + target_persona are all optional. An
	// escalation can carry only a title (e.g. a blocker_filed event
	// the agent only labelled), and the dispatcher already resolves
	// the persona via createConsultRequest.persona_id.
	return &in, nil
}

// ValidateOutputLine parses one element of the model's
// emit_handoff_response array. The dispatcher calls this on every
// emitted line; on error the line is dropped and a `warning` line
// is spliced in its place.
//
// Returns one of: *ResponseLine, *SummaryLine.
func (Skill) ValidateOutputLine(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("escalation_handoff: output line is empty")
	}
	var head struct {
		Type LineType `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("escalation_handoff: line missing type discriminator: %w", err)
	}
	switch head.Type {
	case LineResponse:
		var v ResponseLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.Message) == "" {
			return nil, errors.New("escalation_handoff: response.message must not be empty")
		}
		return &v, nil
	case LineSummary:
		var v SummaryLine
		if err := strictUnmarshal(raw, &v); err != nil {
			return nil, err
		}
		if strings.TrimSpace(v.EscalationID) == "" {
			return nil, errors.New("escalation_handoff: summary.escalation_id must not be empty")
		}
		if strings.TrimSpace(v.Note) == "" {
			return nil, errors.New("escalation_handoff: summary.note must not be empty")
		}
		return &v, nil
	default:
		return nil, fmt.Errorf("escalation_handoff: unknown line type %q", head.Type)
	}
}

// strictUnmarshal rejects unknown fields so a hallucinated extra key
// produces a validation error the dispatcher can splice a warning
// line for, rather than silently dropping the data.
func strictUnmarshal(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("escalation_handoff: decode line: %w", err)
	}
	return nil
}
