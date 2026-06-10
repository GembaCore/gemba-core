package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallGitHook_HappyPath(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	hookPath, err := installGitHook(repo, "/usr/local/bin/gemba-bd-hook")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	want := filepath.Join(repo, ".git", "hooks", "post-commit")
	if hookPath != want {
		t.Errorf("path = %q, want %q", hookPath, want)
	}
	body, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read installed hook: %v", err)
	}
	if !strings.Contains(string(body), "/usr/local/bin/gemba-bd-hook") {
		t.Errorf("installed hook missing bin path:\n%s", body)
	}
	if !strings.Contains(string(body), "--from-dolt-diff HEAD~1") {
		t.Errorf("installed hook missing diff invocation:\n%s", body)
	}
	// Mode 0755 — git refuses to run non-executable hooks.
	st, _ := os.Stat(hookPath)
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("hook is not executable; mode=%v", st.Mode())
	}
}

// Idempotency: re-running install overwrites with the latest template.
func TestInstallGitHook_Idempotent(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	if _, err := installGitHook(repo, "/old/path"); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := installGitHook(repo, "/new/path"); err != nil {
		t.Fatalf("second install: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	if !strings.Contains(string(body), "/new/path") {
		t.Errorf("install was not idempotent; second invocation didn't take effect:\n%s", body)
	}
	if strings.Contains(string(body), "/old/path") {
		t.Errorf("stale path lingered:\n%s", body)
	}
}

// Empty bin path falls back to PATH lookup.
func TestInstallGitHook_DefaultBinPath(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	if _, err := installGitHook(repo, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", "post-commit"))
	// Must invoke "gemba-bd-hook" by bare name (PATH lookup at run time).
	if !strings.Contains(string(body), "\ngemba-bd-hook --from-dolt-diff") {
		t.Errorf("expected PATH-relative invocation:\n%s", body)
	}
}

// Missing .git → clear error.
func TestInstallGitHook_RejectsMissingGit(t *testing.T) {
	repo := t.TempDir() // no .git/
	_, err := installGitHook(repo, "")
	if err == nil {
		t.Fatal("expected error when .git is absent")
	}
	if !strings.Contains(err.Error(), ".git not found") {
		t.Errorf("error missing diagnostic: %v", err)
	}
}

// .git as a FILE (worktree / submodule pattern) is followed.
func TestInstallGitHook_GitFilePointer(t *testing.T) {
	repo := t.TempDir()
	realGit := t.TempDir() // the gitdir the .git file points at
	gitFile := filepath.Join(repo, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+realGit+"\n"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	hookPath, err := installGitHook(repo, "")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.HasPrefix(hookPath, realGit) {
		t.Errorf("hook installed at %q, expected under %q", hookPath, realGit)
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Errorf("hook not at expected path: %v", err)
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines("a\nb\nc")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestStartsWith(t *testing.T) {
	if !startsWith("hello world", "hello") {
		t.Error("hello/hello world failed")
	}
	if startsWith("hi", "hello") {
		t.Error("short s + long prefix shouldn't match")
	}
}

func TestTrimSpace(t *testing.T) {
	if got := trimSpace("  x  "); got != "x" {
		t.Errorf("got %q", got)
	}
	if got := trimSpace("\t\rx\r\t"); got != "x" {
		t.Errorf("got %q", got)
	}
}
