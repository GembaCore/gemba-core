package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdir helper for tests that exercise cwd-based defaults.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestSpecNewKit_CreatesSlashCommands(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	cmd := newSpecCmd()
	cmd.SetArgs([]string{"new", "001-demo", "--kit"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".claude/commands/tasks.md", ".claude/commands/implement.md", "specs/001-demo/spec.md"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	// Idempotent re-run.
	cmd2 := newSpecCmd()
	cmd2.SetArgs([]string{"new", "001-demo", "--kit"})
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("re-run failed: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".claude", "commands", "tasks.md"))
	if !strings.Contains(string(body), "001-demo") {
		t.Fatalf("slug not substituted: %s", body)
	}
}

func TestSpecInitAsdd_FullSeed(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	cmd := newSpecCmd()
	cmd.SetArgs([]string{"init", "--asdd"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		".gemba/constitution.md",
		".claude/hooks.json",
		".claude/commands/tasks.md",
		".claude/commands/implement.md",
		"CLAUDE.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	// CLAUDE.md must contain the ASDD stanza.
	body, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(body), "ASDD") {
		t.Fatalf("CLAUDE.md missing ASDD stanza: %s", body)
	}
}
