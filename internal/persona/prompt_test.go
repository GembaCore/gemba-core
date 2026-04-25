package persona

import (
	"encoding/json"
	"strings"
	"testing"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// fakeSkill is the test-only skill stand-in. The full epic_order
// implementation would also work but pulling it in just for these
// tests inverts the package layering — internal/persona is a
// dependency of the dispatcher, not of the skills.
type fakeSkill struct {
	id     string
	prompt string
}

func (f fakeSkill) ID() string                                                  { return f.id }
func (f fakeSkill) Name() string                                                { return f.id }
func (f fakeSkill) Description() string                                         { return "test fake" }
func (f fakeSkill) SkillPrompt() string                                         { return f.prompt }
func (f fakeSkill) OutputTool() corepersona.ToolSpec                            { return corepersona.ToolSpec{} }
func (f fakeSkill) ValidateInput(raw json.RawMessage) (any, error)              { return raw, nil }
func (f fakeSkill) ValidateOutputLine(raw json.RawMessage) (any, error)         { return raw, nil }

func samplePersona() *corepersona.Persona {
	return &corepersona.Persona{
		ID:           "project-manager",
		Name:         "PM",
		Role:         "Project Manager",
		Variety:      corepersona.VarietyCoach,
		SystemPrompt: "You are Gemba's {{role}} for the {{workspace_name}} workspace. Cite Epics like {{project_prefix}}-e3.",
	}
}

func TestApplyTemplate_AllPlaceholders(t *testing.T) {
	v := TemplateValues{
		WorkspaceName: "Gemba",
		Role:          "Project Manager",
		ProjectPrefix: "gm",
	}
	got := v.ApplyTemplate("{{role}} of {{workspace_name}} cites {{project_prefix}}-e3")
	want := "Project Manager of Gemba cites gm-e3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyTemplate_NoPlaceholdersBypass(t *testing.T) {
	v := TemplateValues{}
	if got := v.ApplyTemplate("plain text"); got != "plain text" {
		t.Errorf("plain text mutated: %q", got)
	}
}

func TestApplyTemplate_UnknownPlaceholderPassesThrough(t *testing.T) {
	// A typo in a persona file leaves visible noise rather than
	// silently dropping the line — operator (and model) sees the
	// problem.
	v := TemplateValues{WorkspaceName: "G"}
	got := v.ApplyTemplate("{{unknown_var}} {{workspace_name}}")
	want := "{{unknown_var}} G"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyTemplate_EmptyValuesProduceEmptySubstitutions(t *testing.T) {
	v := TemplateValues{} // all fields empty
	got := v.ApplyTemplate("[{{workspace_name}}]")
	if got != "[]" {
		t.Errorf("empty workspace_name should substitute to empty, got %q", got)
	}
}

func TestCompose_HappyPath(t *testing.T) {
	req := ComposeRequest{
		Persona: samplePersona(),
		Skill: fakeSkill{
			id:     "epic_order",
			prompt: "Rank the candidate Epics. Emit ordered array.",
		},
		Template: TemplateValues{
			WorkspaceName: "Gemba",
			Role:          "Project Manager",
			ProjectPrefix: "gm",
		},
		Input: map[string]any{
			"workspace": "gemba",
			"as_of":     "2026-04-25T00:00:00Z",
			"candidate_epics": []map[string]any{
				{"epic_id": "gm-e3", "title": "Plan view"},
			},
		},
		Guidance: "focus on unblocking UI work",
		ProjectGoals: []string{"Ship Plan view by end of sprint."},
	}
	got, err := Compose(req)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	// System prompt: persona substitutions applied.
	if !strings.Contains(got.System, "Project Manager") {
		t.Errorf("system missing role substitution:\n%s", got.System)
	}
	if !strings.Contains(got.System, "Gemba workspace") {
		t.Errorf("system missing workspace substitution:\n%s", got.System)
	}
	if !strings.Contains(got.System, "gm-e3") {
		t.Errorf("system missing project_prefix substitution:\n%s", got.System)
	}

	// System prompt: skill fragment present.
	if !strings.Contains(got.System, "Rank the candidate Epics") {
		t.Errorf("system missing skill fragment:\n%s", got.System)
	}

	// System prompt: project goal present.
	if !strings.Contains(got.System, "Ship Plan view") {
		t.Errorf("system missing project goal:\n%s", got.System)
	}

	// User message: input as fenced JSON + guidance section.
	if !strings.Contains(got.User, "## Input") {
		t.Errorf("user missing Input header:\n%s", got.User)
	}
	if !strings.Contains(got.User, "```json") {
		t.Errorf("user missing JSON fence:\n%s", got.User)
	}
	if !strings.Contains(got.User, "gm-e3") {
		t.Errorf("user missing input payload:\n%s", got.User)
	}
	if !strings.Contains(got.User, "## Guidance") {
		t.Errorf("user missing Guidance header:\n%s", got.User)
	}
	if !strings.Contains(got.User, "focus on unblocking") {
		t.Errorf("user missing guidance body:\n%s", got.User)
	}

	if got.TotalTokens <= 0 {
		t.Errorf("TotalTokens = %d, want > 0", got.TotalTokens)
	}
	if len(got.Diagnostics) == 0 {
		t.Error("Diagnostics empty; want one entry per emitted layer")
	}
}

func TestCompose_NoGuidanceOmitsHeader(t *testing.T) {
	req := ComposeRequest{
		Persona:  samplePersona(),
		Skill:    fakeSkill{id: "epic_order", prompt: "x"},
		Template: TemplateValues{WorkspaceName: "Gemba", Role: "PM"},
		Input:    map[string]any{"k": "v"},
	}
	got, err := Compose(req)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if strings.Contains(got.User, "Guidance") {
		t.Errorf("empty guidance must not emit header:\n%s", got.User)
	}
}

func TestCompose_GuidanceWhitespaceOnlyOmitsHeader(t *testing.T) {
	req := ComposeRequest{
		Persona:  samplePersona(),
		Skill:    fakeSkill{id: "epic_order", prompt: "x"},
		Template: TemplateValues{WorkspaceName: "G"},
		Input:    map[string]any{},
		Guidance: "   \n\t  ",
	}
	got, err := Compose(req)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if strings.Contains(got.User, "Guidance") {
		t.Errorf("whitespace-only guidance must not emit header:\n%s", got.User)
	}
}

func TestCompose_RequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		req     ComposeRequest
		wantSub string
	}{
		{
			name:    "nil persona",
			req:     ComposeRequest{Skill: fakeSkill{id: "x"}, Input: map[string]any{}},
			wantSub: "persona must not be nil",
		},
		{
			name:    "nil skill",
			req:     ComposeRequest{Persona: samplePersona(), Input: map[string]any{}},
			wantSub: "skill must not be nil",
		},
		{
			name:    "nil input",
			req:     ComposeRequest{Persona: samplePersona(), Skill: fakeSkill{id: "x"}},
			wantSub: "input must not be nil",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Compose(c.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestCompose_InputMustMarshal(t *testing.T) {
	// A channel can't be json-marshalled — Compose surfaces the
	// underlying error so the dispatcher can return 400 to the caller.
	req := ComposeRequest{
		Persona: samplePersona(),
		Skill:   fakeSkill{id: "x"},
		Input:   make(chan int),
	}
	_, err := Compose(req)
	if err == nil {
		t.Fatal("expected error on unmarshalable input")
	}
	if !strings.Contains(err.Error(), "marshal input") {
		t.Errorf("err = %v, want marshal-input wrap", err)
	}
}

func TestCompose_DiagnosticsExposeLayerCounts(t *testing.T) {
	// Diagnostics should list each emitted layer; downstream
	// observability uses this for "context layer truncated 2 items"
	// surfacing without re-parsing the prompt.
	req := ComposeRequest{
		Persona:           samplePersona(),
		Skill:             fakeSkill{id: "x", prompt: "y"},
		Template:          TemplateValues{WorkspaceName: "G"},
		Input:             map[string]any{},
		ProjectValues:     []string{"transparency"},
		ProjectGuardrails: []string{"never ship without tests"},
	}
	got, err := Compose(req)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	layers := map[string]bool{}
	for _, d := range got.Diagnostics {
		layers[string(d.Layer)] = true
	}
	for _, want := range []string{"project_values", "project_guardrails", "persona_system", "skill"} {
		if !layers[want] {
			t.Errorf("diagnostics missing layer %q (got %v)", want, layers)
		}
	}
}
