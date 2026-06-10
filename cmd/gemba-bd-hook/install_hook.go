package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// installGitHook drops a `.git/hooks/post-commit` shell script in
// repoPath that invokes this binary with --from-dolt-diff HEAD~1
// (gm-1890). Idempotent — re-running overwrites the file with the
// latest template. Returns the absolute path of the installed hook
// on success.
//
// The hook is intentionally minimal: it execs gemba-bd-hook (must
// be on PATH or installed at the absolute path the hook records at
// install time) and returns 0 unconditionally so the post-commit
// chain doesn't break the operator's commit on hook errors.
//
// This is the narrowest of the three local-only triggers: it fires
// only when the operator commits the source repo, which catches
// teams that commit `.beads/issues.jsonl` after every bd write but
// misses bare `bd update` invocations on unstaged trees. For the
// broader coverage path see runWatch (`--watch`).
func installGitHook(repoPath, binPath string) (string, error) {
	if repoPath == "" {
		return "", errors.New("install: repoPath required")
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("install: abs: %w", err)
	}
	gitDir := filepath.Join(abs, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return "", fmt.Errorf("install: %s/.git not found (run `git init` first?): %w", abs, err)
	}
	// Some git workspaces use `.git` as a file pointing at gitdir
	// (worktrees, submodules). Walk that pointer so we install in
	// the right hooks dir.
	hooksDir := filepath.Join(gitDir, "hooks")
	if !info.IsDir() {
		hooksDir, err = resolveGitFilePointer(gitDir)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", fmt.Errorf("install: mkdir %s: %w", hooksDir, err)
	}

	hookPath := filepath.Join(hooksDir, "post-commit")
	body := renderPostCommitHook(binPath)
	if err := os.WriteFile(hookPath, []byte(body), 0o755); err != nil {
		return "", fmt.Errorf("install: write %s: %w", hookPath, err)
	}
	return hookPath, nil
}

// resolveGitFilePointer reads a `.git` FILE (not dir) and follows
// the `gitdir: <path>` line to the real git directory. Returns
// the hooks/ subdir under the resolved path.
func resolveGitFilePointer(gitFilePath string) (string, error) {
	b, err := os.ReadFile(gitFilePath)
	if err != nil {
		return "", fmt.Errorf("install: read %s: %w", gitFilePath, err)
	}
	const prefix = "gitdir:"
	for _, line := range splitLines(string(b)) {
		if !startsWith(line, prefix) {
			continue
		}
		dir := trimSpace(line[len(prefix):])
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(gitFilePath), dir)
		}
		return filepath.Join(dir, "hooks"), nil
	}
	return "", fmt.Errorf("install: unrecognized .git file shape at %s", gitFilePath)
}

// renderPostCommitHook produces the shell script body. binPath is
// the path to gemba-bd-hook to invoke; an empty value falls back
// to "gemba-bd-hook" relying on PATH lookup at hook execution time.
func renderPostCommitHook(binPath string) string {
	if binPath == "" {
		binPath = "gemba-bd-hook"
	}
	// The hook lives in the source repo's .git/hooks/. cwd at
	// invocation time is the repo root, which is also the bd
	// workspace root in most setups. --from-dolt-diff HEAD~1
	// scans the just-committed Dolt commit for changed issue ids.
	// `|| true` keeps the hook fail-open even if the binary is
	// missing on PATH.
	return `#!/bin/sh
# gemba-bd-hook post-commit hook (gm-1890).
# Installed by ` + "`gemba-bd-hook --install-git-hook`" + `; safe to re-run.
# Fail-open: any error here MUST NOT break the operator's git commit.
set -e
` + binPath + ` --from-dolt-diff HEAD~1 || true
exit 0
`
}

// --- tiny string helpers (avoid pulling in strings/bytes for two
//     calls; keeps the binary footprint smaller) ---

func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
