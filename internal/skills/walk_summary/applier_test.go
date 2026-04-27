package walk_summary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/walk"
)

func TestApplier_NewWithEmptyRoot(t *testing.T) {
	if a := NewApplier(""); a != nil {
		t.Errorf("NewApplier(\"\") = %v, want nil", a)
	}
	if a := NewApplier("   "); a != nil {
		t.Errorf("NewApplier(whitespace) = %v, want nil", a)
	}
}

func TestApplier_ApplyWritesFileAtomically(t *testing.T) {
	root := t.TempDir()
	a := NewApplier(root)
	if a == nil {
		t.Fatal("NewApplier returned nil for valid root")
	}
	out := Output{
		RelativePath: "docs/walks/2026-04-26-test.md",
		Markdown:     "# Test\n\nbody\n",
	}
	abs, err := a.Apply(context.Background(), out)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := filepath.Join(root, "docs", "walks", "2026-04-26-test.md")
	if abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != out.Markdown {
		t.Errorf("file content = %q, want %q", got, out.Markdown)
	}
	// No leftover *.tmp files.
	entries, err := os.ReadDir(filepath.Dir(abs))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestApplier_ApplyRejectsEscape(t *testing.T) {
	root := t.TempDir()
	a := NewApplier(root)
	out := Output{
		RelativePath: "../../etc/passwd",
		Markdown:     "x",
	}
	if _, err := a.Apply(context.Background(), out); err == nil {
		t.Fatal("expected escape to be rejected")
	}
}

func TestApplier_ApplyRejectsEmpty(t *testing.T) {
	root := t.TempDir()
	a := NewApplier(root)
	if _, err := a.Apply(context.Background(), Output{}); err == nil {
		t.Errorf("expected error on empty output")
	}
	if _, err := a.Apply(context.Background(), Output{RelativePath: "x", Markdown: "  "}); err == nil {
		t.Errorf("expected error on whitespace-only markdown")
	}
}

func TestApplier_NilApplier(t *testing.T) {
	var a *Applier
	if _, err := a.Apply(context.Background(), Output{RelativePath: "x", Markdown: "y"}); err == nil {
		t.Error("expected error on nil applier")
	}
	if a.Root() != "" {
		t.Error("nil applier root should be empty")
	}
}

func TestApplier_RespectsContextCancel(t *testing.T) {
	root := t.TempDir()
	a := NewApplier(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Apply(ctx, Output{RelativePath: "x.md", Markdown: "y"}); err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestSkill_RunGenerateOnly(t *testing.T) {
	s := New(nil)
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:        "walk-001",
		Label:     "demo",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
	}
	out, abs, err := s.Run(context.Background(), w, fixedNow())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if abs != "" {
		t.Errorf("abs = %q, want empty (no applier)", abs)
	}
	if out.Markdown == "" {
		t.Error("Markdown should still be populated when no applier")
	}
	if s.HasApplier() {
		t.Error("HasApplier should be false")
	}
}

func TestSkill_RunWithApplier(t *testing.T) {
	root := t.TempDir()
	s := New(NewApplier(root))
	ended := mustTime("2026-04-26T21:30:00Z")
	w := walk.Walk{
		ID:        "walk-001",
		Label:     "demo",
		StartedAt: mustTime("2026-04-26T20:00:00Z"),
		EndedAt:   &ended,
	}
	out, abs, err := s.Run(context.Background(), w, fixedNow())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !s.HasApplier() {
		t.Error("HasApplier should be true")
	}
	if abs == "" {
		t.Fatal("abs should be set")
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != out.Markdown {
		t.Errorf("content mismatch")
	}
}

func TestSkill_NilSkill(t *testing.T) {
	var s *Skill
	if _, _, err := s.Run(context.Background(), walk.Walk{}, time.Now()); err == nil {
		t.Error("expected error on nil skill")
	}
	if s.HasApplier() {
		t.Error("nil skill should not claim applier")
	}
}
