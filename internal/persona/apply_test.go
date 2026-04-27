package persona

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// applyTestDispatcher seeds a dispatcher with one running consult
// that already has two validated lines. Returns the dispatcher and
// the consult id so tests can drive Apply.
func applyTestDispatcher(t *testing.T) (*Dispatcher, string) {
	t.Helper()
	d := NewDispatcher(NewAuditLog(t.TempDir()),
		WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
		WithIDFunc(func() string { return "consult-apply-1" }),
		WithWorkspaceDir(t.TempDir()),
	)
	skill := fakeSkill{id: "test"}
	persona := &corepersona.Persona{
		ID: "p", Name: "P", Role: "P",
		Variety:      corepersona.VarietyCoach,
		Scope:        corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:       []string{"test"},
		SystemPrompt: "p",
	}
	c, err := d.Begin(BeginRequest{
		Persona:   persona,
		Skill:     skill,
		Workspace: "ws",
		RawInput:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Receive(c.ID, []json.RawMessage{
		json.RawMessage(`{"type":"recommendation","rank":1}`),
		json.RawMessage(`{"type":"recommendation","rank":2}`),
	}); err != nil {
		t.Fatal(err)
	}
	return d, c.ID
}

func TestApply_RecordsAndReturnsLine(t *testing.T) {
	d, id := applyTestDispatcher(t)
	res, err := d.Apply(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.AppliedIdx) != 1 || res.AppliedIdx[0] != 0 {
		t.Errorf("AppliedIdx = %v, want [0]", res.AppliedIdx)
	}
	if res.Line == nil {
		t.Error("Line nil; want the validated entry at idx 0")
	}
	c, _ := d.Get(id)
	if len(c.AppliedIdx) != 1 || c.AppliedIdx[0] != 0 {
		t.Errorf("consult.AppliedIdx = %v, want [0]", c.AppliedIdx)
	}
}

func TestApply_AppendsMultipleAppliesInOrder(t *testing.T) {
	d, id := applyTestDispatcher(t)
	if _, err := d.Apply(context.Background(), id, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	c, _ := d.Get(id)
	if len(c.AppliedIdx) != 2 || c.AppliedIdx[0] != 1 || c.AppliedIdx[1] != 0 {
		t.Errorf("AppliedIdx = %v, want [1,0] (call order preserved)", c.AppliedIdx)
	}
}

func TestApply_RejectsDuplicateIdx(t *testing.T) {
	d, id := applyTestDispatcher(t)
	if _, err := d.Apply(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	_, err := d.Apply(context.Background(), id, 0)
	if err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Errorf("err = %v, want 'already recorded'", err)
	}
}

func TestApply_RejectsOutOfRange(t *testing.T) {
	d, id := applyTestDispatcher(t)
	cases := []int{-1, 2, 99}
	for _, idx := range cases {
		_, err := d.Apply(context.Background(), id, idx)
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("Apply(%d) err = %v, want out-of-range", idx, err)
		}
	}
}

func TestApply_RejectsUnknownConsult(t *testing.T) {
	d, _ := applyTestDispatcher(t)
	_, err := d.Apply(context.Background(), "never-existed", 0)
	if err == nil || !strings.Contains(err.Error(), "unknown consult_id") {
		t.Errorf("err = %v, want unknown consult_id", err)
	}
}

func TestApply_EmptyConsultIDRejected(t *testing.T) {
	d := NewDispatcher(NewAuditLog(t.TempDir()))
	if _, err := d.Apply(context.Background(), "", 0); err == nil {
		t.Error("expected error for empty consult id")
	}
}

func TestFinish_PersistsAppliedIdxToAuditRecord(t *testing.T) {
	d, id := applyTestDispatcher(t)
	if _, err := d.Apply(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(context.Background(), id, 1); err != nil {
		t.Fatal(err)
	}
	rec, err := d.Finish(id, FinishInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.AppliedIdx) != 2 || rec.AppliedIdx[0] != 0 || rec.AppliedIdx[1] != 1 {
		t.Errorf("audit record AppliedIdx = %v, want [0,1]", rec.AppliedIdx)
	}
}

func TestApply_DoesNotErrUnavailableSentinel(t *testing.T) {
	// Apply errors should be plain — no unwraps to ErrUnsupported
	// or context.Canceled. A future caller relying on errors.Is
	// against well-known sentinels should never accidentally match.
	d, _ := applyTestDispatcher(t)
	_, err := d.Apply(context.Background(), "missing", 0)
	if errors.Is(err, context.Canceled) {
		t.Errorf("Apply error matched context.Canceled: %v", err)
	}
}
