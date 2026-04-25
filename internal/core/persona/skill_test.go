package persona

import (
	"encoding/json"
	"errors"
	"testing"
)

// fakeSkill is a hand-rolled Skill for exercising the registry and
// authorization helpers. It avoids importing a real skill package,
// which would create a cycle.
type fakeSkill struct {
	id      string
	name    string
	desc    string
	prompt  string
	tool    ToolSpec
	inErr   error
	outErr  error
	in      func(raw json.RawMessage) (any, error)
	outLine func(raw json.RawMessage) (any, error)
}

func (f *fakeSkill) ID() string          { return f.id }
func (f *fakeSkill) Name() string        { return f.name }
func (f *fakeSkill) Description() string { return f.desc }
func (f *fakeSkill) SkillPrompt() string { return f.prompt }
func (f *fakeSkill) OutputTool() ToolSpec {
	return f.tool
}
func (f *fakeSkill) ValidateInput(raw json.RawMessage) (any, error) {
	if f.in != nil {
		return f.in(raw)
	}
	return raw, f.inErr
}
func (f *fakeSkill) ValidateOutputLine(raw json.RawMessage) (any, error) {
	if f.outLine != nil {
		return f.outLine(raw)
	}
	return raw, f.outErr
}

func TestSkillRegistry_RegisterAndLookup(t *testing.T) {
	r := NewSkillRegistry()
	s := &fakeSkill{id: "epic_order", name: "Epic order"}

	if err := r.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(s); err != nil {
		t.Errorf("idempotent re-register of same pointer: %v", err)
	}

	other := &fakeSkill{id: "epic_order"}
	if err := r.Register(other); err == nil {
		t.Error("expected duplicate-id error")
	}

	got, ok := r.Get("epic_order")
	if !ok || got != s {
		t.Errorf("Get returned (%v, %v); want (%p, true)", got, ok, s)
	}

	if err := r.Register(nil); err == nil {
		t.Error("expected error on nil skill")
	}
	if err := r.Register(&fakeSkill{id: ""}); err == nil {
		t.Error("expected error on empty-id skill")
	}
}

func TestSkillRegistry_ListAscending(t *testing.T) {
	r := NewSkillRegistry()
	r.MustRegister(&fakeSkill{id: "what_remains"})
	r.MustRegister(&fakeSkill{id: "epic_order"})
	r.MustRegister(&fakeSkill{id: "add_to_backlog"})

	got := r.List()
	want := []string{"add_to_backlog", "epic_order", "what_remains"}
	if len(got) != len(want) {
		t.Fatalf("List length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPersonaCanInvoke(t *testing.T) {
	pm := &Persona{ID: "project-manager", Skills: []string{"epic_order", "what_remains"}}

	if !PersonaCanInvoke(pm, "epic_order") {
		t.Error("PM should be able to invoke epic_order")
	}
	if PersonaCanInvoke(pm, "release_notes") {
		t.Error("PM should NOT be able to invoke release_notes (not in its skills list)")
	}
	if PersonaCanInvoke(nil, "epic_order") {
		t.Error("nil persona should not invoke any skill")
	}
	if PersonaCanInvoke(pm, "") {
		t.Error("empty skill ID should never authorize")
	}
}

func TestToolSpec_IsZero(t *testing.T) {
	if !(ToolSpec{}).IsZero() {
		t.Error("zero ToolSpec should report IsZero")
	}
	if (ToolSpec{Name: "emit_ordering"}).IsZero() {
		t.Error("ToolSpec with Name should NOT report IsZero")
	}
}

func TestSkillValidateInput_ErrorPropagates(t *testing.T) {
	want := errors.New("schema mismatch: missing candidate_epics")
	s := &fakeSkill{id: "epic_order", inErr: want}

	_, err := s.ValidateInput(json.RawMessage(`{}`))
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}
