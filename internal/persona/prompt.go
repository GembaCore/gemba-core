package persona

import (
	"encoding/json"
	"fmt"
	"strings"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/core/prompt"
)

// TemplateValues are the substitutions applied to a persona's
// system_prompt before it lands in [prompt.Envelope.PersonaSystem].
//
// Keep this set small on purpose: every substitution is a load-bearing
// promise to persona authors, so growing the surface adds breakage
// risk on every persona file. Add new fields only when an existing
// persona's prompt would otherwise need workspace-runtime data
// inlined unsafely.
type TemplateValues struct {
	// WorkspaceName is the human-readable workspace label
	// ("Gemba", "Bullet City", …). Substitutes {{workspace_name}}.
	WorkspaceName string

	// Role is the persona's role string ("Project Manager", …).
	// Substitutes {{role}}. Pulled from [corepersona.Persona.Role]
	// so it stays in lockstep with the persona file.
	Role string

	// ProjectPrefix is the bead-id prefix for the workspace
	// ("gm" for Gemba). Substitutes {{project_prefix}}; the PM
	// persona uses it to cite Epic IDs by example.
	ProjectPrefix string
}

// ApplyTemplate runs the supported substitutions on s. Unknown
// placeholders pass through unchanged so a typo in a persona file
// surfaces as visible noise to the operator (and the model) rather
// than failing the consult.
func (v TemplateValues) ApplyTemplate(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	out := s
	out = strings.ReplaceAll(out, "{{workspace_name}}", v.WorkspaceName)
	out = strings.ReplaceAll(out, "{{role}}", v.Role)
	out = strings.ReplaceAll(out, "{{project_prefix}}", v.ProjectPrefix)
	return out
}

// ComposeRequest carries the inputs to [Compose]. The dispatcher
// builds it once per consult; tests construct it directly.
type ComposeRequest struct {
	// Persona is the loaded persona definition. Required.
	Persona *corepersona.Persona

	// Skill is the resolved skill instance. Required.
	Skill corepersona.Skill

	// Template carries the substitution values. Required when the
	// persona's system_prompt contains placeholders; optional
	// otherwise.
	Template TemplateValues

	// Input is the validated, typed skill input (the value
	// [corepersona.Skill.ValidateInput] returned). Required. Must
	// JSON-marshal cleanly because the rendering path serialises
	// it into a fenced block in the user message.
	Input any

	// Guidance is the operator's free-form intent string. Optional;
	// rendered after the input block when non-empty.
	Guidance string

	// ProjectValues / ProjectGuardrails / ProjectGoals /
	// WorkspaceValues / ContextChunks override the matching layers
	// of [prompt.Envelope]. The dispatcher passes through whatever
	// the workspace's promptctx providers returned; a zero-value
	// slice means "no contribution from that layer."
	ProjectValues     []string
	ProjectGuardrails []string
	ProjectGoals      []string
	WorkspaceValues   []string
	ContextChunks     []string

	// WalkID, when non-empty, appends the walk-mode system prompt
	// fragment to the persona's base prompt (gm-4p6). The HTTP
	// layer detects an active walk via walk.IDFromContext on the
	// inbound request and passes the id through; a zero value
	// keeps the prompt unchanged so non-walk consults pay nothing.
	// The id itself is also rendered into the fragment so a debug
	// trail can grep prompts by walk.
	WalkID string
}

// Composed is the result of [Compose]: the system prompt the
// provider sends as the system role, and the user message that
// becomes the first turn. Diagnostics carries the per-layer
// truncation info from the underlying envelope so a caller can
// log "context dropped 3 chunks" without re-walking the prompt.
type Composed struct {
	System      string
	User        string
	Diagnostics []prompt.LayerOutput
	TotalTokens int
}

// Compose builds the final (system, user) prompt pair for a
// persona consult.
//
// System layer assembly walks [prompt.DefaultOrder] up to (but not
// including) LayerUserInput. The user message is produced
// separately so the provider can send it as a user-role turn,
// which providers vary on how they handle when collapsed into the
// system prompt.
//
// Returns an error only on malformed input: nil persona/skill, or
// an Input that fails json.Marshal. Token-budget truncation is
// reported via Composed.Diagnostics, never as an error — losing
// the oldest items is the design (gm-3d6).
func Compose(req ComposeRequest) (Composed, error) {
	if req.Persona == nil {
		return Composed{}, fmt.Errorf("persona/prompt: persona must not be nil")
	}
	if req.Skill == nil {
		return Composed{}, fmt.Errorf("persona/prompt: skill must not be nil")
	}
	if req.Input == nil {
		return Composed{}, fmt.Errorf("persona/prompt: input must not be nil")
	}

	personaSystem := req.Template.ApplyTemplate(req.Persona.SystemPrompt)
	personaSystem = strings.TrimSpace(personaSystem)
	if ext := WalkPromptExtension(req.WalkID); ext != "" {
		// Append walk-mode framing only when a walk is active
		// (gm-4p6). Persona's base rules are preserved verbatim
		// above; the fragment teaches the model the per-item
		// frame/propose/yield/apply rhythm.
		personaSystem = personaSystem + "\n\n" + strings.TrimSpace(ext)
	}
	skillFragment := strings.TrimSpace(req.Skill.SkillPrompt())

	env := prompt.Envelope{
		ProjectValues:     req.ProjectValues,
		ProjectGuardrails: req.ProjectGuardrails,
		ProjectGoals:      req.ProjectGoals,
		WorkspaceValues:   req.WorkspaceValues,
		PersonaSystem:     personaSystem,
		Skill:             skillFragment,
		ContextChunks:     req.ContextChunks,
		// UserInput is rendered separately into the user message;
		// leaving it blank here keeps the system prompt free of
		// the per-call payload.
	}

	composed := env.Compose(prompt.ComposeOptions{})

	user, err := renderUserMessage(req)
	if err != nil {
		return Composed{}, err
	}

	return Composed{
		System:      composed.Text,
		User:        user,
		Diagnostics: composed.Layers,
		TotalTokens: composed.TotalTokens,
	}, nil
}

// renderUserMessage produces the per-call user-role turn. Format:
//
//	## Input
//
//	```json
//	<pretty-printed JSON>
//	```
//
//	## Guidance (when non-empty)
//
//	<text>
//
// JSON pretty-printing makes the input legible in an audit log; the
// model parses it just as well as compact form. Guidance is
// optional and trimmed.
func renderUserMessage(req ComposeRequest) (string, error) {
	body, err := json.MarshalIndent(req.Input, "", "  ")
	if err != nil {
		return "", fmt.Errorf("persona/prompt: marshal input: %w", err)
	}

	var b strings.Builder
	b.WriteString("## Input\n\n")
	b.WriteString("```json\n")
	b.Write(body)
	b.WriteString("\n```")

	if g := strings.TrimSpace(req.Guidance); g != "" {
		b.WriteString("\n\n## Guidance\n\n")
		b.WriteString(g)
	}

	return b.String(), nil
}
