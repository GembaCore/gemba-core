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

// fakeApplier records invocations and returns a canned result or
// error. Lets tests pin executor behavior without dragging in a
// real WorkPlane.
type fakeApplier struct {
	calls []any
	res   ApplierResult
	err   error
}

func (f *fakeApplier) Apply(_ context.Context, line any) (ApplierResult, error) {
	f.calls = append(f.calls, line)
	return f.res, f.err
}

func newApplierTestDispatcher(t *testing.T) (*Dispatcher, string) {
	t.Helper()
	d := NewDispatcher(NewAuditLog(t.TempDir()),
		WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
		WithIDFunc(func() string { return "consult-applier-1" }),
		WithWorkspaceDir(t.TempDir()),
	)
	skill := fakeSkill{id: "test-skill"}
	persona := &corepersona.Persona{
		ID: "p", Name: "P", Role: "P",
		Variety:      corepersona.VarietyCoach,
		Scope:        corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:       []string{"test-skill"},
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

func TestApplyExecutor_NoApplierRegistered_RecordOnly(t *testing.T) {
	d, id := newApplierTestDispatcher(t)
	res, err := d.Apply(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Executed {
		t.Errorf("Executed=true with no applier; want false (record-only)")
	}
	if res.Executor.Detail != "" {
		t.Errorf("Executor.Detail = %q; want empty in record-only mode", res.Executor.Detail)
	}
}

func TestApplyExecutor_RegisteredApplierFires(t *testing.T) {
	d, id := newApplierTestDispatcher(t)
	fa := &fakeApplier{
		res: ApplierResult{Detail: "PATCHed gm-1", Body: json.RawMessage(`{"id":"gm-1"}`)},
	}
	d.SetApplier("test-skill", fa)
	res, err := d.Apply(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Executed {
		t.Errorf("Executed=false with registered applier; want true")
	}
	if res.Executor.Detail != "PATCHed gm-1" {
		t.Errorf("Executor.Detail = %q, want PATCHed gm-1", res.Executor.Detail)
	}
	if len(fa.calls) != 1 {
		t.Errorf("applier called %d times, want 1", len(fa.calls))
	}
	// AppliedIdx still appended.
	c, _ := d.Get(id)
	if len(c.AppliedIdx) != 1 || c.AppliedIdx[0] != 0 {
		t.Errorf("AppliedIdx = %v, want [0]", c.AppliedIdx)
	}
}

func TestApplyExecutor_FailureLeavesAppliedIdxUnchanged(t *testing.T) {
	d, id := newApplierTestDispatcher(t)
	want := errors.New("workplane refused")
	d.SetApplier("test-skill", &fakeApplier{err: want})

	_, err := d.Apply(context.Background(), id, 0)
	if err == nil {
		t.Fatal("Apply did not surface applier error")
	}
	if !strings.Contains(err.Error(), "workplane refused") {
		t.Errorf("err = %v; want wrapping the applier error", err)
	}

	// AppliedIdx must NOT have been appended — operator's retry
	// would otherwise hit the duplicate gate (409).
	c, _ := d.Get(id)
	if len(c.AppliedIdx) != 0 {
		t.Errorf("AppliedIdx = %v after applier failure; want empty so retry is allowed", c.AppliedIdx)
	}

	// Retry without the applier failing succeeds and lands the
	// AppliedIdx — proves the rollback semantics work.
	d.SetApplier("test-skill", &fakeApplier{res: ApplierResult{Detail: "ok"}})
	if _, err := d.Apply(context.Background(), id, 0); err != nil {
		t.Fatalf("retry Apply: %v", err)
	}
	c, _ = d.Get(id)
	if len(c.AppliedIdx) != 1 {
		t.Errorf("AppliedIdx after successful retry = %v, want [0]", c.AppliedIdx)
	}
}

func TestApplyExecutor_DuplicateGateStillFiresWithApplier(t *testing.T) {
	d, id := newApplierTestDispatcher(t)
	d.SetApplier("test-skill", &fakeApplier{res: ApplierResult{Detail: "ok"}})
	if _, err := d.Apply(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	_, err := d.Apply(context.Background(), id, 0)
	if err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Errorf("duplicate err = %v, want already-recorded", err)
	}
}

func TestSetApplier_NilClearsRegistration(t *testing.T) {
	d, id := newApplierTestDispatcher(t)
	fa := &fakeApplier{res: ApplierResult{Detail: "ok"}}
	d.SetApplier("test-skill", fa)
	if _, err := d.Apply(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	if len(fa.calls) != 1 {
		t.Errorf("applier not called: %d", len(fa.calls))
	}

	// Clear the registration; second apply at idx 1 should be
	// record-only (no executor invocation).
	d.SetApplier("test-skill", nil)
	res, err := d.Apply(context.Background(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Executed {
		t.Errorf("Executed=true after SetApplier(nil); want false")
	}
	if len(fa.calls) != 1 {
		t.Errorf("applier called after clear: %d (want 1, the pre-clear call)", len(fa.calls))
	}
}

func TestApplyExecutor_DifferentSkillIDDoesNotFire(t *testing.T) {
	d, id := newApplierTestDispatcher(t)
	fa := &fakeApplier{res: ApplierResult{Detail: "ok"}}
	d.SetApplier("other-skill", fa)
	res, err := d.Apply(context.Background(), id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Executed {
		t.Errorf("Executed=true with applier for different skill; want false")
	}
	if len(fa.calls) != 0 {
		t.Errorf("foreign-skill applier called: %d", len(fa.calls))
	}
}
