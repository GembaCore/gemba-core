package worktrees

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepo initializes a throwaway git repo with a committed main
// branch so worktree add has something to branch from.
func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func TestResolveCreatesWorktreeOnFirstCall(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	path, err := Resolve(ctx, Config{RepoRoot: repo}, "gm-foo")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree not on disk: %v", err)
	}
	if !strings.HasSuffix(path, "bead-gm-foo") {
		t.Errorf("unexpected path shape: %q", path)
	}
}

func TestResolveReusesSilently(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	first, err := Resolve(ctx, Config{RepoRoot: repo}, "gm-foo")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Pollute the worktree so we can verify we're reusing it, not
	// recreating it.
	if err := os.WriteFile(filepath.Join(first, "pollute.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(ctx, Config{RepoRoot: repo}, "gm-foo")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Errorf("path changed: %q vs %q", first, second)
	}
	if _, err := os.Stat(filepath.Join(second, "pollute.txt")); err != nil {
		t.Error("reuse wiped operator work — must be silent")
	}
}

func TestResolveSanitizesBeadID(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	path, err := Resolve(ctx, Config{RepoRoot: repo}, "gm-root.native.7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasSuffix(path, "bead-gm-root.native.7") {
		t.Errorf("dot should survive: %q", path)
	}
}

func TestResolveWithExplicitWorktreesDir(t *testing.T) {
	repo := setupRepo(t)
	dest := filepath.Join(t.TempDir(), "custom")
	ctx := context.Background()
	path, err := Resolve(ctx, Config{RepoRoot: repo, WorktreesDir: dest}, "gm-foo")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasPrefix(path, dest) {
		t.Errorf("path should be under %q, got %q", dest, path)
	}
}

func TestRemoveCleansWorktree(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()
	path, err := Resolve(ctx, Config{RepoRoot: repo}, "gm-foo")
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(ctx, Config{RepoRoot: repo}, "gm-foo"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree path should be gone, got err=%v", err)
	}
}

func TestRemoveMissingIsNoOp(t *testing.T) {
	repo := setupRepo(t)
	if err := Remove(context.Background(), Config{RepoRoot: repo}, "gm-nope"); err != nil {
		t.Errorf("Remove missing worktree should be no-op, got %v", err)
	}
}
