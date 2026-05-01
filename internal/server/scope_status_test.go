package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
)

func TestInspectScopeStatusReportsCleanCurrentGitNexus(t *testing.T) {
	repo := initScopeStatusRepo(t)
	head := gitTestOutput(t, repo, "rev-parse", "HEAD")
	writeGitNexusMeta(t, repo, head)

	got := inspectScopeStatus(context.Background(), repo)
	if got == nil || got.Git == nil || got.Analysis == nil {
		t.Fatalf("scope status incomplete: %+v", got)
	}
	if got.Git.State != "clean" {
		t.Fatalf("git state = %q, want clean; status=%+v", got.Git.State, got.Git)
	}
	if got.Git.ChangedFiles != 0 {
		t.Fatalf("changed_files = %d, want 0", got.Git.ChangedFiles)
	}
	if got.Analysis.State != "current" {
		t.Fatalf("analysis state = %q, want current; status=%+v", got.Analysis.State, got.Analysis)
	}
	if got.Analysis.IndexedCommit != head {
		t.Fatalf("indexed commit = %q, want %q", got.Analysis.IndexedCommit, head)
	}
}

func TestInspectScopeStatusReportsDirtyAndStale(t *testing.T) {
	repo := initScopeStatusRepo(t)
	oldHead := gitTestOutput(t, repo, "rev-parse", "HEAD")
	writeGitNexusMeta(t, repo, oldHead)

	if err := os.WriteFile(filepath.Join(repo, "app.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "app.go")
	gitTest(t, repo, "commit", "-m", "change")
	if err := os.WriteFile(filepath.Join(repo, "app.go"), []byte("package main\n\nfunc main() { println(\"dirty\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := inspectScopeStatus(context.Background(), repo)
	if got.Git.State != "dirty" {
		t.Fatalf("git state = %q, want dirty; status=%+v", got.Git.State, got.Git)
	}
	if got.Git.ChangedFiles != 1 {
		t.Fatalf("changed_files = %d, want 1", got.Git.ChangedFiles)
	}
	if got.Analysis.State != "stale" {
		t.Fatalf("analysis state = %q, want stale; status=%+v", got.Analysis.State, got.Analysis)
	}
}

func TestInspectScopeStatusMarksCurrentIndexStaleWhenWorktreeDirty(t *testing.T) {
	repo := initScopeStatusRepo(t)
	head := gitTestOutput(t, repo, "rev-parse", "HEAD")
	writeGitNexusMeta(t, repo, head)

	if err := os.WriteFile(filepath.Join(repo, "app.go"), []byte("package main\n\n// uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := inspectScopeStatus(context.Background(), repo)
	if got.Git.State != "dirty" {
		t.Fatalf("git state = %q, want dirty", got.Git.State)
	}
	if got.Analysis.State != "stale" {
		t.Fatalf("analysis state = %q, want stale; status=%+v", got.Analysis.State, got.Analysis)
	}
}

func TestScopeStatusPathFallsBackToSessionWorktreeMetadata(t *testing.T) {
	got := scopeStatusPath(&planner.OperationalContext{
		Session: &core.Session{
			ID: "sess-1",
			ProviderMetadata: map[string]any{
				"worktree": "/tmp/gemba-worktree",
			},
		},
	})
	if got != "/tmp/gemba-worktree" {
		t.Fatalf("path = %q, want session metadata worktree", got)
	}
}

func initScopeStatusRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitTest(t, repo, "init")
	gitTest(t, repo, "config", "user.email", "test@example.com")
	gitTest(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".gitnexus/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", ".gitignore", "app.go")
	gitTest(t, repo, "commit", "-m", "initial")
	return repo
}

func writeGitNexusMeta(t *testing.T, repo, commit string) {
	t.Helper()
	dir := filepath.Join(repo, ".gitnexus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{
		"lastCommit": commit,
		"indexedAt":  time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	_ = gitTestOutput(t, repo, args...)
}

func gitTestOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return stringTrimSpace(string(out))
}

func stringTrimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && (s[0] == '\n' || s[0] == '\r' || s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}
