package escalation_handoff

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/core/persona"
)

func minimalInput() map[string]any {
	return map[string]any{
		"escalation_id":  "esc-42",
		"title":          "Approve risky write",
		"prompt":         "May I delete logs?",
		"assignment_id":  "tmux:gm-42:1",
		"target_persona": "project-manager",
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestSkill_StaticMetadata(t *testing.T) {
	s := New()
	if s.ID() != ID {
		t.Errorf("ID = %q, want %q", s.ID(), ID)
	}
	if s.Name() == "" {
		t.Error("Name must not be empty")
	}
	if s.Description() == "" {
		t.Error("Description must not be empty")
	}
	if s.SkillPrompt() == "" {
		t.Error("SkillPrompt must not be empty")
	}
	if got := s.OutputTool(); got.Name != "emit_handoff_response" {
		t.Errorf("OutputTool.Name = %q, want emit_handoff_response", got.Name)
	}
}

func TestSkill_RegisterRoundtrip(t *testing.T) {
	r := persona.NewSkillRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get(ID)
	if !ok {
		t.Fatalf("registry missing %q after Register", ID)
	}
	if got.ID() != ID {
		t.Errorf("registered skill ID = %q, want %q", got.ID(), ID)
	}
}

func TestValidateInput_Accepts(t *testing.T) {
	s := New()
	out, err := s.ValidateInput(mustJSON(t, minimalInput()))
	if err != nil {
		t.Fatalf("ValidateInput: %v", err)
	}
	in, ok := out.(*HandoffInput)
	if !ok {
		t.Fatalf("ValidateInput: got %T, want *HandoffInput", out)
	}
	if in.EscalationID != "esc-42" {
		t.Errorf("EscalationID = %q, want esc-42", in.EscalationID)
	}
	if in.Title != "Approve risky write" {
		t.Errorf("Title = %q", in.Title)
	}
}

func TestValidateInput_AcceptsMinimal(t *testing.T) {
	// prompt / assignment_id / target_persona are optional.
	s := New()
	body := map[string]any{"escalation_id": "esc-1", "title": "Just a title"}
	if _, err := s.ValidateInput(mustJSON(t, body)); err != nil {
		t.Fatalf("ValidateInput: %v", err)
	}
}

func TestValidateInput_RejectsEmptyBody(t *testing.T) {
	s := New()
	if _, err := s.ValidateInput(nil); err == nil {
		t.Error("ValidateInput: want error on empty body")
	}
}

func TestValidateInput_RejectsMissingEscalationID(t *testing.T) {
	s := New()
	body := minimalInput()
	delete(body, "escalation_id")
	if _, err := s.ValidateInput(mustJSON(t, body)); err == nil {
		t.Error("ValidateInput: want error when escalation_id missing")
	}
}

func TestValidateInput_RejectsMissingTitle(t *testing.T) {
	s := New()
	body := minimalInput()
	delete(body, "title")
	if _, err := s.ValidateInput(mustJSON(t, body)); err == nil {
		t.Error("ValidateInput: want error when title missing")
	}
}

func TestValidateInput_RejectsUnknownField(t *testing.T) {
	s := New()
	body := minimalInput()
	body["bogus_field"] = "nope"
	if _, err := s.ValidateInput(mustJSON(t, body)); err == nil {
		t.Error("ValidateInput: want error on unknown field")
	}
}

func TestValidateOutputLine_AcceptsResponse(t *testing.T) {
	s := New()
	raw := mustJSON(t, map[string]any{"type": "response", "message": "I would defer this."})
	out, err := s.ValidateOutputLine(raw)
	if err != nil {
		t.Fatalf("ValidateOutputLine: %v", err)
	}
	got, ok := out.(*ResponseLine)
	if !ok {
		t.Fatalf("ValidateOutputLine: got %T, want *ResponseLine", out)
	}
	if got.Message != "I would defer this." {
		t.Errorf("Message = %q", got.Message)
	}
}

func TestValidateOutputLine_AcceptsSummary(t *testing.T) {
	s := New()
	raw := mustJSON(t, map[string]any{
		"type":          "summary",
		"escalation_id": "esc-42",
		"disposition":   "approve",
		"note":          "Looks safe.",
	})
	out, err := s.ValidateOutputLine(raw)
	if err != nil {
		t.Fatalf("ValidateOutputLine: %v", err)
	}
	got, ok := out.(*SummaryLine)
	if !ok {
		t.Fatalf("ValidateOutputLine: got %T, want *SummaryLine", out)
	}
	if got.Disposition != "approve" {
		t.Errorf("Disposition = %q", got.Disposition)
	}
	if got.Note == "" {
		t.Error("Note must not be empty")
	}
}

func TestValidateOutputLine_RejectsEmptyResponse(t *testing.T) {
	s := New()
	raw := mustJSON(t, map[string]any{"type": "response", "message": "   "})
	if _, err := s.ValidateOutputLine(raw); err == nil {
		t.Error("ValidateOutputLine: want error on whitespace-only message")
	}
}

func TestValidateOutputLine_RejectsSummaryMissingNote(t *testing.T) {
	s := New()
	raw := mustJSON(t, map[string]any{
		"type":          "summary",
		"escalation_id": "esc-42",
		"note":          "",
	})
	if _, err := s.ValidateOutputLine(raw); err == nil {
		t.Error("ValidateOutputLine: want error on empty note")
	}
}

func TestValidateOutputLine_RejectsUnknownType(t *testing.T) {
	s := New()
	raw := mustJSON(t, map[string]any{"type": "wibble"})
	if _, err := s.ValidateOutputLine(raw); err == nil {
		t.Error("ValidateOutputLine: want error on unknown type")
	}
}

func TestSkillPrompt_ContainsContract(t *testing.T) {
	s := New()
	prompt := s.SkillPrompt()
	for _, want := range []string{"emit_handoff_response", "summary", "escalation_id"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("SkillPrompt missing %q", want)
		}
	}
}
