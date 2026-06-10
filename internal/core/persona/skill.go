package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Skill is the runtime contract a coded skill exposes. Skills are
// registered at startup and resolved by ID at consult time. A persona
// declares the IDs it ships with in its TOML file; the dispatcher
// rejects a consult whose skill_id is not in the persona's `skills`
// list, so this interface is also load-bearing for authorization.
//
// The interface is deliberately small: validation, prompt material,
// and the structured-output tool shape. Provider-specific concerns
// (which vendor? which model?) live on the persona, not the skill —
// so the same skill can be wired to Anthropic today and an OpenAI
// path later without touching this surface.
type Skill interface {
	// ID is the stable kebab/snake-case identifier ("epic_order").
	// It MUST match the entry in the persona's `skills` list.
	ID() string

	// Name is the short display label ("Epic order").
	Name() string

	// Description is one-paragraph copy shown in the skill picker.
	Description() string

	// ValidateInput parses and validates a raw JSON input body. It
	// returns the canonical typed form for downstream code, or an
	// error whose Error() string is safe to surface to the caller
	// (the HTTP layer maps it to a 400). Implementations MUST NOT
	// fall through to "best effort" parsing — invalid input fails
	// fast.
	ValidateInput(raw json.RawMessage) (any, error)

	// ValidateOutputLine parses one element of the model's
	// structured-output array. The dispatcher calls this on every
	// emitted line; a non-nil error causes the line to be dropped
	// and a `warning` line spliced in its place. Implementations
	// MUST distinguish line types via the `type` discriminator so
	// the dispatcher can enforce ordering rules (strategy first,
	// summary last).
	ValidateOutputLine(raw json.RawMessage) (any, error)

	// SkillPrompt returns the skill-specific prompt text spliced
	// into LayerSkill of the prompt envelope (gm-3d6). It SHOULD
	// state the task shape, the output schema, and any invariants
	// the persona system_prompt does not already cover.
	SkillPrompt() string

	// OutputTool is the vendor-agnostic structured-output tool spec
	// the provider router maps to its native form (Anthropic's
	// `tools` field, OpenAI's `function_call`, etc.). Returning a
	// zero-value ToolSpec selects the constrained-generation
	// fallback path: no tool is registered with the provider, the
	// system prompt carries the schema, and the dispatcher
	// validates+retries on malformed output.
	OutputTool() ToolSpec
}

// ToolSpec describes a vendor-agnostic structured-output tool. The
// schema is carried as a generic JSON-shaped map so skills can
// import a single source of truth (the JSON Schema file shipped at
// `/api/v1/skills/{id}/output_schema.json`) without coupling to a
// specific tokenizer or SDK type.
type ToolSpec struct {
	// Name is the tool the model is told to call (e.g. "emit_ordering").
	Name string

	// Description is the tool description the model sees. Anthropic
	// in particular uses this to decide whether to call the tool;
	// it should state the task and the expected output shape.
	Description string

	// InputSchema is the parsed JSON Schema describing the array
	// the model must produce. Stored as map[string]any so providers
	// can re-marshal into their native shape without an extra
	// vendor dependency at this layer.
	InputSchema map[string]any
}

// IsZero reports whether the spec is the constrained-generation
// fallback marker (no tool registered).
func (t ToolSpec) IsZero() bool { return t.Name == "" }

// SkillRegistry is the lookup table the dispatcher consults at
// request time. Like Registry, it is built once at startup and
// treated as read-only for concurrent reads thereafter.
type SkillRegistry struct {
	skills map[string]Skill
}

// NewSkillRegistry returns an empty SkillRegistry.
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{skills: make(map[string]Skill)}
}

// Register adds s to the registry. Returns an error when s is nil,
// s.ID() is empty, or another skill is already registered under that
// ID. Re-registering the same instance is a no-op so init paths can
// be defensive without erroring.
func (r *SkillRegistry) Register(s Skill) error {
	if s == nil {
		return errors.New("persona: SkillRegistry.Register: skill is nil")
	}
	id := s.ID()
	if strings.TrimSpace(id) == "" {
		return errors.New("persona: SkillRegistry.Register: skill ID is empty")
	}
	if existing, ok := r.skills[id]; ok {
		if existing == s {
			return nil
		}
		return fmt.Errorf("persona: SkillRegistry.Register: id %q already registered", id)
	}
	r.skills[id] = s
	return nil
}

// MustRegister panics on error. Use only at init().
func (r *SkillRegistry) MustRegister(s Skill) {
	if err := r.Register(s); err != nil {
		panic(err)
	}
}

// Get returns the skill registered under id. The bool is false when
// no skill is registered under that id.
func (r *SkillRegistry) Get(id string) (Skill, bool) {
	s, ok := r.skills[id]
	return s, ok
}

// List returns the registered skill IDs in ascending order.
func (r *SkillRegistry) List() []string {
	out := make([]string, 0, len(r.skills))
	for id := range r.skills {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// PersonaCanInvoke reports whether p declares skillID in its
// `skills` list. The dispatcher MUST consult this before resolving
// the skill in the registry — a skill being implemented and
// registered does not authorize every persona to invoke it.
func PersonaCanInvoke(p *Persona, skillID string) bool {
	if p == nil || skillID == "" {
		return false
	}
	for _, s := range p.Skills {
		if s == skillID {
			return true
		}
	}
	return false
}
