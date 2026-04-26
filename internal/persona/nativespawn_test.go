package persona

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// recordingOP captures the StartSession call inputs so the test can
// assert the SessionPrompt.Extension keys NativeSpawn populates.
// The zero value is a fully-functional fake that records one call.
type recordingOP struct {
	core.OrchestrationPlaneAdaptor
	startCalled bool
	gotAssign   string
	gotPrompt   core.SessionPrompt
	startErr    error
}

func (r *recordingOP) StartSession(_ context.Context, assignmentID string, prompt core.SessionPrompt) (core.Session, error) {
	r.startCalled = true
	r.gotAssign = assignmentID
	r.gotPrompt = prompt
	if r.startErr != nil {
		return core.Session{}, r.startErr
	}
	return core.Session{ID: assignmentID, Status: core.SessionInitializing}, nil
}

func TestNativeSpawn_RejectsNilDeps(t *testing.T) {
	cases := map[string]SpawnFunc{
		"nil-op":    NativeSpawn(nil, "claude"),
		"empty-typ": NativeSpawn(&recordingOP{}, ""),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			err := f(context.Background(), &Consult{ID: "consult-1"})
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNativeSpawn_RejectsNilConsult(t *testing.T) {
	op := &recordingOP{}
	f := NativeSpawn(op, "claude")
	if err := f(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil consult")
	}
	if op.startCalled {
		t.Error("StartSession should not be called when consult is nil")
	}
}

func TestNativeSpawn_PopulatesExpectedExtensionKeys(t *testing.T) {
	op := &recordingOP{}
	f := NativeSpawn(op, "claude")
	c := &Consult{
		ID:         "consult-test-1",
		PersonaID:  "project-manager",
		SkillID:    "epic_order",
		WorkingDir: "/work/repo-a",
	}
	if err := f(context.Background(), c); err != nil {
		t.Fatalf("spawn err = %v", err)
	}
	if !op.startCalled {
		t.Fatal("StartSession was not invoked")
	}
	if op.gotAssign != c.ID {
		t.Errorf("assignmentID = %q, want %q", op.gotAssign, c.ID)
	}
	ext := op.gotPrompt.Extension
	checks := map[string]any{
		"gemba:bead_id":             c.ID,
		"gemba:agent_type":          "claude",
		"gemba:workspace":           c.WorkingDir,
		"gemba:session_id_override": c.ID,
	}
	for k, want := range checks {
		if got := ext[k]; got != want {
			t.Errorf("ext[%q] = %v, want %v", k, got, want)
		}
	}
	if title, _ := ext["gemba:title"].(string); title == "" {
		t.Error("gemba:title is empty; spawned-pane label would be the backend default")
	}
}

func TestNativeSpawn_PropagatesStartSessionError(t *testing.T) {
	op := &recordingOP{startErr: errors.New("backend exploded")}
	f := NativeSpawn(op, "claude")
	err := f(context.Background(), &Consult{ID: "c-1", WorkingDir: "/w"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, op.startErr) {
		t.Errorf("err = %v, want wrapping %v", err, op.startErr)
	}
}

func TestSetSpawnFunc_NilClearsCallback(t *testing.T) {
	d := NewDispatcher(NewAuditLog(t.TempDir()))
	called := 0
	d.SetSpawnFunc(func(context.Context, *Consult) error { called++; return nil })
	if err := d.MaybeSpawn(context.Background(), &Consult{ID: "c-1"}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("called = %d, want 1", called)
	}
	d.SetSpawnFunc(nil)
	if err := d.MaybeSpawn(context.Background(), &Consult{ID: "c-2"}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("called = %d after clear, want 1 (dry-run)", called)
	}
}

func TestMaybeSpawn_NilFuncIsNoOp(t *testing.T) {
	d := NewDispatcher(NewAuditLog(t.TempDir()))
	if err := d.MaybeSpawn(context.Background(), &Consult{ID: "c-1"}); err != nil {
		t.Errorf("MaybeSpawn with no func should be no-op; got %v", err)
	}
}

func TestMaybeSpawn_PropagatesError(t *testing.T) {
	d := NewDispatcher(NewAuditLog(t.TempDir()))
	want := errors.New("spawn went sideways")
	d.SetSpawnFunc(func(context.Context, *Consult) error { return want })
	if err := d.MaybeSpawn(context.Background(), &Consult{ID: "c-1"}); !errors.Is(err, want) {
		t.Errorf("err = %v, want wrapping %v", err, want)
	}
}
