package persona

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/native/claudemd"
	"github.com/MikeBengtson/gemba/core"
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
	ws := t.TempDir()
	c := &Consult{
		ID:         "consult-test-1",
		PersonaID:  "project-manager",
		SkillID:    "epic_order",
		WorkingDir: ws,
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
	err := f(context.Background(), &Consult{ID: "c-1", WorkingDir: t.TempDir()})
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

func TestNativeSpawn_WritesComposedPromptToClaudeMD(t *testing.T) {
	op := &recordingOP{}
	f := NativeSpawn(op, "claude")
	ws := t.TempDir()
	c := &Consult{
		ID:         "consult-pre-1",
		PersonaID:  "pm",
		SkillID:    "epic_order",
		WorkingDir: ws,
		Composed: Composed{
			System: "you are the project manager",
			User:   "rank these epics",
		},
	}
	if err := f(context.Background(), c); err != nil {
		t.Fatalf("spawn err = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(ws, claudemd.FileName))
	if err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
	body := string(got)
	for _, want := range []string{
		claudemd.SentinelBegin,
		claudemd.SentinelEnd,
		"you are the project manager",
		"rank these epics",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("CLAUDE.md missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestNativeSpawn_PreservesOperatorContent(t *testing.T) {
	op := &recordingOP{}
	f := NativeSpawn(op, "claude")
	ws := t.TempDir()
	original := "# my notes\n\nhand-authored content\n"
	if err := os.WriteFile(filepath.Join(ws, claudemd.FileName), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Consult{
		ID:         "consult-pre-2",
		WorkingDir: ws,
		Composed:   Composed{System: "consult system", User: "consult user"},
	}
	if err := f(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, claudemd.FileName))
	if !strings.Contains(string(got), "hand-authored content") {
		t.Error("operator content wiped by spawn preamble write")
	}
}

func TestNativeSpawn_RemovesPreambleOnSpawnFailure(t *testing.T) {
	op := &recordingOP{startErr: errors.New("backend exploded")}
	f := NativeSpawn(op, "claude")
	ws := t.TempDir()
	c := &Consult{
		ID:         "consult-pre-3",
		WorkingDir: ws,
		Composed:   Composed{System: "sys", User: "usr"},
	}
	if err := f(context.Background(), c); err == nil {
		t.Fatal("expected spawn failure")
	}
	// CLAUDE.md should not exist (created then removed) — Apply
	// would have written one but Remove on failure cleans it up.
	if _, err := os.Stat(filepath.Join(ws, claudemd.FileName)); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md left behind after spawn failure; stat err = %v", err)
	}
}

func TestNativeSpawn_NoPreambleWriteWhenWorkingDirEmpty(t *testing.T) {
	// Defensive: a consult with no working dir (shouldn't happen in
	// production, but the dispatcher may register one in unusual
	// scope-resolution edge cases) skips the preamble write rather
	// than writing to "". The spawn itself still runs.
	op := &recordingOP{}
	f := NativeSpawn(op, "claude")
	c := &Consult{ID: "no-dir", Composed: Composed{User: "x"}}
	if err := f(context.Background(), c); err != nil {
		t.Errorf("spawn with empty WorkingDir errored: %v", err)
	}
}
