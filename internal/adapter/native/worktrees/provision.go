// Package worktrees provisions git worktrees for native-adaptor
// sessions (gm-native.9). Auto-create + silent reuse are the only
// behaviors; branch base is always "main" per design review. If
// the worktree exists but is dirty, we still reuse silently — the
// operator's in-progress work is more important than a clean
// starting point.
package worktrees

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config carries the tunables the provisioner needs.
type Config struct {
	// RepoRoot is the path to the main repo worktree (the one that
	// contains .git/). When empty the provisioner discovers it via
	// `git rev-parse --show-toplevel` from the cwd.
	RepoRoot string
	// WorktreesDir overrides the default worktree parent dir. When
	// empty the provisioner defaults to "../worktrees" sibling to
	// the repo root — the standard convention for co-located
	// worktrees.
	WorktreesDir string
	// BaseBranch is the branch new worktrees branch from. Empty
	// means "main".
	BaseBranch string
}

// Resolve computes the worktree path for the given bead id, creating
// it if missing. Returns the absolute path. Silent reuse when the
// path already exists on disk and is a valid worktree.
func Resolve(ctx context.Context, cfg Config, beadID string) (string, error) {
	if beadID == "" {
		return "", fmt.Errorf("worktrees: bead id required")
	}
	repo, err := discoverRepoRoot(ctx, cfg.RepoRoot)
	if err != nil {
		return "", err
	}
	parent := cfg.WorktreesDir
	if parent == "" {
		parent = filepath.Join(filepath.Dir(repo), "worktrees")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("worktrees: mkdir %s: %w", parent, err)
	}

	target := filepath.Join(parent, "bead-"+safePath(beadID))
	if _, err := os.Stat(target); err == nil {
		// Already exists — silent reuse per design review.
		return target, nil
	}

	base := cfg.BaseBranch
	if base == "" {
		base = "main"
	}
	branch := "bead/" + beadID
	if err := runGit(ctx, repo, "worktree", "add", "-b", branch, target, base); err != nil {
		// If the branch already exists (maybe a previous session
		// failed to clean up), retry without -b so the worktree
		// attaches to the existing branch.
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "already used") {
			if retry := runGit(ctx, repo, "worktree", "add", target, branch); retry == nil {
				return target, nil
			}
		}
		return "", err
	}
	return target, nil
}

// Remove detaches the worktree for a bead. Best-effort — if the
// worktree is gone already, returns nil. Errors only when git
// itself reports a problem.
func Remove(ctx context.Context, cfg Config, beadID string) error {
	repo, err := discoverRepoRoot(ctx, cfg.RepoRoot)
	if err != nil {
		return err
	}
	parent := cfg.WorktreesDir
	if parent == "" {
		parent = filepath.Join(filepath.Dir(repo), "worktrees")
	}
	target := filepath.Join(parent, "bead-"+safePath(beadID))
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return nil
	}
	return runGit(ctx, repo, "worktree", "remove", "--force", target)
}

func discoverRepoRoot(ctx context.Context, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("worktrees: cwd: %w", err)
	}
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("worktrees: discover repo root from %s: %w", cwd, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runGit(ctx context.Context, cwd string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// safePath mirrors the bridge's safeSessionID conversion so worktree
// paths don't pick up shell-special characters from bead ids.
func safePath(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-', c == '.', c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
